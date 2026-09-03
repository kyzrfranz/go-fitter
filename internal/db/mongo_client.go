package db

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type v1MongoClient struct {
	uri            string
	databaseName   string
	collectionName string
	collection     *mongo.Collection
}

func NewV1MongoClient(opts ...DatabaseClientOption) (*mongo.Client, error) {

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	offc := &v1MongoClient{}
	for _, opt := range opts {
		opt(offc)
	}

	client, err := mongo.Connect(options.Client().ApplyURI(offc.uri))
	if err != nil {
		return nil, err
	}

	err = client.Ping(ctx, readpref.Primary())

	if err != nil {
		return nil, err
	}

	return client, nil
}
