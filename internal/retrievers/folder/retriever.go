package folder

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/pkg/converters"
	cJson "github.com/kyzrfranz/go-fitter/pkg/converters/fit/json"
	"github.com/muktihari/fit/decoder"
)

type Retriever struct {
	max       int
	extension string
	// from/to gate which activities get imported by their start time.
	// Zero values disable the respective bound. Only used for raw .fit imports.
	from time.Time
	to   time.Time
}

func New(max int, extension string) *Retriever {
	return &Retriever{
		max:       max,
		extension: extension,
	}
}

// NewWithRange is like New but restricts raw .fit imports to activities whose
// start time falls within [from, to]. Zero from/to disables that bound.
func NewWithRange(max int, extension string, from, to time.Time) *Retriever {
	return &Retriever{
		max:       max,
		extension: extension,
		from:      from,
		to:        to,
	}
}

func (d *Retriever) Retrieve(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var foundFiles []string
	for idx, entry := range entries {
		if d.max > -1 && idx > d.max {
			break
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), fmt.Sprintf(".%s", d.extension)) {
			foundFiles = append(foundFiles, path+"/"+entry.Name())
		}
	}

	return foundFiles, nil
}

// ReadRaw returns the raw FIT bytes for a path along with a .fit filename. The
// syncer stashes these in a BlobStore so the chart endpoint can rebuild records
// later by decoding them with FitToActivity. This is the syncer.RawSource
// capability.
//
// For a .zip source we must return the FIT extracted from the archive, NOT the
// zip itself: the blob store's reader decodes the bytes as a FIT, and a raw zip
// would fail with "not a FIT file". Extracting here also keeps the stored blob's
// content hash identical to the activities' source.file_hash computed at import.
func (d *Retriever) ReadRaw(path string) ([]byte, string, error) {
	if d.extension == "zip" {
		fitReader, err := retrieveFitFromZip(path)
		if err != nil {
			return nil, "", err
		}
		if fitReader == nil {
			return nil, "", NoFitFileFound
		}
		data, err := io.ReadAll(fitReader)
		if err != nil {
			return nil, "", err
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + ".fit"
		return data, name, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	return data, filepath.Base(path), nil
}

var NoFitFileFound = fmt.Errorf("no fit file found")

func (d *Retriever) Read(path string) (io.ReadCloser, error) {
	if d.extension == "json" {
		return os.Open(path)
	} else if d.extension == "zip" {
		fitFile, err := retrieveFitFromZip(path)
		if err != nil {
			return nil, err
		}

		if fitFile == nil {
			return nil, NoFitFileFound
		}
		activities, err := getActivity(fitFile, true)
		if err != nil {
			return nil, err
		}
		for _, activity := range activities {
			activity.Source.Provider = "folder"
			activity.Source.Path = path
		}
		return activitiesReader(activities)
	} else if d.extension == "fit" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		activities, err := getActivity(f, true)
		if err != nil {
			// Corrupt or otherwise undecodable file: skip rather than abort the
			// whole run. Returning nil lets the syncer move on to the next file.
			return nil, nil
		}

		var keep []*v1.Activity
		for _, activity := range activities {
			// A Garmin export folder is full of non-activity .fit files (device
			// settings, daily monitoring, HRV, metrics). Those carry no session
			// and therefore no start time, so we drop them here.
			if activity.StartTime.IsZero() || len(activity.SessionSummary) == 0 {
				continue
			}

			// Date-range gate: only import what the caller explicitly asked for.
			if !d.from.IsZero() && activity.StartTime.Before(d.from) {
				continue
			}
			if !d.to.IsZero() && activity.StartTime.After(d.to) {
				continue
			}

			activity.Source.Provider = "garmin"
			activity.Source.Path = path
			keep = append(keep, activity)
		}

		if len(keep) == 0 {
			return nil, nil
		}
		return activitiesReader(keep)
	}
	return nil, nil
}

// activitiesReader marshals the legs of one source file into the JSON array that
// the syncer and chart endpoint decode.
func activitiesReader(activities []*v1.Activity) (io.ReadCloser, error) {
	b, err := json.Marshal(activities)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(string(b))), nil
}

func retrieveFitFromZip(zipPath string) (io.Reader, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		if !strings.EqualFold(filepath.Ext(f.Name), ".fit") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		fitFileBytes, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		return strings.NewReader(string(fitFileBytes)), nil
	}

	return nil, nil
}

func getActivity(fitFile io.Reader, withRecords bool) ([]*v1.Activity, error) {
	var decoderOptions []decoder.Option
	decoderOptions = append(decoderOptions, decoder.WithIgnoreChecksum())

	var jsonOpts []cJson.Option
	if !withRecords {
		jsonOpts = append(jsonOpts, cJson.WithNoRecords())
	}

	return converters.FitToActivity(fitFile, decoderOptions, jsonOpts...)
}
