package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const jsonDateLayout = "2006-01-02 15:04:05 -0700"

type Health struct {
	bulkWriter BulkWriter
	retriever  Retriever
	out        io.Writer
}

func NewHealthSyncer(bulkWriter BulkWriter, retriever Retriever, out io.Writer) *Health {
	return &Health{
		bulkWriter: bulkWriter,
		retriever:  retriever,
		out:        out,
	}
}

func (h *Health) Sync(workspace string) error {
	healthFiles, err := h.retriever.Retrieve(workspace)
	if err != nil {
		return fmt.Errorf("retrieving health files: %w", err)
	}

	for _, healthFile := range healthFiles {
		fmt.Printf("Processing %s...\n", healthFile)
		f, err := h.retriever.Read(healthFile)
		if err != nil {
			return fmt.Errorf("reading health file %s: %w", healthFile, err)
		}

		var healthWrapper struct {
			Data struct {
				Metrics []struct {
					Name  string                   `json:"name"`
					Units string                   `json:"units"`
					Data  []map[string]interface{} `json:"data"`
				} `json:"metrics"`
			} `json:"data"`
		}

		if err := json.NewDecoder(f).Decode(&healthWrapper); err != nil {
			return fmt.Errorf("decoding health file %s: %w", healthFile, err)
		}

		err = f.Close()
		if err != nil {
			return fmt.Errorf("closing health file %s: %w", healthFile, err)
		}

		var models []mongo.WriteModel

		for _, metric := range healthWrapper.Data.Metrics {
			if len(metric.Data) == 0 {
				continue
			}

			// --- LOGIC FIX: In-Memory Bucketing ---
			// We cannot assume the whole array belongs to one day.
			// We must group samples by their actual timestamp first.

			// Map: UnixTimestamp (Midnight) -> List of Samples
			dayBuckets := make(map[int64][]map[string]interface{})

			for _, sample := range metric.Data {
				dateStr, ok := sample["date"].(string)
				if !ok {
					continue
				}

				t, err := time.Parse(jsonDateLayout, dateStr)
				if err != nil {
					continue
				}

				// 1. Normalize sample date to Midnight UTC (The Bucket Key)
				dayKey := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)

				// 2. Convert string date in sample to real Date object (better for DB)
				sample["date"] = t

				// 3. Add to specific day bucket
				dayBuckets[dayKey.Unix()] = append(dayBuckets[dayKey.Unix()], sample)
			}

			// --- Create Write Operations per Day Bucket ---
			for timestamp, samples := range dayBuckets {
				dayDate := time.Unix(timestamp, 0).UTC()

				// Filter: Update the document for THIS metric on THIS specific day
				filter := bson.M{
					"name": metric.Name,
					"date": dayDate,
				}

				// Update:
				// - $setOnInsert: If creating for the first time, set metadata
				// - $addToSet + $each: Append distinct samples (prevents duplicates if script reruns)
				update := bson.M{
					"$setOnInsert": bson.M{
						"name":  metric.Name,
						"units": metric.Units,
						"date":  dayDate,
					},
					"$addToSet": bson.M{
						"samples": bson.M{
							"$each": samples,
						},
					},
					"$set": bson.M{
						"last_updated": time.Now(),
					},
				}

				model := mongo.NewUpdateOneModel().
					SetFilter(filter).
					SetUpdate(update).
					SetUpsert(true)

				models = append(models, model)
			}
		}

		//// Execute Batch
		if len(models) > 0 {
			opts := options.BulkWrite().SetOrdered(false)
			res, err := h.bulkWriter.BulkWrite(context.Background(), models, opts)
			if err != nil {
				return fmt.Errorf("executing bulk write for health file %s: %w", healthFile, err)
			}
			if res != nil {
				fmt.Fprintf(h.out, "  -> Metric/Days touched: %d (Upserts: %d)\n", res.MatchedCount+res.UpsertedCount, res.UpsertedCount)
			}
		}
	}
	return nil
}
