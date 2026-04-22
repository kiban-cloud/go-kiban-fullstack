package session

import (
	"time"

	"github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/commons"
)

const (
	SessionSubjectForgotPassword = "forgot-password"
)

var sessionExpirationTime = time.Duration(24 * time.Hour)
var sessionExpirationTimeExtension = time.Duration(3600 * time.Second)

type Session struct {
	ID        string    `bson:"_id"`
	UserID    string    `bson:"userId"`
	TenantID  string    `bson:"tenantId"`
	CreatedAt time.Time `bson:"createdAt"`
	ExpiresAt time.Time `bson:"expiresAt"`
}

func NewSession(userID, tenantID string) *Session {
	return &Session{
		ID:        commons.NewUUID(),
		UserID:    userID,
		TenantID:  tenantID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(sessionExpirationTime),
	}
}

func (s *Session) ExtendsSessionExpirationTime() {
	s.ExpiresAt = s.ExpiresAt.Add(sessionExpirationTimeExtension)
}
