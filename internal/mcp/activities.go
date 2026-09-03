package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/rest"
)

// maxActivityLimit caps list_activities page size regardless of what the
// caller asks for.
const maxActivityLimit = 200

// activityFieldKeys maps a caller-facing field name to its Mongo projection
// key. This is the allow-list for the `fields` argument on both activity tools.
var activityFieldKeys = map[string]string{
	"id":              "_id",
	"sport":           "sport",
	"sub_sport":       "sub_sport",
	"start_time":      "start_time",
	"meta":            "meta",
	"custom":          "custom",
	"source":          "source",
	"session_summary": "sessionSummary",
	"sport_mesg":      "sportMesg",
	"laps":            "laps",
	"records":         "records",
}

// activityListDefaultFields is the lean projection list_activities returns when
// no `fields` is given: identity + curated summary stats, none of the bulky raw
// FIT message maps.
var activityListDefaultFields = []string{"id", "sport", "sub_sport", "start_time", "meta", "custom", "source"}

// activitySummaryFields is what get_activity returns with with_records=false:
// everything except the per-sample records and laps streams.
var activitySummaryFields = []string{"id", "sport", "sub_sport", "start_time", "meta", "custom", "source", "session_summary", "sport_mesg"}

// projectionFor turns caller-facing field names into a Mongo projection.
// Unknown names are ignored; _id is always kept so results stay addressable.
func projectionFor(names []string) bson.M {
	proj := bson.M{"_id": 1}
	for _, n := range names {
		if key, ok := activityFieldKeys[n]; ok {
			proj[key] = 1
		}
	}
	return proj
}

func registerActivityTools(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("list_activities",
			mcp.WithDescription("List activities, newest first (sorted by start_time desc). Returns summary fields only — no records, laps or raw FIT messages — at roughly 1-2KB per activity. Defaults: limit 50, max 200. Filters mirror the REST /activities endpoint."),
			mcp.WithString("sport", mcp.Description("Exact match (e.g. running, cycling).")),
			mcp.WithString("from", mcp.Description("Inclusive lower bound on start_time. RFC3339 or YYYY-MM-DD.")),
			mcp.WithString("to", mcp.Description("Inclusive upper bound on start_time. RFC3339 or YYYY-MM-DD.")),
			mcp.WithString("title", mcp.Description("Case-insensitive regex match against meta.title.")),
			mcp.WithString("activity_type", mcp.Description("Exact match against meta.activity_type.")),
			mcp.WithNumber("limit", mcp.Description("Page size. Default 50, max 200.")),
			mcp.WithString("fields", mcp.Description("Comma-separated fields to return. Available: id, sport, sub_sport, start_time, meta, custom, source, session_summary, sport_mesg. Defaults to id,sport,sub_sport,start_time,meta,custom,source.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			q := url.Values{}
			for _, k := range []string{"sport", "from", "to", "title", "activity_type"} {
				if v := req.GetString(k, ""); v != "" {
					q.Set(k, v)
				}
			}

			limit := int64(rest.DefaultItemLimit)
			if l := req.GetFloat("limit", 0); l > 0 {
				limit = int64(l)
			}
			if limit > maxActivityLimit {
				limit = maxActivityLimit
			}

			names := splitFields(req.GetString("fields", ""))
			if len(names) == 0 {
				names = activityListDefaultFields
			}

			opts := options.Find().
				SetSort(bson.D{{Key: "start_time", Value: -1}}).
				SetProjection(projectionFor(names)).
				SetLimit(limit)

			activities, total := deps.Activities.List(ctx, rest.BuildActivityFilter(q), opts)

			resp := ActivityListResponse{
				Count:      len(activities),
				MatchCount: int(total),
				Activities: activities,
			}
			if int64(len(activities)) < total {
				resp.Truncated = true
				resp.Warning = fmt.Sprintf("%d activities matched; returning the %d most recent. Raise limit (max %d) or narrow from/to.", total, len(activities), maxActivityLimit)
			}
			return jsonResult(resp, func(v any) ([]byte, error) { return json.Marshal(v) })
		},
	)

	s.AddTool(
		mcp.NewTool("get_activity",
			mcp.WithDescription("Fetch a single activity by id. By default returns summary fields only (~2-5KB) — laps, per-sample records and raw FIT messages are stripped. Set with_records:true to include the full records + laps stream, typically 0.5-10MB depending on duration; for analysis prefer get_activity_series, which downsamples."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Activity id (first 16 hex chars of the .FIT sha256).")),
			mcp.WithBoolean("with_records", mcp.Description("Include the per-sample records and laps streams. Off by default — payload can reach several MB.")),
			mcp.WithString("fields", mcp.Description("Comma-separated fields to return, overriding the default set. Available: id, sport, sub_sport, start_time, meta, custom, source, session_summary, sport_mesg, laps, records.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id := req.GetString("id", "")
			if id == "" {
				return mcp.NewToolResultError("id is required"), nil
			}

			var proj bson.M
			if names := splitFields(req.GetString("fields", "")); len(names) > 0 {
				proj = projectionFor(names)
			} else {
				names := append([]string{}, activitySummaryFields...)
				if req.GetBool("with_records", false) {
					names = append(names, "laps", "records")
				}
				proj = projectionFor(names)
			}

			activity, err := deps.Activities.Get(ctx, id, options.FindOne().SetProjection(proj))
			if err != nil {
				return mcp.NewToolResultErrorFromErr("lookup failed", err), nil
			}
			return jsonResult(activity, func(v any) ([]byte, error) { return json.Marshal(v) })
		},
	)
}

// ActivityListResponse wraps the activity list with the metadata an agent needs
// to know whether more matched than were returned.
type ActivityListResponse struct {
	Count      int           `json:"count"`
	MatchCount int           `json:"match_count"`
	Truncated  bool          `json:"truncated,omitempty"`
	Warning    string        `json:"warning,omitempty"`
	Activities []v1.Activity `json:"activities"`
}