package fit

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/pkg/aggregators"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// DefaultSeriesFields are emitted when the caller doesn't pass ?fields=.
var DefaultSeriesFields = []string{"time", "hr", "power", "cadence", "speed", "altitude"}

// fieldSource maps a response field name to the FIT record keys to read.
// Multiple keys are tried in order; first non-zero wins.
var fieldSource = map[string][]string{
	"hr":       {"heart_rate"},
	"cadence":  {"cadence"},
	"speed":    {"speed", "enhanced_speed"},
	"altitude": {"altitude", "enhanced_altitude"},
}

// fieldReader holds bespoke readers for fields that need sanitization beyond a
// plain key lookup. `power` runs through aggregators.Power to drop sensor
// spikes and the FIT uint16 sentinel.
var fieldReader = map[string]func(map[string]any) float64{
	"power": aggregators.Power,
}

func (h *Handler) Series(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Need Source.Path to find the file; project just what we need.
	findOpts := options.FindOne().SetProjection(bson.M{
		"source":     1,
		"start_time": 1,
	})
	stub, err := h.repository.Get(r.Context(), id, findOpts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if stub.Source.Path == "" {
		http.Error(w, "activity has no source.path", http.StatusUnprocessableEntity)
		return
	}
	if h.retriever == nil {
		http.Error(w, "no retriever configured for chart serving", http.StatusServiceUnavailable)
		return
	}

	raw, err := h.retriever.Read(stub.Source.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer raw.Close()

	// The retriever returns one entry per leg (a multisport file has several).
	// Pick the leg matching the requested activity so a triathlon's bike chart
	// doesn't include swim/run records.
	var legs []v1.Activity
	if err := json.NewDecoder(raw).Decode(&legs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	full := selectLeg(legs, id, stub.StartTime)

	fields := ParseFields(r.URL.Query().Get("fields"))
	resp := BuildSeries(full.Records, stub.StartTime, fields)
	// Identify the payload so a client can't confuse one leg's series with
	// another's (multisport legs share a source file and start within minutes).
	resp.Id = id

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// selectLeg returns the leg matching the requested activity. It prefers an id
// match, then a start-time match (within a second), and falls back to the first
// leg so single-sport files keep working unchanged.
func selectLeg(legs []v1.Activity, id string, start time.Time) v1.Activity {
	for i := range legs {
		if legs[i].Id == id {
			return legs[i]
		}
	}
	for i := range legs {
		if d := legs[i].StartTime.Sub(start); d < time.Second && d > -time.Second {
			return legs[i]
		}
	}
	if len(legs) > 0 {
		return legs[0]
	}
	return v1.Activity{}
}

type SeriesResponse struct {
	Id          string           `json:"id"`
	SampleCount int              `json:"sample_count"`
	StartTime   time.Time        `json:"start_time"`
	Fields      []string         `json:"fields"`
	Data        map[string][]any `json:"data"`
}

func ParseFields(q string) []string {
	if q == "" {
		return DefaultSeriesFields
	}
	parts := strings.Split(q, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return DefaultSeriesFields
	}
	// Always include time as the x-axis.
	hasTime := false
	for _, f := range out {
		if f == "time" {
			hasTime = true
			break
		}
	}
	if !hasTime {
		out = append([]string{"time"}, out...)
	}
	return out
}

func BuildSeries(records []map[string]any, start time.Time, fields []string) SeriesResponse {
	data := make(map[string][]any, len(fields))
	for _, f := range fields {
		data[f] = make([]any, 0, len(records))
	}

	for _, rec := range records {
		// Resolve sample time once.
		var sec any = nil
		if ts, ok := rec["timestamp"].(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				if !start.IsZero() {
					sec = t.Sub(start).Seconds()
				} else {
					sec = t.Unix()
				}
			}
		}

		for _, f := range fields {
			if f == "time" {
				data[f] = append(data[f], sec)
				continue
			}
			var v any = nil
			if reader, ok := fieldReader[f]; ok {
				if fv := reader(rec); fv != 0 {
					v = fv
				}
			} else {
				keys, ok := fieldSource[f]
				if !ok {
					// Unknown field — try the raw key from the record.
					keys = []string{f}
				}
				for _, k := range keys {
					if fv := aggregators.GetFloat(rec, k); fv != 0 {
						v = fv
						break
					}
				}
			}
			data[f] = append(data[f], v)
		}
	}

	return SeriesResponse{
		SampleCount: len(records),
		StartTime:   start,
		Fields:      fields,
		Data:        data,
	}
}
