package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/db"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type Activity struct {
	dbClient  db.DatabaseClient[v1.Activity]
	retriever Retriever
	out       io.Writer
	overwrite bool
	blobStore BlobStore
}

func NewActivitySyncer(dbClient db.DatabaseClient[v1.Activity], retriever Retriever, out io.Writer, overwrite bool) *Activity {
	return &Activity{
		dbClient:  dbClient,
		retriever: retriever,
		out:       out,
		overwrite: overwrite,
	}
}

// SetBlobStore enables stashing each activity's raw source bytes in a BlobStore
// and repointing Source.Path at the stored blob. Only takes effect when the
// retriever also implements RawSource. No-op when nil (the default).
func (h *Activity) SetBlobStore(bs BlobStore) { h.blobStore = bs }

func (h *Activity) Sync(workspace string) error {
	zipfiles, err := h.retriever.Retrieve(workspace)
	if err != nil {
		return fmt.Errorf("retrieving activity files: %w", err)
	}

	// Skip already-imported zips to keep the sync cheap as the folder grows.
	// `--overwrite` bypasses the skip because the caller wants to refresh.
	var imported map[string]struct{}
	if !h.overwrite {
		imported, err = h.importedPaths()
		if err != nil {
			fmt.Fprintf(h.out, "warning: could not load imported paths, processing all zips: %v\n", err)
		}
	}

	for _, zipfile := range zipfiles {
		if _, seen := imported[zipfile]; seen {
			fmt.Fprintf(h.out, "skipping already-imported archive: %s\n", zipfile)
			continue
		}

		fmt.Printf("processing archive: %s...\n", zipfile)

		//if not already extracted, extract it
		activityRaw, err := h.retriever.Read(zipfile)
		if err != nil {

			if _, ok := errors.AsType[error](err); ok {
				fmt.Printf("No fit file found in zip %s\n", zipfile)
				continue
			}

			return fmt.Errorf("reading activity file from zip %s: %w", zipfile, err)
		}

		if activityRaw == nil {
			fmt.Printf("No activity file found in zip %s\n", zipfile)
			continue
		}

		// One file can yield several activities: a multisport (triathlon) FIT
		// produces one leg per sport (swim, bike, run).
		var activities []v1.Activity
		if err := json.NewDecoder(activityRaw).Decode(&activities); err != nil {
			return fmt.Errorf("decoding activity file %s: %w", zipfile, err)
		}

		if err := activityRaw.Close(); err != nil {
			return fmt.Errorf("closing activity file %s: %w", zipfile, err)
		}

		for i := range activities {
			h.saveActivity(&activities[i], zipfile)
		}
	}
	return nil
}

// saveActivity stashes an activity's raw source and persists it, handling the
// duplicate-key/overwrite path. It is called once per leg of a source file.
func (h *Activity) saveActivity(activity *v1.Activity, zipfile string) {
	fmt.Fprintf(h.out, "Found '%s' \n", activity.Meta.Title)

	// Stash the raw source bytes so the chart endpoint can rebuild records
	// later (the local file won't exist on the API server), and repoint
	// Source.Path at the stored blob. Only runs when a BlobStore is set and
	// the retriever can surface raw bytes.
	h.stashRawSource(activity, zipfile)

	// Records are kept in transit (for chart-on-demand) but never persisted.
	activity.Records = nil

	created, createErr := h.dbClient.Create(nil, activity)
	if createErr != nil {
		if mongo.IsDuplicateKeyError(createErr) && h.overwrite {
			// Convert struct to bson.M for update
			updateData, err := bson.Marshal(activity)
			if err != nil {
				fmt.Fprintf(h.out, "failed to marshal activity for update: %v\n", err)
				return
			}
			var updateMap bson.M
			if err := bson.Unmarshal(updateData, &updateMap); err != nil {
				fmt.Fprintf(h.out, "failed to unmarshal activity for update: %v\n", err)
				return
			}
			// Remove _id from update map as it's immutable
			delete(updateMap, "_id")

			res, uerr := h.dbClient.Update(context.Background(), activity.Id, bson.M{"$set": updateMap})
			if uerr != nil {
				fmt.Fprintf(h.out, "failed to update activity: %v\n", uerr)
			} else if ur, ok := res.(*mongo.UpdateResult); ok && ur.MatchedCount == 0 {
				// The duplicate wasn't on _id, so updating by _id matched nothing
				// and wrote nothing. Surface the original create error — it names
				// the unique index and the conflicting value, which is exactly
				// what's blocking this leg from being stored.
				fmt.Fprintf(h.out, "failed to persist activity %s: no document with this _id exists, yet create was rejected as a duplicate by another unique index. Original error: %v\n", activity.Id, createErr)
			} else {
				fmt.Fprintf(h.out, "Updated activity with id: %s\n", activity.Id)
			}
		} else {
			fmt.Fprintf(h.out, "failed to create activity: %v", createErr)
		}
	} else {
		fmt.Fprintf(h.out, "Created activity with id: %s\n", created)
	}
}

// stashRawSource uploads the raw source file for activity to the configured
// BlobStore and repoints activity.Source.Path at the returned locator. It is a
// no-op unless a BlobStore is set and the retriever implements RawSource.
func (h *Activity) stashRawSource(activity *v1.Activity, path string) {
	if h.blobStore == nil {
		return
	}
	rs, ok := h.retriever.(RawSource)
	if !ok {
		return
	}
	data, filename, err := rs.ReadRaw(path)
	if err != nil {
		fmt.Fprintf(h.out, "warning: could not read raw source %s for blob store: %v\n", path, err)
		return
	}
	locator, err := h.blobStore.Upload(context.Background(), activity.Id, filename, data)
	if err != nil {
		fmt.Fprintf(h.out, "warning: could not store raw blob for %s: %v\n", activity.Id, err)
		return
	}
	activity.Source.Path = locator
}

func (h *Activity) importedPaths() (map[string]struct{}, error) {
	values, err := h.dbClient.Distinct(context.Background(), "source.path", bson.M{})
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			set[s] = struct{}{}
		}
	}
	return set, nil
}
