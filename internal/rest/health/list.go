package fit

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/db"
	"github.com/kyzrfranz/go-fitter/internal/rest"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := rest.BuildHealthFilter(q)
	findOptions := rest.BuildHealthFindOptions(q)

	data, _ := h.repo.List(r.Context(), filter, findOptions)

	response := AggregateAndClean(data)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AggregateAndClean(buckets []db.HealthMetric) map[string]*v1.HealthMetricSeries {
	// Map: MetricName -> Series Data
	result := make(map[string]*v1.HealthMetricSeries)

	for _, bucket := range buckets {
		if _, exists := result[bucket.Name]; !exists {
			result[bucket.Name] = &v1.HealthMetricSeries{
				Name:    bucket.Name,
				Units:   bucket.Units,
				Samples: []map[string]interface{}{},
			}
		}

		// Append raw samples from this day to the main list
		result[bucket.Name].Samples = append(result[bucket.Name].Samples, bucket.Samples...)
	}

	// Post-Process each series: Sort by Date & Deduplicate
	for _, series := range result {
		cleanSamples(series)
	}

	return result
}

func cleanSamples(series *v1.HealthMetricSeries) {
	// 1. Format Dates
	// Iterate through all samples to ensure 'date' is a nice string for JSON.
	// We do NOT drop the sample if parsing fails; we just leave it as is.
	for _, sample := range series.Samples {
		t := getTime(sample["date"])
		if !t.IsZero() {
			sample["date"] = t.Format(time.RFC3339)
		}
	}

	// 2. Sort by Date
	sort.Slice(series.Samples, func(i, j int) bool {
		t1 := getTime(series.Samples[i]["date"])
		t2 := getTime(series.Samples[j]["date"])
		return t1.Before(t2)
	})

	// 3. Deduplicate
	// Remove entries that have the exact same timestamp to fix overlapping imports.
	if len(series.Samples) == 0 {
		return
	}

	uniqueSamples := make([]map[string]interface{}, 0, len(series.Samples))
	seen := make(map[int64]bool)

	for _, sample := range series.Samples {
		t := getTime(sample["date"])

		// If we can't parse the date, keep it to be safe.
		if t.IsZero() {
			uniqueSamples = append(uniqueSamples, sample)
			continue
		}

		ts := t.Unix() // precision to second
		if !seen[ts] {
			seen[ts] = true
			uniqueSamples = append(uniqueSamples, sample)
		}
	}

	series.Samples = uniqueSamples
}

func getTime(val interface{}) time.Time {
	if val == nil {
		return time.Time{}
	}

	switch v := val.(type) {
	case time.Time:
		return v
	case bson.DateTime:
		return v.Time()
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			return t
		}
		// Fallback for other formats if needed
		t, err = time.Parse("2006-01-02 15:04:05 -0700", v)
		if err == nil {
			return t
		}
		return time.Time{}
	default:
		return time.Time{}
	}
}
