package mcp

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// maxRecords caps every MCP tool response. Agents pull data to reason over it,
// not to stream megabytes — anything past this is truncated and flagged in the
// payload so the caller knows to narrow its from/to window.
const maxRecords = 1000

// toFloat coerces the numeric types BSON and JSON decoding can produce into a
// float64. The bool reports whether v was numeric at all, so callers can tell
// a real zero apart from a missing field.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// splitFields parses a comma-separated `fields` argument into a normalized,
// lower-cased list with blanks dropped.
func splitFields(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// asTime extracts a time.Time from a value decoded out of Mongo, covering the
// native time.Time, the driver's bson.DateTime, and the string layouts the
// health export uses.
func asTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case bson.DateTime:
		return t.Time()
	case string:
		if p, err := time.Parse(time.RFC3339, t); err == nil {
			return p
		}
		if p, err := time.Parse("2006-01-02 15:04:05 -0700", t); err == nil {
			return p
		}
	}
	return time.Time{}
}