package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders sets the baseline HTTP security response headers on every
// request. It mirrors the header set applied by the go-kiban router so that
// htmx apps built on this shared lib (which do not mount the go-kiban router)
// reach the same posture.
//
// The Content-Security-Policy is intentionally limited to directives that do
// not depend on scripts: frame-ancestors (anti-clickjacking, reinforces
// X-Frame-Options), base-uri and object-src. No default-src/script-src is set
// because the templ views rely on inline <script> blocks and inline event
// handlers; a strict script-src would break the UI and needs a refactor first.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Permitted-Cross-Domain-Policies", "none")
		h.Set("Referrer-Policy", "no-referrer-when-downgrade")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Content-Security-Policy", "frame-ancestors 'self'; base-uri 'self'; object-src 'none'")

		c.Next()
	}
}
