package db

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type HealthMetric struct {
	ID    bson.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Name  string        `bson:"name" json:"name"`   // e.g. "heart_rate", "step_count"
	Units string        `bson:"units" json:"units"` // e.g. "count/min", "kg"

	Date time.Time `bson:"date" json:"date"`

	Samples []map[string]any `bson:"samples" json:"samples"`

	LastUpdated time.Time `bson:"last_updated" json:"last_updated"`
}
