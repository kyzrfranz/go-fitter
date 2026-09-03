package mcp

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/kyzrfranz/go-fitter/internal/rest"
)

// MetricVolume is the per-metric count summary describe_data_volume returns.
// DayCount is the number of day-buckets stored — not individual samples.
type MetricVolume struct {
	Metric    string `json:"metric"`
	Units     string `json:"units,omitempty"`
	DayCount  int    `json:"day_count"`
	FirstDate string `json:"first_date,omitempty"`
	LastDate  string `json:"last_date,omitempty"`
}

// DataVolumeResponse is a payload-free overview of what a date range holds, so
// an agent can decide how to filter before pulling the real data.
type DataVolumeResponse struct {
	Range struct {
		From string `json:"from,omitempty"`
		To   string `json:"to,omitempty"`
	} `json:"range"`
	Activities struct {
		Total   int            `json:"total"`
		BySport map[string]int `json:"by_sport"`
	} `json:"activities"`
	Health struct {
		TotalDayBuckets int            `json:"total_day_buckets"`
		Metrics         []MetricVolume `json:"metrics"`
	} `json:"health"`
	Note string `json:"note"`
}

func registerDescribeTool(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("describe_data_volume",
			mcp.WithDescription("Return record counts for activities and health metrics in a date range WITHOUT any payload (~1-5KB). Call this first for wide or open-ended ranges to decide how to filter or page list_activities / list_health_metrics. Activities are unbounded when from/to are omitted; health defaults to the last 7 days."),
			mcp.WithString("from", mcp.Description("Inclusive lower bound on date (YYYY-MM-DD).")),
			mcp.WithString("to", mcp.Description("Inclusive upper bound on date (YYYY-MM-DD).")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			q := url.Values{}
			for _, k := range []string{"from", "to"} {
				if v := req.GetString(k, ""); v != "" {
					q.Set(k, v)
				}
			}

			var resp DataVolumeResponse
			resp.Range.From = q.Get("from")
			resp.Range.To = q.Get("to")
			resp.Activities.BySport = map[string]int{}
			resp.Health.Metrics = []MetricVolume{}
			resp.Note = "Health day_count is the number of stored day-buckets per metric, not individual samples; each bucket can hold many samples. Use list_health_metrics for values."

			// Activities: project just sport so the scan stays tiny; List still
			// reports the true total even when items are capped.
			actOpts := options.Find().
				SetProjection(bson.M{"sport": 1, "start_time": 1}).
				SetLimit(10000)
			activities, actTotal := deps.Activities.List(ctx, rest.BuildActivityFilter(q), actOpts)
			resp.Activities.Total = int(actTotal)
			for _, a := range activities {
				sport := a.Sport
				if sport == "" {
					sport = "unknown"
				}
				resp.Activities.BySport[sport]++
			}

			// Health: project name/date/units only — never the samples array — so
			// this carries no payload.
			healthOpts := options.Find().
				SetProjection(bson.M{"name": 1, "date": 1, "units": 1}).
				SetLimit(100000)
			health, _ := deps.Health.List(ctx, rest.BuildHealthFilter(q), healthOpts)

			byMetric := map[string]*MetricVolume{}
			for _, h := range health {
				m := byMetric[h.Name]
				if m == nil {
					m = &MetricVolume{Metric: h.Name, Units: h.Units}
					byMetric[h.Name] = m
				}
				m.DayCount++
				resp.Health.TotalDayBuckets++

				day := h.Date.UTC().Format("2006-01-02")
				if m.FirstDate == "" || day < m.FirstDate {
					m.FirstDate = day
				}
				if day > m.LastDate {
					m.LastDate = day
				}
			}
			for _, m := range byMetric {
				resp.Health.Metrics = append(resp.Health.Metrics, *m)
			}
			sort.Slice(resp.Health.Metrics, func(i, j int) bool {
				return resp.Health.Metrics[i].Metric < resp.Health.Metrics[j].Metric
			})

			return jsonResult(resp, func(v any) ([]byte, error) { return json.Marshal(v) })
		},
	)
}