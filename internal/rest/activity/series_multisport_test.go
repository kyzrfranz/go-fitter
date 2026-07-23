package fit

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/pkg/converters"
	"github.com/muktihari/fit/decoder"
)

// TestSeriesPerLegDistinct reproduces the chart path for a multisport file:
// decode the stored FIT blob into legs (as gridfs.Read does), then for each
// leg's stub run selectLeg + BuildSeries and confirm the legs return DIFFERENT
// series — i.e. the bike chart isn't showing the swim/run data.
//
//	FITTER_TEST_ZIP=/Users/kyzrfranz/Downloads/23330833908.zip go test ./internal/rest/activity -run TestSeriesPerLegDistinct -v
func TestSeriesPerLegDistinct(t *testing.T) {
	zipPath := os.Getenv("FITTER_TEST_ZIP")
	if zipPath == "" {
		t.Skip("set FITTER_TEST_ZIP=/path/to/export.zip")
	}
	fit := fitBytesFromZip(t, zipPath)

	// What the syncer stored as stubs (id + start_time per leg).
	stubs, err := converters.FitToActivity(strings.NewReader(string(fit)), []decoder.Option{decoder.WithIgnoreChecksum()})
	if err != nil {
		t.Fatalf("FitToActivity (stubs): %v", err)
	}
	t.Logf("file produced %d legs", len(stubs))
	for _, s := range stubs {
		t.Logf("  stub id=%s sport=%s start=%s records=%d", s.Id, s.Sport, s.StartTime.Format("15:04:05"), len(s.Records))
	}

	// What gridfs.Read returns for ANY leg's blob: decode the full FIT and
	// marshal every leg to a JSON array.
	blobLegs, err := converters.FitToActivity(strings.NewReader(string(fit)), []decoder.Option{decoder.WithIgnoreChecksum()})
	if err != nil {
		t.Fatalf("FitToActivity (blob): %v", err)
	}
	blobJSON, err := json.Marshal(blobLegs)
	if err != nil {
		t.Fatalf("marshal blob legs: %v", err)
	}

	fields := []string{"time", "hr", "power", "altitude"}
	seriesFor := func(stub *v1.Activity) SeriesResponse {
		var legs []v1.Activity
		if err := json.Unmarshal(blobJSON, &legs); err != nil {
			t.Fatalf("unmarshal legs: %v", err)
		}
		full := selectLeg(legs, stub.Id, stub.StartTime)
		t.Logf("request id=%s -> selectLeg picked id=%s sport=%s records=%d",
			stub.Id, full.Id, full.Sport, len(full.Records))
		return BuildSeries(full.Records, stub.StartTime, fields)
	}

	// Find the cycling and running legs.
	var cycle, run *v1.Activity
	for _, s := range stubs {
		switch s.Sport {
		case "cycling":
			cycle = s
		case "running":
			run = s
		}
	}
	if cycle == nil || run == nil {
		t.Fatalf("expected cycling and running legs, got sports %v", sportsOf(stubs))
	}

	cs := seriesFor(cycle)
	rs := seriesFor(run)

	if cs.SampleCount == rs.SampleCount {
		t.Errorf("cycle and run have identical sample counts (%d) — likely the same leg", cs.SampleCount)
	}
	if fmt.Sprintf("%v", cs.Data) == fmt.Sprintf("%v", rs.Data) {
		t.Errorf("cycle and run series data are IDENTICAL — selectLeg returned the wrong leg")
	}
	t.Logf("cycle: samples=%d  run: samples=%d", cs.SampleCount, rs.SampleCount)
}

func sportsOf(acts []*v1.Activity) []string {
	out := make([]string, len(acts))
	for i, a := range acts {
		out[i] = a.Sport
	}
	return out
}

func fitBytesFromZip(t *testing.T, zipPath string) []byte {
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
			t.Fatalf("open fit: %v", err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read fit: %v", err)
		}
		return b
	}
	t.Fatalf("no .fit in zip")
	return nil
}
