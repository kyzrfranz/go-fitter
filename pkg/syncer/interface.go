package syncer

import (
	"context"
	"io"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Retriever interface {
	Retrieve(path string) ([]string, error)
	Read(string) (io.ReadCloser, error)
}

// RawSource is an optional capability of a Retriever: hand back the raw,
// undecoded source bytes (and base filename) for a path so they can be stashed
// in a BlobStore. Retrievers that can't provide raw bytes simply don't
// implement it.
type RawSource interface {
	ReadRaw(path string) (data []byte, filename string, err error)
}

// BlobStore persists the raw source bytes for an activity, keyed by activity id,
// and returns a Source.Path locator that addresses them. It lets the chart
// endpoint regenerate an activity's series without the original file being
// reachable from the API server.
type BlobStore interface {
	Upload(ctx context.Context, id, filename string, data []byte) (locator string, err error)
}

type BulkWriter interface {
	BulkWrite(ctx context.Context, models []mongo.WriteModel, opts ...options.Lister[options.BulkWriteOptions]) (*mongo.BulkWriteResult, error)
}

type CreateWriter interface {
	Create(context.Context, interface{}) (interface{}, error)
}
