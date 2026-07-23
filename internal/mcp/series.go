package mcp

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	restActivity "github.com/kyzrfranz/go-fitter/internal/rest/activity"
)

// defaultResolutionSeconds is the bucket size get_activity_series averages into
// when the caller doesn't specify one.
const defaultResolutionSeconds = 10

func registerSeriesTool(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("get_activity_series",
			mcp.WithDescription("Return a downsampled time-series projection of an activity, re-read from the raw .FIT. resolution_seconds buckets samples and averages them: the default 10s yields ~360 points for a 1h activity (~20-40KB across the default fields). resolution_seconds of 1 or 0 returns full per-second resolution, which can be 200KB-1MB+ for long activities — only request it when you need fine detail."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Activity id.")),
			mcp.WithString("fields", mcp.Description("Comma-separated field list. Defaults to time,hr,power,cadence,speed,altitude.")),
			mcp.WithNumber("resolution_seconds", mcp.Description("Bucket size in seconds; samples per bucket are averaged. Default 10. Use 1 or 0 for full per-second resolution.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if deps.Retriever == nil {
				return mcp.NewToolResultError("no retriever configured"), nil
			}
			id := req.GetString("id", "")
			if id == "" {
				return mcp.NewToolResultError("id is required"), nil
			}

			stubOpts := options.FindOne().SetProjection(bson.M{
				"source":     1,
				"start_time": 1,
			})
			stub, err := deps.Activities.Get(ctx, id, stubOpts)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("lookup failed", err), nil
			}
			if stub.Source.Path == "" {
				return mcp.NewToolResultError("activity has no source.path"), nil
			}

			raw, err := deps.Retriever.Read(stub.Source.Path)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("retriever read failed", err), nil
			}
			defer raw.Close()

			var full v1.Activity
			if err := json.NewDecoder(raw).Decode(&full); err != nil {
				return mcp.NewToolResultErrorFromErr("decode failed", err), nil
			}

			fields := restActivity.ParseFields(req.GetString("fields", ""))
			resp := restActivity.BuildSeries(full.Records, stub.StartTime, fields)
			resp.Id = id

			// GetFloat returns the default only when the argument is absent, so
			// an explicit 0 (full resolution) still comes through as 0.
			resolution := int(req.GetFloat("resolution_seconds", defaultResolutionSeconds))
			if resolution > 1 {
				resp = downsampleSeries(resp, resolution)
			}
			return jsonResult(resp, func(v any) ([]byte, error) { return json.Marshal(v) })
		},
	)
}

// downsampleSeries collapses a per-sample series into fixed-width time buckets,
// averaging each field over the samples that fall in a bucket. Records arrive
// in chronological order, so a single pass with a running accumulator is
// enough. The bucketed `time` value is the bucket's start (seconds from start).
func downsampleSeries(in restActivity.SeriesResponse, res int) restActivity.SeriesResponse {
	times := in.Data["time"]
	if len(times) == 0 || res <= 1 {
		return in
	}

	out := restActivity.SeriesResponse{
		Id:        in.Id,
		StartTime: in.StartTime,
		Fields:    in.Fields,
		Data:      make(map[string][]any, len(in.Fields)),
	}
	for _, f := range in.Fields {
		out.Data[f] = make([]any, 0, len(times)/res+1)
	}

	sums := make(map[string]float64, len(in.Fields))
	counts := make(map[string]int, len(in.Fields))
	curBucket := -1

	flush := func(bucket int) {
		if bucket < 0 {
			return
		}
		out.Data["time"] = append(out.Data["time"], float64(bucket*res))
		for _, f := range in.Fields {
			if f == "time" {
				continue
			}
			if counts[f] > 0 {
				out.Data[f] = append(out.Data[f], sums[f]/float64(counts[f]))
			} else {
				out.Data[f] = append(out.Data[f], nil)
			}
		}
	}

	for i := range times {
		t, ok := toFloat(times[i])
		if !ok {
			continue
		}
		bucket := int(t) / res
		if bucket != curBucket {
			flush(curBucket)
			curBucket = bucket
			for k := range sums {
				delete(sums, k)
				delete(counts, k)
			}
		}
		for _, f := range in.Fields {
			if f == "time" {
				continue
			}
			if v, ok := toFloat(in.Data[f][i]); ok {
				sums[f] += v
				counts[f]++
			}
		}
	}
	flush(curBucket)

	out.SampleCount = len(out.Data["time"])
	return out
}