package converters

import (
	"bytes"
	"testing"
	"time"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"
)

// buildRepeatedSportFIT crafts a minimal multisport FIT with two same-sport
// (running) sessions back to back, each with its own records and lap. This is
// the duathlon/brick/aquathlon shape that a <filehash>-<sport> id scheme would
// collide on.
func buildRepeatedSportFIT(t *testing.T) []byte {
	t.Helper()

	base := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	var msgs []proto.Message

	msgs = append(msgs, mesgdef.NewFileId(nil).
		SetType(typedef.FileActivity).
		ToMesg(nil))

	// session is built from leg's [start, start+dur); records every second.
	addLeg := func(start time.Time, dur time.Duration, distM float64) {
		var t0 time.Time = start
		count := int(dur.Seconds())
		for i := 0; i < count; i++ {
			rec := mesgdef.NewRecord(nil).
				SetTimestamp(t0.Add(time.Duration(i) * time.Second)).
				SetHeartRate(uint8(140 + i%10)).
				SetSpeedScaled(3.0) // moving, so swim/run moving-time logic is exercised
			msgs = append(msgs, rec.ToMesg(nil))
		}
		msgs = append(msgs, mesgdef.NewLap(nil).
			SetStartTime(start).
			SetTotalTimerTime(uint32(dur.Seconds()*1000)).
			SetTotalDistance(uint32(distM*100)).
			ToMesg(nil))
		msgs = append(msgs, mesgdef.NewSession(nil).
			SetStartTime(start).
			SetSport(typedef.SportRunning).
			SetTotalElapsedTime(uint32(dur.Seconds()*1000)).
			SetTotalTimerTime(uint32(dur.Seconds()*1000)).
			SetTotalDistance(uint32(distM*100)).
			ToMesg(nil))
	}

	// Leg 1: 60s / 200m run. Leg 2: 90s / 350m run, starting right after.
	addLeg(base, 60*time.Second, 200)
	addLeg(base.Add(60*time.Second), 90*time.Second, 350)

	fit := &proto.FIT{Messages: msgs}
	var buf bytes.Buffer
	if err := encoder.New(&buf).Encode(fit); err != nil {
		t.Fatalf("encode fixture FIT: %v", err)
	}
	return buf.Bytes()
}

// TestRepeatedSportLegsDistinctIds proves that a multisport file repeating a
// sport produces one activity per leg with distinct ids and its own records, so
// neither leg overwrites the other on persist.
func TestRepeatedSportLegsDistinctIds(t *testing.T) {
	fit := buildRepeatedSportFIT(t)

	acts, err := FitToActivity(bytes.NewReader(fit), nil)
	if err != nil {
		t.Fatalf("FitToActivity: %v", err)
	}

	if len(acts) != 2 {
		t.Fatalf("leg count: got %d, want 2", len(acts))
	}

	// Distinct ids.
	if acts[0].Id == acts[1].Id {
		t.Fatalf("legs share an id %q — second would overwrite the first", acts[0].Id)
	}
	for i, a := range acts {
		t.Logf("leg[%d]: id=%s sport=%s records=%d distance=%.0f",
			i, a.Id, a.Sport, len(a.Records), getF(a.SessionSummary, "total_distance"))
		if a.Sport != "running" {
			t.Errorf("leg[%d] sport: got %q, want running", i, a.Sport)
		}
	}

	// Each leg kept its own records, partitioned by time window (60 vs 90).
	if len(acts[0].Records) != 60 {
		t.Errorf("leg[0] records: got %d, want 60", len(acts[0].Records))
	}
	if len(acts[1].Records) != 90 {
		t.Errorf("leg[1] records: got %d, want 90", len(acts[1].Records))
	}

	// Distances confirm the right session attached to the right leg.
	if d := getF(acts[0].SessionSummary, "total_distance"); d != 200 {
		t.Errorf("leg[0] distance: got %.0f, want 200", d)
	}
	if d := getF(acts[1].SessionSummary, "total_distance"); d != 350 {
		t.Errorf("leg[1] distance: got %.0f, want 350", d)
	}
}