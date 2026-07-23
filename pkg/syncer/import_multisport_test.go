package syncer_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/db"
	"github.com/kyzrfranz/go-fitter/internal/retrievers/folder"
	"github.com/kyzrfranz/go-fitter/internal/retrievers/gridfs"
	"github.com/kyzrfranz/go-fitter/pkg/syncer"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestImportMultisportToRealDB is a one-shot importer: it loads a single .zip
// (e.g. the triathlon export in your Downloads) into your real MongoDB, exactly
// the way `cmd/sync --source folder` does — activities collection + GridFS blob
// store so the chart endpoint can rebuild each leg's series later.
//
// It is skipped unless you point it at a file and a database:
//
//	FITTER_TEST_ZIP=/Users/kyzrfranz/Downloads/23330833908.zip \
//	MONGO_URI='mongodb://localhost:27017' \
//	DATABASE_NAME=fitter \
//	go test ./pkg/syncer -run TestImportMultisportToRealDB -v
//
// Re-running is safe: it imports with overwrite=true, so each leg is updated in
// place instead of erroring on its existing id.
func TestImportMultisportToRealDB(t *testing.T) {
	zipPath := os.Getenv("FITTER_TEST_ZIP")
	mongoURI := os.Getenv("MONGO_URI")
	if zipPath == "" || mongoURI == "" {
		t.Skip("set FITTER_TEST_ZIP=/path/to/export.zip and MONGO_URI=mongodb://... to run the real import")
	}
	dbName := os.Getenv("DATABASE_NAME")
	if dbName == "" {
		dbName = "fitter"
	}

	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("FITTER_TEST_ZIP %q not readable: %v", zipPath, err)
	}

	// The folder retriever scans a directory, so isolate the one zip in a temp
	// dir (symlinked) to avoid importing the rest of your Downloads.
	dir := t.TempDir()
	link := filepath.Join(dir, filepath.Base(zipPath))
	if err := os.Symlink(zipPath, link); err != nil {
		t.Fatalf("linking zip into temp dir: %v", err)
	}

	// Real Mongo client + repository, same as cmd/sync.
	client, err := db.NewV1MongoClient(db.WithUri(mongoURI))
	if err != nil {
		t.Fatalf("connecting to mongo: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Disconnect(ctx)
	})

	repo := db.NewItemsRepository[v1.Activity](client, dbName, "activities")

	// Read-only: dump the live indexes so we can see exactly which unique index
	// (if any) rejects a multisport file's legs — they share one file_hash, so a
	// UNIQUE source.file_hash would block every leg after the first.
	coll := client.Database(dbName).Collection("activities")
	logActivityIndexes(t, coll)

	// Opt-in migration: a multisport file's legs share one file_hash, so a UNIQUE
	// source.file_hash index makes storing all of them impossible. Set
	// FITTER_FIX_INDEXES=1 to drop that obsolete unique index through this exact
	// connection (lookups still work via the non-unique source_file_hash index;
	// re-import idempotency is guaranteed by the deterministic per-leg _id).
	if os.Getenv("FITTER_FIX_INDEXES") != "" {
		dropUniqueFileHashIndexes(t, coll)
	}

	// Raw FIT blobs go to the same GridFS bucket the API server reads from.
	const fitBucketName = "fit_files"
	bucket := client.Database(dbName).GridFSBucket(options.GridFSBucket().SetName(fitBucketName))
	store := gridfs.New(bucket)

	retriever := folder.New(-1, "zip")
	sncr := syncer.NewActivitySyncer(repo, retriever, os.Stdout, true /* overwrite */)
	sncr.SetBlobStore(store)

	if err := sncr.Sync(dir); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Report exactly the legs that came from THIS file by querying its content
	// hash — all legs share one file_hash, so this lists swim/bike/run together
	// without pulling in unrelated activities.
	fileHash := fitFileHash(t, zipPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	imported, _ := repo.List(ctx, bson.M{"source.file_hash": fileHash})

	t.Logf("import complete — %d legs from %s (file_hash=%s) now in %q:",
		len(imported), filepath.Base(zipPath), fileHash[:16], dbName)
	for _, a := range imported {
		t.Logf("  id=%s sport=%-9s start=%s title=%q source.path=%s",
			a.Id, a.Sport, a.StartTime.Format(time.RFC3339), a.Meta.Title, a.Source.Path)
	}
	if len(imported) == 0 {
		t.Errorf("no legs found for this file — the import did not persist (check unique indexes on source.file_hash)")
	}

	// Read each leg's blob back through the REAL chart path (gridfs store) and
	// confirm the stored bytes decode to that leg's own records — this catches a
	// stale ZIP blob (chart shows "not a FIT file") or a blob that yields the
	// wrong leg (cycle/run charts looking identical).
	for _, a := range imported {
		rc, err := store.Read(a.Source.Path)
		if err != nil {
			t.Errorf("chart read for %s (%s) failed: %v", a.Id, a.Sport, err)
			continue
		}
		var legs []v1.Activity
		decErr := json.NewDecoder(rc).Decode(&legs)
		rc.Close()
		if decErr != nil {
			t.Errorf("chart decode for %s (%s) failed: %v", a.Id, a.Sport, decErr)
			continue
		}
		records, matched := 0, false
		for i := range legs {
			if legs[i].Id == a.Id {
				records, matched = len(legs[i].Records), true
				break
			}
		}
		t.Logf("series %s (%s): blob decodes to %d legs, matched=%v, records=%d",
			a.Id, a.Sport, len(legs), matched, records)
		if !matched {
			t.Errorf("blob for %s does not contain a leg with that id — chart will fall back to the wrong leg", a.Id)
		}
		if matched && records == 0 {
			t.Errorf("leg %s decoded with 0 records — chart will be empty/identical", a.Id)
		}
	}
}

// logActivityIndexes prints every index on the activities collection (name, key,
// unique) without changing anything, so we can identify what's rejecting a leg.
func logActivityIndexes(t *testing.T, coll *mongo.Collection) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cur, err := coll.Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	var idxs []bson.M
	if err := cur.All(ctx, &idxs); err != nil {
		t.Fatalf("read indexes: %v", err)
	}
	t.Logf("activities has %d indexes:", len(idxs))
	for _, ix := range idxs {
		unique := false
		if u, ok := ix["unique"].(bool); ok {
			unique = u
		}
		t.Logf("  name=%-26v unique=%-5v key=%v", ix["name"], unique, ix["key"])
	}
}

// dropUniqueFileHashIndexes removes every UNIQUE index whose key is on
// source.file_hash. They are incompatible with multisport files (whose legs
// share a file_hash). Key matching is done on the stringified key so it works
// whether the driver decodes it as bson.M or bson.D.
func dropUniqueFileHashIndexes(t *testing.T, coll *mongo.Collection) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cur, err := coll.Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	var idxs []bson.M
	if err := cur.All(ctx, &idxs); err != nil {
		t.Fatalf("read indexes: %v", err)
	}

	dropped := 0
	for _, ix := range idxs {
		unique, _ := ix["unique"].(bool)
		name, _ := ix["name"].(string)
		onFileHash := strings.Contains(fmt.Sprintf("%v", ix["key"]), "source.file_hash")
		if unique && onFileHash {
			t.Logf("FITTER_FIX_INDEXES: dropping obsolete unique index %q on source.file_hash", name)
			if err := coll.Indexes().DropOne(ctx, name); err != nil {
				t.Fatalf("drop index %s: %v", name, err)
			}
			dropped++
		}
	}
	if dropped == 0 {
		t.Logf("FITTER_FIX_INDEXES: no unique source.file_hash index found (nothing to drop)")
	}
}

// fitFileHash reproduces the content hash FitToActivity computes: the sha256 of
// the .fit bytes inside the zip, hex-encoded. It's the activities' source.file_hash.
func fitFileHash(t *testing.T, zipPath string) string {
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
		h := sha256.New()
		if _, err := io.Copy(h, rc); err != nil {
			rc.Close()
			t.Fatalf("hashing fit: %v", err)
		}
		rc.Close()
		return hex.EncodeToString(h.Sum(nil))
	}
	t.Fatalf("no .fit found in %s", zipPath)
	return ""
}
