// Package emaildomain is the Mongo-backed override layer for
// pkg/domain/emaildomain: the per-deployment list of email domains to
// block or allow at signup, on top of the one compiled into the binary.
//
// It lives here because it is identical in every project that gates
// signup this way — same collection, same shape, same query.
package emaildomain

import (
	"context"

	domain_emaildomain "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/emaildomain"
	common "github.com/kiban-cloud/go-kiban-fullstack/pkg/repository/common"

	errorsWrapper "github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// EMAIL_DOMAIN_COLLECTION holds one document per domain, keyed by the
// domain itself so it can be curated by hand:
//
//	{ _id: "novelv.com" }                 → blocked
//	{ _id: "cliente.com", allow: true }   → allowed despite the public list
const EMAIL_DOMAIN_COLLECTION = "emailDomainRules"

type EmailDomainRepository struct {
	collection *mongo.Collection
}

func NewEmailDomainRepository(mongoClient *common.MongoClient) *EmailDomainRepository {
	return &EmailDomainRepository{
		collection: mongoClient.DatabaseCommons.Collection(EMAIL_DOMAIN_COLLECTION),
	}
}

// NewChecker wires the embedded list to this deployment's overrides. It
// is the whole integration surface a project needs: one call at boot.
func NewChecker(mongoClient *common.MongoClient, onOverridesError func(error)) *domain_emaildomain.Checker {
	return domain_emaildomain.NewChecker(NewEmailDomainRepository(mongoClient), onOverridesError)
}

// Load returns the domains this deployment blocks and the ones it allows.
// Called once per signup, which is rare enough that reading it fresh
// beats caching: the point of this list is that adding a domain takes
// effect immediately.
func (r *EmailDomainRepository) Load(ctx context.Context) (blocked, allowed []string, err error) {
	cursor, err := r.collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, nil, errorsWrapper.Wrap(err, "error reading email domain rules")
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var doc struct {
			Domain string `bson:"_id"`
			Allow  bool   `bson:"allow"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, nil, errorsWrapper.Wrap(err, "error decoding email domain rule")
		}
		if doc.Allow {
			allowed = append(allowed, doc.Domain)
			continue
		}
		blocked = append(blocked, doc.Domain)
	}
	if err := cursor.Err(); err != nil {
		return nil, nil, errorsWrapper.Wrap(err, "error iterating email domain rules")
	}
	return blocked, allowed, nil
}
