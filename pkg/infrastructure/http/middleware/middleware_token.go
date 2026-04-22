package middleware

import (
	"context"
	"net/http"

	http_errors "github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/http/errors"

	"github.com/gin-gonic/gin"
)

// TokenAuthenticator validates a token and returns a context enriched with
// whatever the project wants (tenant, user, etc.). Returning an error causes
// the middleware to respond 401.
type TokenAuthenticator func(ctx context.Context, token string) (context.Context, error)

type AuthTokenMiddleware struct {
	authenticate TokenAuthenticator
}

func NewAuthTokenMiddleware(authenticate TokenAuthenticator) *AuthTokenMiddleware {
	return &AuthTokenMiddleware{authenticate: authenticate}
}

func (m *AuthTokenMiddleware) Middleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token := ctx.GetHeader("token")

		if token == "" {
			ctx.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		newCtx, err := m.authenticate(ctx.Request.Context(), token)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, http_errors.NewUnauthorizedErrorResponse(ctx, err, "error validating token"))
			return
		}

		ctx.Request = ctx.Request.WithContext(newCtx)
		ctx.Next()
	}
}
