package converters

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/pkg/aggregators"
	cJson "github.com/kyzrfranz/go-fitter/pkg/converters/fit/json"
	"github.com/kyzrfranz/go-fitter/pkg/extractors"
	"github.com/muktihari/fit/decoder"
)

// FitToActivity decodes a FIT stream into one activity per non-transition
// session. A normal single-sport file yields a one-element slice; a multisport
// (triathlon) file yields one activity per leg (swim, bike, run), each carrying
// its own session, laps and records. See the converter for how transitions are
// dropped and records/laps are partitioned by time window.
func FitToActivity(ff io.Reader, decoderOptions []decoder.Option, opts ...cJson.Option) ([]*v1.Activity, error) {
	// We don't need a bufio.Writer, json.Marshal writes it all at once at the end
	conv := cJson.NewFITToJSONConv(opts...) // Use the new converter

	// Tee the FIT bytes through a sha256 hasher so we can content-address
	// the resulting activity without buffering the whole file in memory.
	hasher := sha256.New()
	teed := io.TeeReader(ff, hasher)

	options := []decoder.Option{
		decoder.WithMesgDefListener(conv),
		decoder.WithMesgListener(conv),
		decoder.WithBroadcastOnly(),
		decoder.WithBroadcastMesgCopy(),
	}
	options = append(options, decoderOptions...)
	dec := decoder.New(teed, options...)

	var err error
	for dec.Next() {
		_, err = dec.Decode()
		if err != nil {
			break
		}
	}

	conv.Wait() // This is where the JSON is marshaled and written

	if err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
	}

	if err := conv.Err(); err != nil {
		return nil, fmt.Errorf("convert done with error: %v", err)
	}

	result := conv.Result()

	var legs []v1.Activity
	if err := json.Unmarshal([]byte(result), &legs); err != nil {
		return nil, fmt.Errorf("unmarshal activities: %w", err)
	}

	fileHash := hex.EncodeToString(hasher.Sum(nil))
	ingest := time.Now().UTC()
	multi := len(legs) > 1

	activities := make([]*v1.Activity, 0, len(legs))
	for i := range legs {
		response := legs[i]

		// Lift canonical fields to the document root so indexes on
		// {sport,start_time} and projections without sessionSummary work cleanly.
		response.Sport = aggregators.GetString(response.SportMesg, "sport")
		if response.Sport == "Unknown" || response.Sport == "" {
			response.Sport = aggregators.GetString(response.SessionSummary, "sport")
		}
		response.SubSport = aggregators.GetString(response.SportMesg, "sub_sport")
		if response.SubSport == "Unknown" || response.SubSport == "" {
			response.SubSport = aggregators.GetString(response.SessionSummary, "sub_sport")
		}
		if ts, ok := response.SessionSummary["start_time"].(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				response.StartTime = t
			}
		}

		response.Source.FileHash = fileHash
		response.Source.IngestTime = ingest
		if response.Id == "" {
			// One file, one id. For a multisport file each leg needs its own
			// stable id, so suffix with the leg index and sport. The index keeps
			// ids unique even when a file repeats a sport (duathlon run/bike/run,
			// brick, aquathlon); the sport keeps them readable. Re-decoding the
			// same file yields the same legs in the same order, so the ids are
			// stable, which the chart endpoint relies on.
			if multi {
				response.Id = fmt.Sprintf("%s-%d-%s", fileHash[:16], i, strings.ToLower(response.Sport))
			} else {
				response.Id = fileHash[:16]
			}
		}

		response.Meta.Title = defaultTitle(response.Sport, response.StartTime)

		response.Custom.MaxHR = extractors.MaxHr(response)
		response.Custom.AvgHR = extractors.AvgHeartRate(response)
		response.Custom.AvgPwr = extractors.AvgPower(response)
		response.Custom.AvgSpeed = extractors.AvgSpeed(response)
		response.Custom.TotalTime = extractors.TotalTime(response)
		response.Custom.TotalDistance = extractors.TotalDistance(response)
		response.Custom.NormalizedPower = extractors.NP(response)
		response.Custom.Decoupling = extractors.Decoupling(response)
		response.Custom.Efficiency = extractors.Efficiency(response)
		response.Custom.PacingTrend = extractors.PacingTrend(response)

		activities = append(activities, &response)
	}

	return activities, nil
}

func defaultTitle(sport string, start time.Time) string {
	label := sport
	if label == "" || label == "Unknown" {
		label = "Activity"
	}
	if start.IsZero() {
		return label
	}
	return fmt.Sprintf("%s %s", label, start.Format("2006-01-02"))
}
