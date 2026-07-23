package db

import (
	"context"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type DatabaseClient[Item any] interface {
	List(ctx context.Context, filter bson.M, opts ...options.Lister[options.FindOptions]) ([]Item, int64)
	Get(ctx context.Context, id string, opts ...options.Lister[options.FindOneOptions]) (*Item, error)
	Create(ctx context.Context, item *Item) (interface{}, error)
	Update(ctx context.Context, id string, update bson.M) (interface{}, error)
	Delete(ctx context.Context, id string) (int64, error)
	Distinct(ctx context.Context, field string, filter bson.M) ([]any, error)
}

type MongoCLient struct {
	httpClient     *http.Client
	baseURL        string
	dbName         string
	collectionName string
	username       string
	password       string
	baseUrl        string
}
