package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/kyzrfranz/go-fitter/internal/db"
	"github.com/kyzrfranz/go-fitter/internal/rest"
)

// healthValueKeys are the sample keys that carry a metric's numeric value,
// tried in order. Apple Health exports store scalars under `qty`, heart-rate
// style metrics under `Avg`, and sleep under `totalSleep`.
var healthValueKeys = []string{"qty", "Avg", "value", "totalSleep"}

// HealthDailyRecord is one metric on one day. A day with a single sample gets
// Value; a day with several gets Avg/Min/Max instead. SampleCount is always
// the raw number of samples behind the record.
type HealthDailyRecord struct {
	Date        string   `json:"date"`
	MetricName  string   `json:"metric_name"`
	Units       string   `json:"units,omitempty"`
	SampleCount int      `json:"sample_count"`
	Value       *float64 `json:"value,omitempty"`
	Avg         *float64 `json:"avg,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
}

// HealthResponse wraps either daily aggregates or raw samples, plus the
// metadata an agent needs to know whether it saw everything.
type HealthResponse struct {
	Mode        string              `json:"mode"`         // "daily" or "raw"
	RecordCount int                 `json:"record_count"` // records actually returned
	MatchCount  int                 `json:"match_count"`  // records that matched before the cap
	Truncated   bool                `json:"truncated,omitempty"`
	Warning     string              `json:"warning,omitempty"`
	Records     []HealthDailyRecord `json:"records,omitempty"`
	RawSamples  []map[string]any    `json:"raw_samples,omitempty"`
}

func registerHealthTool(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("list_health_metrics",
			mcp.WithDescription("List health metrics for a date range. Defaults: aggregated to one record per metric per day, newest first, last 7 days when from/to are omitted. Daily mode returns ~10-50KB for a 7-day window. Pass raw:true for ungrouped samples — that can reach several MB, so narrow the range first. Every response is hard-capped at 1000 records (watch `truncated`/`warning`)."),
			mcp.WithString("name", mcp.Description("Metric name filter (e.g. resting_heart_rate, sleep_analysis, heart_rate_variability). Omit for all metrics.")),
			mcp.WithString("from", mcp.Description("Inclusive lower bound on date (YYYY-MM-DD). Defaults to 7 days ago.")),
			mcp.WithString("to", mcp.Description("Inclusive upper bound on date (YYYY-MM-DD). Defaults to today.")),
			mcp.WithBoolean("raw", mcp.Description("Return ungrouped raw samples instead of daily aggregates. Off by default — large payload.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			q := url.Values{}
			for _, k := range []string{"name", "from", "to"} {
				if v := req.GetString(k, ""); v != "" {
					q.Set(k, v)
				}
			}

			data, _ := deps.Health.List(ctx, rest.BuildHealthFilter(q), rest.BuildHealthFindOptions(q))

			var resp HealthResponse
			if req.GetBool("raw", false) {
				resp = buildRawHealth(data)
			} else {
				resp = buildDailyHealth(data)
			}
			return jsonResult(resp, func(v any) ([]byte, error) { return json.Marshal(v) })
		},
	)
}

// buildDailyHealth collapses each day-bucket into a single record. The health
// collection already stores one document per (metric, day) — see the syncer —
// so a bucket maps straight to a HealthDailyRecord.
func buildDailyHealth(buckets []db.HealthMetric) HealthResponse {
	records := make([]HealthDailyRecord, 0, len(buckets))
	for _, b := range buckets {
		vals := make([]float64, 0, len(b.Samples))
		for _, sample := range b.Samples {
			if v, ok := healthValue(sample); ok {
				vals = append(vals, v)
			}
		}

		rec := HealthDailyRecord{
			Date:        b.Date.UTC().Format("2006-01-02"),
			MetricName:  b.Name,
			Units:       b.Units,
			SampleCount: len(b.Samples),
		}
		switch len(vals) {
		case 0:
			// no numeric value (e.g. a non-scalar metric) — date + count only
		case 1:
			v := vals[0]
			rec.Value = &v
		default:
			mn, mx, sum := vals[0], vals[0], 0.0
			for _, v := range vals {
				if v < mn {
					mn = v
				}
				if v > mx {
					mx = v
				}
				sum += v
			}
			avg := sum / float64(len(vals))
			rec.Avg, rec.Min, rec.Max = &avg, &mn, &mx
		}
		records = append(records, rec)
	}

	// Newest first; metric name as a stable tiebreaker within a day.
	sort.Slice(records, func(i, j int) bool {
		if records[i].Date != records[j].Date {
			return records[i].Date > records[j].Date
		}
		return records[i].MetricName < records[j].MetricName
	})

	resp := HealthResponse{Mode: "daily", MatchCount: len(records)}
	if len(records) > maxRecords {
		resp.Truncated = true
		resp.Warning = fmt.Sprintf("%d daily records matched; returning the %d most recent. Narrow from/to or filter by name.", len(records), maxRecords)
		records = records[:maxRecords]
	}
	resp.Records = records
	resp.RecordCount = len(records)
	return resp
}

// buildRawHealth flattens every sample into a single newest-first list, tagging
// each with its metric so the grouping isn't lost.
func buildRawHealth(buckets []db.HealthMetric) HealthResponse {
	samples := make([]map[string]any, 0)
	for _, b := range buckets {
		for _, s := range b.Samples {
			rs := make(map[string]any, len(s)+2)
			for k, v := range s {
				// Normalize driver datetimes to RFC3339 so the JSON stays readable.
				if dt, ok := v.(bson.DateTime); ok {
					rs[k] = dt.Time().Format(time.RFC3339)
				} else {
					rs[k] = v
				}
			}
			if t := asTime(s["date"]); !t.IsZero() {
				rs["date"] = t.Format(time.RFC3339)
			}
			rs["metric_name"] = b.Name
			if b.Units != "" {
				rs["units"] = b.Units
			}
			samples = append(samples, rs)
		}
	}

	sort.Slice(samples, func(i, j int) bool {
		return asTime(samples[i]["date"]).After(asTime(samples[j]["date"]))
	})

	resp := HealthResponse{Mode: "raw", MatchCount: len(samples)}
	if len(samples) > maxRecords {
		resp.Truncated = true
		resp.Warning = fmt.Sprintf("%d samples matched; returning the %d most recent. Narrow from/to, filter by name, or use the default daily mode.", len(samples), maxRecords)
		samples = samples[:maxRecords]
	}
	resp.RawSamples = samples
	resp.RecordCount = len(samples)
	return resp
}

// healthValue pulls the numeric value out of a sample, trying the known value
// keys in order.
func healthValue(sample map[string]any) (float64, bool) {
	for _, k := range healthValueKeys {
		if v, ok := sample[k]; ok {
			if f, ok := toFloat(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}