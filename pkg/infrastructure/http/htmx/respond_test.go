package htmx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	domain_errors "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/errors"
	http_errors "github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/http/errors"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stringRenderable is a minimal Renderable used by tests to assert which
// fragment factory ran. It writes its own label into the response body so
// tests can grep for it.
type stringRenderable string

func (s stringRenderable) Render(_ context.Context, w io.Writer) error {
	_, err := w.Write([]byte(s))
	return err
}

func testConfig(mapper ProjectMapper) Config {
	return Config{
		Fragments: CanonicalFragments{
			Validation:   func(msg string) Renderable { return stringRenderable("validation:" + msg) },
			NotFound:     func(msg string) Renderable { return stringRenderable("notfound:" + msg) },
			Unauthorized: func() Renderable { return stringRenderable("unauthorized") },
			Forbidden:    func() Renderable { return stringRenderable("forbidden") },
			Banner:       func(msg string) Renderable { return stringRenderable("banner:" + msg) },
		},
		ProjectMapper: mapper,
		Render: func(c *gin.Context, status int, comp Renderable) {
			c.Status(status)
			_ = comp.Render(c.Request.Context(), c.Writer)
		},
	}
}

func newCtx(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	return c, rec
}

// TestCanonicalMapping covers every canonical error → status + fragment slot.
func TestCanonicalMapping(t *testing.T) {
	cfg := testConfig(nil)
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"InvalidInput → 422 validation", domain_errors.ErrInvalidInput, 422, "validation:"},
		{"Validation → 422 validation", domain_errors.ErrValidation, 422, "validation:"},
		{"NotFound → 404 notfound", domain_errors.ErrNotFound, 404, "notfound:"},
		{"Unauthorized → 401", domain_errors.ErrUnauthorized, 401, "unauthorized"},
		{"Forbidden → 403", domain_errors.ErrForbidden, 403, "forbidden"},
		{"AlreadyExists → 409 validation", domain_errors.ErrAlreadyExists, 409, "validation:"},
		{"Conflict → 409 validation", domain_errors.ErrConflict, 409, "validation:"},
		{"ExternalSvc → 502 banner", domain_errors.ErrExternalServiceUnavailable, 502, "banner:"},
		{"unknown → 500 banner", errors.New("kaboom"), 500, "banner:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newCtx(t)
			cfg.RespondHTMX(c, tc.err)
			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.wantBody)
		})
	}
}

// TestProjectMapper covers the project-specific extension point — projects
// inject their own errors (rate limit, external dep flavor, payment required,
// etc.) without modifying this shared package.
func TestProjectMapper(t *testing.T) {
	errRateLimited := errors.New("rate limited")
	errCustom := errors.New("custom thing")

	cfg := testConfig(func(err error) (int, Renderable) {
		switch {
		case errors.Is(err, errRateLimited):
			return http.StatusTooManyRequests, stringRenderable("project:rate-limit")
		case errors.Is(err, errCustom):
			return http.StatusPaymentRequired, stringRenderable("project:custom")
		}
		return 0, nil
	})

	t.Run("project sentinel routed via mapper", func(t *testing.T) {
		c, rec := newCtx(t)
		cfg.RespondHTMX(c, errRateLimited)
		assert.Equal(t, 429, rec.Code)
		assert.Contains(t, rec.Body.String(), "project:rate-limit")
	})

	t.Run("canonical takes precedence over project mapper", func(t *testing.T) {
		// Wrap a canonical and the project mapper would never see it.
		wrapped := fmt.Errorf("%w: with detail", domain_errors.ErrNotFound)
		c, rec := newCtx(t)
		cfg.RespondHTMX(c, wrapped)
		assert.Equal(t, 404, rec.Code)
		assert.Contains(t, rec.Body.String(), "notfound:")
	})

	t.Run("project mapper returns 0 → falls through to 500", func(t *testing.T) {
		c, rec := newCtx(t)
		cfg.RespondHTMX(c, errors.New("truly unknown"))
		assert.Equal(t, 500, rec.Code)
		assert.Contains(t, rec.Body.String(), "banner:")
	})
}

// TestFormFallback covers the per-call option: 4xx routes to the form
// component, 5xx ignores the fallback and uses the canonical banner.
func TestFormFallback(t *testing.T) {
	cfg := testConfig(nil)
	fallback := stringRenderable("FORM_RENDERED")

	t.Run("422 uses fallback", func(t *testing.T) {
		c, rec := newCtx(t)
		cfg.RespondHTMX(c, domain_errors.ErrValidation, WithFormFallback(fallback))
		assert.Equal(t, 422, rec.Code)
		assert.Contains(t, rec.Body.String(), "FORM_RENDERED")
	})

	t.Run("401 uses fallback (login-form preservation case)", func(t *testing.T) {
		c, rec := newCtx(t)
		cfg.RespondHTMX(c, domain_errors.ErrUnauthorized, WithFormFallback(fallback))
		assert.Equal(t, 401, rec.Code)
		assert.Contains(t, rec.Body.String(), "FORM_RENDERED")
	})

	t.Run("502 ignores fallback, renders banner", func(t *testing.T) {
		c, rec := newCtx(t)
		cfg.RespondHTMX(c, domain_errors.ErrExternalServiceUnavailable, WithFormFallback(fallback))
		assert.Equal(t, 502, rec.Code)
		assert.NotContains(t, rec.Body.String(), "FORM_RENDERED")
		assert.Contains(t, rec.Body.String(), "banner:")
	})
}

// TestErrorMessageContextKey confirms the helper stashes the error so the
// shared logger middleware records it — without this the migration's whole
// premise (Cloud Logging sees errors even though status was 200 before) breaks.
func TestErrorMessageContextKey(t *testing.T) {
	cfg := testConfig(nil)

	t.Run("RespondHTMX sets the key", func(t *testing.T) {
		c, _ := newCtx(t)
		sentinel := fmt.Errorf("specific: %w", domain_errors.ErrNotFound)
		cfg.RespondHTMX(c, sentinel)
		stashed, ok := c.Get(http_errors.ERROR_MESSAGE_CONTEXT_KEY)
		require.True(t, ok)
		assert.ErrorIs(t, stashed.(error), domain_errors.ErrNotFound)
	})

	t.Run("TagError sets the key without writing", func(t *testing.T) {
		c, rec := newCtx(t)
		cfg.TagError(c, errors.New("background failure"))
		_, ok := c.Get(http_errors.ERROR_MESSAGE_CONTEXT_KEY)
		assert.True(t, ok)
		assert.Equal(t, http.StatusOK, rec.Code, "TagError must NOT write a status")
		assert.Empty(t, rec.Body.String(), "TagError must NOT write a body")
	})
}

// TestStatusForError exposes the status mapping without rendering — useful
// for callers that want to build their own response (e.g. an integration
// test asserting the status only).
func TestStatusForError(t *testing.T) {
	cfg := testConfig(func(err error) (int, Renderable) {
		if err != nil && err.Error() == "x" {
			return 418, nil
		}
		return 0, nil
	})

	assert.Equal(t, 422, cfg.StatusForError(domain_errors.ErrValidation))
	assert.Equal(t, 502, cfg.StatusForError(domain_errors.ErrExternalServiceUnavailable))
	assert.Equal(t, 418, cfg.StatusForError(errors.New("x")), "project mapper status surfaces here")
	assert.Equal(t, 500, cfg.StatusForError(errors.New("unmapped")))
}
