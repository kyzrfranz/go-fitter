package syncer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/retrievers/folder"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// fakeActivityDB is an in-memory db.DatabaseClient[v1.Activity] so the syncer
// can run without a live MongoDB. It records every created activity.
type fakeActivityDB struct {
	created []*v1.Activity
}

func (f *fakeActivityDB) List(ctx context.Context, filter bson.M, opts ...options.Lister[options.FindOptions]) ([]v1.Activity, int64) {
	return nil, 0
}
func (f *fakeActivityDB) Get(ctx context.Context, id string, opts ...options.Lister[options.FindOneOptions]) (*v1.Activity, error) {
	return nil, nil
}
func (f *fakeActivityDB) Create(ctx context.Context, item *v1.Activity) (interface{}, error) {
	f.created = append(f.created, item)
	return item.Id, nil
}
func (f *fakeActivityDB) Update(ctx context.Context, id string, update bson.M) (interface{}, error) {
	return nil, nil
}
func (f *fakeActivityDB) Delete(ctx context.Context, id string) (int64, error) { return 0, nil }
func (f *fakeActivityDB) Distinct(ctx context.Context, field string, filter bson.M) ([]any, error) {
	// Pretend nothing has been imported yet so the syncer processes every zip.
	return nil, nil
}

// TestSyncActivitiesFromDownloads is a manual, one-shot test: it points the
// folder retriever at a single .zip export and runs the activity syncer against
// an in-memory DB so you can eyeball the result without touching MongoDB.
//
// Run it against one zip in your Downloads folder:
//
//	FITTER_TEST_ZIP="$HOME/Downloads/my-export.zip" go test ./pkg/syncer -run TestSyncActivitiesFromDownloads -v
//
// Without FITTER_TEST_ZIP set, the test is skipped.
func TestSyncActivitiesFromDownloads(t *testing.T) {
	zipPath := os.Getenv("FITTER_TEST_ZIP")
	if zipPath == "" {
		t.Skip("set FITTER_TEST_ZIP=/path/to/export.zip to run this manual sync test")
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("FITTER_TEST_ZIP %q not readable: %v", zipPath, err)
	}

	// The folder retriever scans a directory; isolate the single zip by handing
	// it a temp dir that only contains a link to the requested file.
	dir := t.TempDir()
	link := filepath.Join(dir, filepath.Base(zipPath))
	if err := os.Symlink(zipPath, link); err != nil {
		t.Fatalf("linking zip into temp dir: %v", err)
	}

	retriever := folder.New(-1, "zip")
	db := &fakeActivityDB{}

	syncer := NewActivitySyncer(db, retriever, os.Stdout, false)

	if err := syncer.Sync(dir); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(db.created) == 0 {
		t.Fatalf("expected at least one activity to be created, got 0")
	}
	for _, a := range db.created {
		t.Logf("created activity: id=%s title=%q start=%s", a.Id, a.Meta.Title, a.StartTime)
	}
}
