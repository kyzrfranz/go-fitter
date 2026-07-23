package db

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type itemsV1[T any] struct {
	client     *mongo.Client
	collection *mongo.Collection
}

func NewItemsRepository[T any](client *mongo.Client, dbName, collectionName string) DatabaseClient[T] {
	return itemsV1[T]{
		client:     client,
		collection: client.Database(dbName).Collection(collectionName),
	}
}

func (c itemsV1[T]) List(ctx context.Context, filter bson.M, opts ...options.Lister[options.FindOptions]) ([]T, int64) {
	items, totalCount, err := c.RunFilterAggregation(c.collection, ctx, filter, opts...)
	if err != nil {
		return []T{}, 0
	}
	if items == nil {
		items = []T{}
	}
	return items, totalCount
}

func (c itemsV1[T]) Get(ctx context.Context, id string, opts ...options.Lister[options.FindOneOptions]) (*T, error) {
	item := new(T)

	sr := c.collection.FindOne(ctx, getIdFilter(id), opts...)
	err := sr.Decode(&item)
	if err != nil {
		return nil, err
	}

	return item, nil
}

func (c itemsV1[T]) Create(ctx context.Context, item *T) (interface{}, error) {
	result, err := c.collection.InsertOne(ctx, item)
	if err != nil {
		return nil, err
	}
	return result.InsertedID, nil
}

func (c itemsV1[T]) Update(ctx context.Context, id string, update bson.M) (interface{}, error) {
	result, err := c.collection.UpdateOne(ctx, getIdFilter(id), update)
	return result, err
}

func (c itemsV1[T]) Delete(ctx context.Context, id string) (int64, error) {

	res, err := c.collection.DeleteOne(ctx, getIdFilter(id))
	return res.DeletedCount, err
}

func (c itemsV1[T]) Distinct(ctx context.Context, field string, filter bson.M) ([]any, error) {
	var values []any
	if err := c.collection.Distinct(ctx, field, filter).Decode(&values); err != nil {
		return nil, err
	}
	return values, nil
}

func (c itemsV1[T]) RunFilterAggregation(collection *mongo.Collection, ctx context.Context, filter bson.M, opts ...options.Lister[options.FindOptions]) ([]T, int64, error) {
	totalCount, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	var items []T
	cur, err := collection.Find(ctx, filter, opts...)

	if err != nil {
		return nil, 0, err
	}

	defer cur.Close(ctx)
	for cur.Next(ctx) {
		dto := new(T)
		cur.Decode(&dto)
		items = append(items, *dto)
	}

	if err = cur.Err(); err != nil {
		return nil, 0, err
	}

	return items, totalCount, err
}

func getIdFilter(id string) bson.M {

	return bson.M{"_id": id}
	//oID, err := primitive.ObjectIDFromHex(id)
	//if err == nil {
	//	filter = bson.M{"_id": oID}
	//}
	//
	//return filter
}
