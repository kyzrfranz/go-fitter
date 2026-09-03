package rest

import (
	"fmt"
	"net/url"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func BuildActivityFilter(query url.Values) bson.M {
	filter := bson.M{}

	if sport := query.Get("sport"); sport != "" {
		filter["sport"] = sport
	}

	addDateFilter := func(field, op, val string) {
		t, err := time.Parse(time.RFC3339, val)
		if err != nil {
			t, err = time.Parse("2006-01-02", val)
		}
		if err != nil {
			fmt.Printf("Invalid date format received: %s\n", val)
			return
		}

		if op == "$lte" {
			t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location())
		}
		if op == "$gte" {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		}

		if existing, ok := filter[field].(bson.M); ok {
			existing[op] = t
			filter[field] = existing
		} else {
			filter[field] = bson.M{op: t}
		}
	}

	if from := query.Get("from"); from != "" {
		addDateFilter("start_time", "$gte", from)
	}

	if to := query.Get("to"); to != "" {
		addDateFilter("start_time", "$lte", to)
	}

	if title := query.Get("title"); title != "" {
		filter["meta.title"] = bson.M{"$regex": title, "$options": "i"}
	}

	if activityType := query.Get("activity_type"); activityType != "" {
		filter["meta.activity_type"] = activityType
	}

	return filter
}

func BuildHealthFilter(query url.Values) bson.M {
	filter := bson.M{}

	// 1. Filter by Metric Name (check "name" then "metric")
	name := query.Get("name")
	if name == "" {
		name = query.Get("metric")
	}

	if name != "" {
		filter["name"] = name
	}

	// 2. Filter by Date Range (from/to)
	// Helper to handle the date range logic
	dateFilter := bson.M{}

	if from := query.Get("from"); from != "" {
		// Parse YYYY-MM-DD
		if t, err := time.Parse("2006-01-02", from); err == nil {
			dateFilter["$gte"] = t
		}
	} else {
		// SAFETY NET: Default to last 7 days to prevent querying 5 years of data.
		// Must be a time.Time, not a string — the `date` field is a BSON Date,
		// and a Date-vs-String $gte comparison matches every document, which
		// silently defeats this guard.
		t := time.Now().AddDate(0, 0, -7)
		dateFilter["$gte"] = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}

	if to := query.Get("to"); to != "" {
		// Parse YYYY-MM-DD
		if t, err := time.Parse("2006-01-02", to); err == nil {
			dateFilter["$lte"] = t
		}
	}

	// Only add the date filter if parameters were provided
	if len(dateFilter) > 0 {
		filter["date"] = dateFilter
	}

	return filter
}
