package dropbox

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/kyzrfranz/go-fitter/pkg/converters"
	cJson "github.com/kyzrfranz/go-fitter/pkg/converters/fit/json"
	"github.com/muktihari/fit/decoder"
)

func getActivity(fitFile io.Reader, withRecords bool, remotePath string) (io.ReadCloser, error) {
	var decoderOptions []decoder.Option
	decoderOptions = append(decoderOptions, decoder.WithIgnoreChecksum())

	var jsonOpts []cJson.Option
	if !withRecords {
		jsonOpts = append(jsonOpts, cJson.WithNoRecords())
	}

	activities, err := converters.FitToActivity(fitFile, decoderOptions, jsonOpts...)
	if err != nil {
		return nil, err
	}

	for _, activity := range activities {
		activity.Source.Provider = "dropbox"
		activity.Source.Path = remotePath
	}
	// Return the legs as a JSON array, matching the folder/gridfs retrievers.
	activityBytes, err := json.Marshal(activities)
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(activityBytes)
	return io.NopCloser(reader), nil
}
