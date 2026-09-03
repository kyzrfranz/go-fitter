// Package gridfs persists raw FIT bytes in a MongoDB GridFS bucket and serves
// them back as decoded activity JSON. It exists because activities imported
// from a local Garmin export have no source file reachable from the API server;
// stashing the raw FIT in the database lets /activities/{id}/series rebuild the
// per-record series on demand, exactly like the dropbox/folder retrievers do
// for their own sources.
package gridfs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/kyzrfranz/go-fitter/pkg/converters"
	"github.com/muktihari/fit/decoder"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Scheme prefixes a Source.Path that points at a FIT blob in GridFS, e.g.
// "gridfs://a1b2c3d4...". The id after the scheme is the activity id, which is
// also the GridFS file id.
const Scheme = "gridfs://"

// PathFor returns the Source.Path locator for an activity id.
func PathFor(id string) string { return Scheme + id }

// Handles reports whether a Source.Path points at a GridFS blob.
func Handles(path string) bool { return strings.HasPrefix(path, Scheme) }

// RawReader is the read side the chart endpoint needs; the dropbox retriever
// satisfies it and is used as the Dispatcher fallback.
type RawReader interface {
	Read(path string) (io.ReadCloser, error)
}

// Store reads and writes raw FIT blobs in a single GridFS bucket.
type Store struct {
	bucket *mongo.GridFSBucket
}

func New(bucket *mongo.GridFSBucket) *Store {
	return &Store{bucket: bucket}
}

// Upload stores raw FIT bytes under id and returns the Source.Path locator that
// addresses them. It is idempotent: any existing blob with the same id is
// removed first so re-running an import doesn't collide on the unique files _id.
func (s *Store) Upload(ctx context.Context, id, filename string, data []byte) (string, error) {
	_ = s.bucket.Delete(ctx, id) // ignore "file not found" on first import
	if err := s.bucket.UploadFromStreamWithID(ctx, id, filename, bytes.NewReader(data)); err != nil {
		return "", err
	}
	return PathFor(id), nil
}

// Read implements RawReader for the chart endpoint. path must be "gridfs://<id>".
// It downloads the raw FIT, decodes it with records and returns activity JSON,
// matching the contract of the dropbox/folder retrievers.
func (s *Store) Read(path string) (io.ReadCloser, error) {
	id := strings.TrimPrefix(path, Scheme)

	var raw bytes.Buffer
	if _, err := s.bucket.DownloadToStream(context.Background(), id, &raw); err != nil {
		return nil, err
	}

	activities, err := converters.FitToActivity(&raw, []decoder.Option{decoder.WithIgnoreChecksum()})
	if err != nil {
		return nil, err
	}
	// Return all legs as a JSON array; the chart endpoint selects the leg that
	// matches the requested activity by its start time.
	activityBytes, err := json.Marshal(activities)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(activityBytes)), nil
}

// Dispatcher routes Read calls to GridFS for gridfs:// paths and to a fallback
// retriever (e.g. dropbox) for everything else, so a single RawReader can serve
// both locally-imported and dropbox-sourced activities.
type Dispatcher struct {
	store    *Store
	fallback RawReader
}

func NewDispatcher(store *Store, fallback RawReader) *Dispatcher {
	return &Dispatcher{store: store, fallback: fallback}
}

func (d *Dispatcher) Read(path string) (io.ReadCloser, error) {
	if Handles(path) {
		return d.store.Read(path)
	}
	if d.fallback == nil {
		return nil, fmt.Errorf("no retriever configured for source path %q", path)
	}
	return d.fallback.Read(path)
}
