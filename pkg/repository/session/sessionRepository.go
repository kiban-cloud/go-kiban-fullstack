package session

import (
	"context"
	"errors"
	"time"

	domain_session "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/session"
	common "github.com/kiban-cloud/go-kiban-fullstack/pkg/repository/common"

	errorsWrapper "github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

const SESSION_COLLECTION = "sessions"

type SessionRepository struct {
	collection *mongo.Collection
}

func NewSessionRepository(mongoClient *common.MongoClient) *SessionRepository {
	return &SessionRepository{
		collection: mongoClient.DatabaseCommons.Collection(SESSION_COLLECTION),
	}
}

func (r *SessionRepository) FindBySessionID(ctx context.Context, sessionID string) (*domain_session.Session, error) {
	var doc domain_session.Session
	err := r.collection.FindOne(ctx, bson.M{"_id": sessionID}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, errorsWrapper.Wrap(err, "error when trying to find session by session id")
	}
	return &doc, nil
}

func (r *SessionRepository) InsertSession(ctx context.Context, session *domain_session.Session) error {
	_, err := r.collection.InsertOne(ctx, session)
	if err != nil {
		return errorsWrapper.Wrap(err, "error when trying to insert session")
	}
	return nil
}

func (r *SessionRepository) UpdateExpiresAt(ctx context.Context, sessionID string, expiresAt time.Time) (bool, error) {
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": sessionID}, bson.M{"$set": bson.M{"expiresAt": expiresAt}})
	if err != nil {
		return false, errorsWrapper.Wrap(err, "error when trying to update expires at")
	}
	return result.MatchedCount > 0, nil
}

func (r *SessionRepository) DeleteSessionByUserID(ctx context.Context, userID string) (bool, error) {
	result, err := r.collection.DeleteMany(ctx, bson.M{"userId": userID})
	if err != nil {
		return false, errorsWrapper.Wrap(err, "error when trying to delete sessions by user id")
	}
	return result.DeletedCount > 0, nil
}

func (r *SessionRepository) DeleteSessionsByTenantID(ctx context.Context, tenantID string) (bool, error) {
	result, err := r.collection.DeleteMany(ctx, bson.M{"tenantId": tenantID})
	if err != nil {
		return false, errorsWrapper.Wrap(err, "error when trying to delete sessions by tenant id")
	}
	return result.DeletedCount > 0, nil
}
