package middleware

import (
	"context"
	"net/http"
	"strings"

	http_errors "github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/http/errors"

	"github.com/gin-gonic/gin"
)

// ApiKeyData is the minimal info the middleware extracts from an API key.
type ApiKeyData struct {
	TenantID string
	Sandbox  bool
}

// ApiKeyLookup retrieves an API key by its value. Return a nil *ApiKeyData
// (with nil error) when the key does not exist — the middleware will respond
// 401. An error is treated as a real lookup failure (500).
type ApiKeyLookup func(ctx context.Context, key string) (*ApiKeyData, error)

// TenantEnricher verifies the tenant referenced by the API key exists and
// returns a new context enriched with tenant and sandbox information.
// Returning an error causes the middleware to respond 401.
type TenantEnricher func(ctx context.Context, tenantID string, sandbox bool) (context.Context, error)

type ApiKeyMiddleware struct {
	lookup   ApiKeyLookup
	enricher TenantEnricher
}

func NewApiKeyMiddleware(lookup ApiKeyLookup, enricher TenantEnricher) *ApiKeyMiddleware {
	return &ApiKeyMiddleware{lookup: lookup, enricher: enricher}
}

func (m *ApiKeyMiddleware) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("x-api-key")
		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, http_errors.ErrorResponse{
				Message: "x-api-key header is required",
				Code:    http_errors.ERROR_CODE_UNAUTHORIZED,
			})
			return
		}

		apiKey, err := m.lookup(c.Request.Context(), key)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, http_errors.ErrorResponse{
				Message: "error validating api key",
				Code:    http_errors.ERROR_CODE_INTERNAL_SERVER_ERROR,
			})
			return
		}

		if apiKey == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, http_errors.ErrorResponse{
				Message: "invalid api key",
				Code:    http_errors.ERROR_CODE_UNAUTHORIZED,
			})
			return
		}

		isSandboxHost := strings.HasPrefix(c.Request.Host, "sandbox.")
		if isSandboxHost != apiKey.Sandbox {
			c.AbortWithStatusJSON(http.StatusUnauthorized, http_errors.ErrorResponse{
				Message: "api key environment does not match host",
				Code:    http_errors.ERROR_CODE_UNAUTHORIZED,
			})
			return
		}

		newCtx, err := m.enricher(c.Request.Context(), apiKey.TenantID, isSandboxHost)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, http_errors.ErrorResponse{
				Message: "tenant not found for api key",
				Code:    http_errors.ERROR_CODE_UNAUTHORIZED,
			})
			return
		}
		c.Request = c.Request.WithContext(newCtx)

		c.Next()
	}
}
