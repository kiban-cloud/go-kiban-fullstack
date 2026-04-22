package commons

import (
	"encoding/hex"

	"github.com/google/uuid"
)

func NewUUID() string {
	id := uuid.New()
	return hex.EncodeToString(id[:])
}
