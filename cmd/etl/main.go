package main

import (
	"context"
	"flag"
	"time"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/args"
	"github.com/kyzrfranz/go-fitter/internal/db"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	mongoUri string
	dbName   string
)

func main() {
	flag.StringVar(&mongoUri, "mongo-uri", args.EnvOrDefault[string]("MONGO_URI", ""), "")
	flag.StringVar(&dbName, "database", args.EnvOrDefault[string]("DATABASE_NAME", "test"), "")

	flag.Parse()

	cli, err := db.NewV1MongoClient(db.WithUri(mongoUri))
	if err != nil {
		panic(err)
	}
	activityRepo := db.NewItemsRepository[v1.Activity](cli, dbName, "activities")
	before := time.Now()

	findOptions := options.Find()
	findOptions.SetSort(bson.D{{"sessionSummary.start_time", -1}})

	projection := bson.M{
		"sessionSummary": 1,
		"meta":           1,
		"sport":          1,
		"custom":         1,
	}

	findOptions.SetProjection(projection)
	findOptions.SetSort(bson.D{{"sessionSummary.start_time", -1}})
	findOptions.SetLimit(1)

	activities, _ := activityRepo.List(context.Background(), bson.M{}, findOptions)

	for _, activity := range activities {
		println(activity.SessionSummary["timestamp"], activity.Meta.Description)
	}
	duration := time.Now().Sub(before)
	println("Duration:", duration.String())
}

func update(activity v1.Activity) error {
	return nil
}
