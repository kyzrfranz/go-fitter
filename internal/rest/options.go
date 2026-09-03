package rest

import (
	"net/url"
	"strconv"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func BuildFindOptions(query url.Values, records bool, laps bool) options.Lister[options.FindOptions] {
	findOptions := options.Find()
	findOptions.SetSort(bson.D{{"start_time", -1}})

	projection := bson.M{
		"sessionSummary": 1,
		"meta":           1,
		"sport":          1,
		"sub_sport":      1,
		"start_time":     1,
		"sportMesg":      1,
		"source":         1,
		"custom":         1,
	}

	if records {
		projection["records"] = 1
	}

	if laps {
		projection["laps"] = 1
	}

	findOptions.SetProjection(projection)

	limitStr := query.Get("limit")
	limit, err := strconv.ParseInt(limitStr, 10, 64)
	if err != nil || limit <= 0 {
		limit = DefaultItemLimit
	}
	findOptions.SetLimit(limit)
	return findOptions
}

func BuildHealthFindOptions(query url.Values) options.Lister[options.FindOptions] {
	opts := options.Find()

	// 1. REMOVED DEFAULT LIMIT
	// We rely on the Date Range ($gte / $lte) in the Filter to bound the data.
	// This ensures we get ALL metrics (Heart Rate AND Weight) for the requested period.

	// Only apply a limit if the frontend explicitly asks for one (e.g. for pagination views)
	if l := query.Get("limit"); l != "" {
		if v, err := strconv.ParseInt(l, 10, 64); err == nil && v > 0 {
			opts.SetLimit(v)
		}
	}

	// 2. Skip (Pagination)
	if s := query.Get("skip"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil && v >= 0 {
			opts.SetSkip(v)
		}
	}

	// 3. Sorting
	// Default: Date Ascending (Oldest -> Newest)
	// Charts usually need chronological order. It is faster to sort in DB than in JS.
	sortKey := "date"
	sortOrder := 1 // 1 = Ascending

	if s := query.Get("sort"); s != "" {
		if s[0] == '-' {
			sortKey = s[1:]
			sortOrder = -1 // Descending
		} else {
			sortKey = s
			sortOrder = 1
		}
	}
	opts.SetSort(bson.D{{Key: sortKey, Value: sortOrder}})

	return opts
}
