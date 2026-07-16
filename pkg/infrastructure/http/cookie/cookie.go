package cookie

import (
	"net/http"
	"time"

	domain_session "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/session"

	"github.com/gin-gonic/gin"
	errorsWrapper "github.com/pkg/errors"
)

const sessionCookieName = "session_id"

type CookieService struct {
	isDev bool
}

func NewCookieService(isDev bool) *CookieService {
	return &CookieService{isDev: isDev}
}

func (s *CookieService) SetCookie(c *gin.Context, session *domain_session.Session) {
	if s.isDev {
		c.SetSameSite(http.SameSiteNoneMode)
	} else {
		// Lax, not Strict: the session is set inside the OAuth callback and
		// followed by a redirect to the dashboard. That redirect is the first
		// hop after a cross-site navigation from the identity provider, and a
		// Strict cookie is withheld on it — the dashboard would see no session
		// and bounce back to /login. Lax is sent on top-level GET navigations
		// while still blocking CSRF on state-changing POSTs.
		c.SetSameSite(http.SameSiteLaxMode)
	}
	c.SetCookie(sessionCookieName, session.ID, int(time.Until(session.ExpiresAt).Seconds()), "/", "", true, true)
}

func (s *CookieService) UnsetCookie(c *gin.Context) {
	c.SetCookie(sessionCookieName, "", -1, "/", "", true, true)
}

func GetCookie(c *gin.Context) (string, error) {
	cookie, err := c.Cookie(sessionCookieName)
	if err != nil {
		return "", errorsWrapper.Wrap(err, "error when trying to read session cookie")
	}
	return cookie, nil
}
