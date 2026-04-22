package common

import (
	"context"
	"fmt"
	"sync"
	"time"

	errorsWrapper "github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	DATABASE_NAME_COMMONS = "commons"
	TenantIDKey           = "tenantId"
	CreatedAtKey          = "createdAt"
	UpdatedAtKey          = "updatedAt"
)

type MongoClient struct {
	databasePrefix         string
	MongoClient            *mongo.Client
	DatabaseCommons        *mongo.Database
	databasesCacheByTenant sync.Map
}

func NewMongoClient(mongoURI, databasePrefix string) (*MongoClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	clientOptions := options.Client().ApplyURI(mongoURI)

	client, err := mongo.Connect(clientOptions)
	if err != nil {
		return nil, errorsWrapper.Wrap(err, "error when trying to connect to MongoDB")
	}

	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return nil, errorsWrapper.Wrap(err, "error when trying to ping MongoDB")
	}

	db := client.Database(fmt.Sprintf("%s_%s", databasePrefix, DATABASE_NAME_COMMONS))

	return &MongoClient{
		MongoClient:     client,
		DatabaseCommons: db,
		databasePrefix:  databasePrefix,
	}, nil
}

func (m *MongoClient) GetDatabase(ctx context.Context, tenantID string) (*mongo.Database, error) {
	if value, ok := m.databasesCacheByTenant.Load(tenantID); ok {
		return value.(*mongo.Database), nil
	}

	dbName := m.getDatabaseName(tenantID)
	db := m.MongoClient.Database(dbName)

	actual, _ := m.databasesCacheByTenant.LoadOrStore(tenantID, db)
	return actual.(*mongo.Database), nil
}

func (m *MongoClient) getDatabaseName(tenantID string) string {
	return fmt.Sprintf("%s_%s", m.databasePrefix, tenantID)
}

func (mc *MongoClient) Close(ctx context.Context) error {
	return mc.MongoClient.Disconnect(ctx)
}
