package apikey

import (
	"context"
	"errors"

	domain_apikey "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/apikey"
	common "github.com/kiban-cloud/go-kiban-fullstack/pkg/repository/common"

	errorsWrapper "github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const APIKEY_COLLECTION = "apikeys"

type ApiKeyRepository struct {
	collection *mongo.Collection
}

func NewApiKeyRepository(mongoClient *common.MongoClient) *ApiKeyRepository {
	collection := mongoClient.DatabaseCommons.Collection(APIKEY_COLLECTION)
	_, err := collection.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.M{"key": 1},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		panic(errorsWrapper.Wrap(err, "error creating apikeys key index"))
	}
	_, err = collection.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.M{"tenantId": 1},
	})
	if err != nil {
		panic(errorsWrapper.Wrap(err, "error creating apikeys tenantId index"))
	}
	return &ApiKeyRepository{collection: collection}
}

func (r *ApiKeyRepository) Insert(ctx context.Context, apiKey *domain_apikey.ApiKey) error {
	_, err := r.collection.InsertOne(ctx, apiKey)
	if err != nil {
		return errorsWrapper.Wrap(err, "error inserting api key")
	}
	return nil
}

func (r *ApiKeyRepository) FindByTenantID(ctx context.Context, tenantID string) ([]*domain_apikey.ApiKey, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"tenantId": tenantID})
	if err != nil {
		return nil, errorsWrapper.Wrap(err, "error finding api keys by tenant id")
	}
	var keys []*domain_apikey.ApiKey
	if err := cursor.All(ctx, &keys); err != nil {
		return nil, errorsWrapper.Wrap(err, "error decoding api keys")
	}
	return keys, nil
}

func (r *ApiKeyRepository) CountByTenantAndSandbox(ctx context.Context, tenantID string, sandbox bool) (int64, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{"tenantId": tenantID, "sandbox": sandbox})
	if err != nil {
		return 0, errorsWrapper.Wrap(err, "error counting api keys")
	}
	return count, nil
}

func (r *ApiKeyRepository) FindByKey(ctx context.Context, key string) (*domain_apikey.ApiKey, error) {
	var apiKey domain_apikey.ApiKey
	err := r.collection.FindOne(ctx, bson.M{"key": key}).Decode(&apiKey)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, errorsWrapper.Wrap(err, "error finding api key")
	}
	return &apiKey, nil
}

func (r *ApiKeyRepository) Delete(ctx context.Context, id string, tenantID string) (bool, error) {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id, "tenantId": tenantID})
	if err != nil {
		return false, errorsWrapper.Wrap(err, "error deleting api key")
	}
	return result.DeletedCount > 0, nil
}
