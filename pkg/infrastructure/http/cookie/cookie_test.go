package cookie

import (
	"net/http/httptest"
	"testing"
	"time"

	domain_session "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/session"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setCookieHeader(t *testing.T, isDev bool) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	svc := NewCookieService(isDev)
	svc.SetCookie(c, &domain_session.Session{
		ID:        "session-abc",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	header := w.Header().Get("Set-Cookie")
	require.NotEmpty(t, header, "expected a Set-Cookie header")
	return header
}

// TestSetCookie_ProdUsesLaxSoOAuthRedirectCarriesSession guards the OAuth
// login flow: the session is set inside the identity-provider callback and
// followed by a redirect to the dashboard. With SameSite=Strict the browser
// withholds the cookie on that post-cross-site redirect and the user bounces
// back to /login. Lax is sent on the top-level GET navigation, so the session
// survives the redirect.
func TestSetCookie_ProdUsesLaxSoOAuthRedirectCarriesSession(t *testing.T) {
	header := setCookieHeader(t, false)

	assert.Contains(t, header, "SameSite=Lax")
	assert.NotContains(t, header, "SameSite=Strict")
	assert.Contains(t, header, "session_id=session-abc")
	assert.Contains(t, header, "HttpOnly")
	assert.Contains(t, header, "Secure")
}

// TestSetCookie_DevUsesNone keeps the local/dev cross-origin behaviour (the
// front-end runs on a different origin during development).
func TestSetCookie_DevUsesNone(t *testing.T) {
	header := setCookieHeader(t, true)

	assert.Contains(t, header, "SameSite=None")
}
