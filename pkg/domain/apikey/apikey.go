package apikey

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type ApiKey struct {
	ID        string     `bson:"_id" json:"id"`
	TenantID  string     `bson:"tenantId" json:"tenantId"`
	Name      string     `bson:"name" json:"name"`
	Key       string     `bson:"key" json:"key"`
	Sandbox   bool       `bson:"sandbox" json:"sandbox"`
	CreatedAt *time.Time `bson:"createdAt,omitempty" json:"createdAt,omitempty"`
}

// NewApiKey builds a fresh ApiKey with a random Key value prefixed by
// keyPrefix (pass the separator explicitly, e.g. "dns_").
func NewApiKey(id, tenantID, name, keyPrefix string, sandbox bool) *ApiKey {
	now := time.Now()
	return &ApiKey{
		ID:        id,
		TenantID:  tenantID,
		Name:      name,
		Key:       generateKey(keyPrefix),
		Sandbox:   sandbox,
		CreatedAt: &now,
	}
}

func generateKey(prefix string) string {
	bytes := make([]byte, 32)
	_, _ = rand.Read(bytes)
	return prefix + hex.EncodeToString(bytes)
}
