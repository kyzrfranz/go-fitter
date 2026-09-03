package converters

import (
	"archive/zip"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/profile/untyped/mesgnum"
	"github.com/muktihari/fit/proto"
)

// collector grabs every decoded message so we can inspect the file structure.
type collector struct {
	msgs []proto.Message
}

func (c *collector) OnMesg(m proto.Message) { c.msgs = append(c.msgs, m) }

func fitFromZip(t *testing.T, zipPath string) []byte {
	t.Helper()
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if !strings.EqualFold(filepath.Ext(f.Name), ".fit") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open fit in zip: %v", err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read fit: %v", err)
		}
		return b
	}
	t.Fatalf("no .fit in %s", zipPath)
	return nil
}

// groundTruthSession is what a session message claims, straight from the file.
type groundTruthSession struct {
	idx        int
	sport      string
	startEpoch uint32  // FIT epoch seconds
	elapsed    float64 // seconds
	timer      float64 // seconds
	distance   float64 // meters
	records    int     // records that fall in this session's window
}

// collectGroundTruth decodes the file and reports the session structure plus a
// per-session record count, using the same "latest start <= timestamp" rule the
// converter uses to partition the timeline.
func collectGroundTruth(t *testing.T, fit []byte) (sessions []groundTruthSession, totalRecords int) {
	t.Helper()
	c := &collector{}
	dec := decoder.New(strings.NewReader(string(fit)),
		decoder.WithMesgListener(c),
		decoder.WithBroadcastOnly(),
		decoder.WithBroadcastMesgCopy(),
		decoder.WithIgnoreChecksum(),
	)
	for dec.Next() {
		if _, err := dec.Decode(); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}

	for _, m := range c.msgs {
		if m.Num != mesgnum.Session {
			continue
		}
		s := groundTruthSession{
			idx:        len(sessions),
			sport:      typedef.Sport(m.FieldValueByNum(5).Uint8()).String(),
			startEpoch: m.FieldValueByNum(2).Uint32(),
			elapsed:    float64(m.FieldValueByNum(7).Uint32()) / 1000.0, // scale 1000
			timer:      float64(m.FieldValueByNum(8).Uint32()) / 1000.0, // scale 1000
			distance:   float64(m.FieldValueByNum(9).Uint32()) / 100.0,  // scale 100
		}
		sessions = append(sessions, s)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].startEpoch < sessions[j].startEpoch })

	// Partition record timestamps the same way the converter does.
	var recTs []uint32
	for _, m := range c.msgs {
		if m.Num != mesgnum.Record {
			continue
		}
		recTs = append(recTs, m.FieldValueByNum(253).Uint32()) // timestamp
	}
	totalRecords = len(recTs)
	for _, ts := range recTs {
		owner := 0
		for i := range sessions {
			if ts >= sessions[i].startEpoch {
				owner = i
			}
		}
		sessions[owner].records++
	}
	return sessions, totalRecords
}

// TestMultisportExtraction proves a triathlon FIT extracts every leg, not just
// the first. It uses the session messages as ground truth.
//
//	FITTER_TEST_ZIP=/Users/kyzrfranz/Downloads/23330833908.zip go test ./pkg/converters -run TestMultisportExtraction -v
func TestMultisportExtraction(t *testing.T) {
	zipPath := os.Getenv("FITTER_TEST_ZIP")
	if zipPath == "" {
		t.Skip("set FITTER_TEST_ZIP=/path/to/export.zip")
	}
	fit := fitFromZip(t, zipPath)

	sessions, totalRecords := collectGroundTruth(t, fit)

	// --- Ground truth report ---
	var legSessions []groundTruthSession
	for _, s := range sessions {
		t.Logf("session[%d]: sport=%-10s timer=%8.2fs distance=%9.2fm records=%d",
			s.idx, s.sport, s.timer, s.distance, s.records)
		if s.sport != "transition" {
			legSessions = append(legSessions, s)
		}
	}
	t.Logf("file has %d sessions (%d legs + %d transitions), %d records",
		len(sessions), len(legSessions), len(sessions)-len(legSessions), totalRecords)

	// --- Run the converter ---
	acts, err := FitToActivity(strings.NewReader(string(fit)), []decoder.Option{decoder.WithIgnoreChecksum()})
	if err != nil {
		t.Fatalf("FitToActivity: %v", err)
	}

	// BEFORE (the bug): only sessionMessages[0] survived -> 1 leg.
	// AFTER (the fix): one activity per non-transition session.
	t.Logf("BEFORE fix: 1 leg extracted (%s only)", legSessions[0].sport)
	t.Logf("AFTER  fix: %d legs extracted", len(acts))

	// --- Assert leg count ---
	if len(acts) != len(legSessions) {
		t.Fatalf("leg count: got %d, want %d (swim+bike+run)", len(acts), len(legSessions))
	}

	recordSum := 0
	for i, a := range acts {
		gt := legSessions[i]
		t.Logf("leg[%d]: id=%s sport=%-10s timer=%8.2fs distance=%9.2fm records=%d",
			i, a.Id, a.Sport, getF(a.SessionSummary, "total_timer_time"),
			getF(a.SessionSummary, "total_distance"), len(a.Records))

		// sport matches the session
		if a.Sport != gt.sport {
			t.Errorf("leg[%d] sport: got %q, want %q", i, a.Sport, gt.sport)
		}
		// duration matches the session's total_timer_time (within rounding)
		if got := getF(a.SessionSummary, "total_timer_time"); math.Abs(got-gt.timer) > 0.01 {
			t.Errorf("leg[%d] timer: got %.2f, want %.2f", i, got, gt.timer)
		}
		// distance matches the session's total_distance (within rounding)
		if got := getF(a.SessionSummary, "total_distance"); math.Abs(got-gt.distance) > 0.01 {
			t.Errorf("leg[%d] distance: got %.2f, want %.2f", i, got, gt.distance)
		}
		// records partitioned to this leg match the ground-truth window count
		if len(a.Records) != gt.records {
			t.Errorf("leg[%d] records: got %d, want %d", i, len(a.Records), gt.records)
		}
		recordSum += len(a.Records)
	}

	// No leg's records leak into another: the legs plus the dropped transition
	// records account for exactly every record in the file.
	transitionRecords := 0
	for _, s := range sessions {
		if s.sport == "transition" {
			transitionRecords += s.records
		}
	}
	if recordSum+transitionRecords != totalRecords {
		t.Errorf("record accounting: legs=%d + transitions=%d != total=%d",
			recordSum, transitionRecords, totalRecords)
	}
	t.Logf("records: %d in legs + %d in transitions = %d total (no leaks)",
		recordSum, transitionRecords, totalRecords)
}

func getF(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	default:
		return 0
	}
}
