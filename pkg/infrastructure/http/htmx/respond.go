// Package htmx maps domain errors to HTMX-friendly HTTP responses.
//
// The pattern (see HTMX_ERROR_HANDLING.md at the repo root): handlers that
// fail call RespondHTMX, which writes the correct HTTP status (4xx / 5xx)
// AND an HTML fragment that HTMX swaps into the page via the
// htmx-ext-response-targets extension. The original error is stashed in the
// gin context under ERROR_MESSAGE_CONTEXT_KEY so the logger middleware
// records it — Cloud Logging / Monitoring sees both the real status and the
// real error, no more 200-with-error-fragment blind spots.
//
// This package is template-engine agnostic: it accepts any value that
// implements the same Render(ctx, io.Writer) error shape as a-h/templ. Each
// project supplies its own fragment factories (with its own design system)
// via the Config.Fragments hook.
package htmx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	domain_errors "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/errors"
	http_errors "github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/http/errors"

	"github.com/gin-gonic/gin"
)

// Renderable is satisfied by any value with the templ.Component shape. Keeps
// this package decoupled from a-h/templ — projects pass templ components
// directly and the structural match handles assignment.
type Renderable interface {
	Render(ctx context.Context, w io.Writer) error
}

// CanonicalFragments provides the project's fragment factories for each
// canonical error category. Mandatory — RespondHTMX will panic on a nil
// factory if the matching error category fires.
type CanonicalFragments struct {
	// Validation is rendered for ErrInvalidInput / ErrValidation (422) AND
	// for ErrAlreadyExists / ErrConflict (409) — same UX (inline alert by
	// the form), different HTTP status.
	Validation func(message string) Renderable

	// NotFound is rendered for ErrNotFound (404).
	NotFound func(message string) Renderable

	// Unauthorized is rendered for ErrUnauthorized (401). Typical
	// implementation triggers a redirect to /login on the client.
	Unauthorized func() Renderable

	// Forbidden is rendered for ErrForbidden (403).
	Forbidden func() Renderable

	// Banner is rendered for ErrExternalServiceUnavailable (502) and the
	// default 500 fallback. Projects also use this for their own 5xx /
	// 429 cases via ProjectMapper.
	Banner func(message string) Renderable
}

// ProjectMapper handles errors specific to the project (e.g. ErrRateLimited,
// ErrRenapoUnavailable, ErrInsufficientTokens). It runs AFTER the canonical
// mapping fails. Return (0, nil) to fall through to the default 500 banner.
type ProjectMapper func(err error) (status int, comp Renderable)

// Renderer writes the response. Most projects can use DefaultRender; only
// override if the project's view layer has its own write conventions (custom
// content-type, response-recorder hooks, etc.).
type Renderer func(c *gin.Context, status int, comp Renderable)

// DefaultRender writes the component as text/html with the supplied status.
// On render failure, logs and overrides the status to 500. This is the
// out-of-the-box implementation that most projects will plug into
// Config.Render — there is no project-specific concern in this path.
func DefaultRender(c *gin.Context, status int, comp Renderable) {
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := comp.Render(c.Request.Context(), c.Writer); err != nil {
		slog.Error("htmx render error", "error", err)
		c.Status(http.StatusInternalServerError)
	}
}

// Config bundles the project-specific wiring. Build one at boot and keep it
// for the lifetime of the process; the methods are safe to call concurrently.
type Config struct {
	Fragments     CanonicalFragments
	ProjectMapper ProjectMapper // optional
	Render        Renderer      // required
}

// Default is the package-level Config used by the top-level RespondHTMX /
// TagError / StatusForError functions. Projects set this once at boot
// (typically in an init() of their httperrors package) and then callsites
// reach for `htmx.RespondHTMX(c, err)` directly — no project-level wrapper
// needed.
//
// If you need multiple Configs in the same process (rare), use the Config
// methods directly instead of the package-level functions.
var Default Config

// RespondHTMX writes an HTMX error response using the package-level Default
// Config. Equivalent to `Default.RespondHTMX(c, err, opts...)`.
func RespondHTMX(c *gin.Context, err error, opts ...Option) {
	Default.RespondHTMX(c, err, opts...)
}

// TagError stashes err in the gin context using the package-level Default
// Config. Equivalent to `Default.TagError(c, err)`.
func TagError(c *gin.Context, err error) {
	Default.TagError(c, err)
}

// StatusForError returns the HTTP status for err using the package-level
// Default Config. Equivalent to `Default.StatusForError(err)`.
func StatusForError(err error) int {
	return Default.StatusForError(err)
}

// Option mutates per-call behavior of RespondHTMX.
type Option func(*opts)

type opts struct {
	formFallback Renderable
}

// WithFormFallback re-renders the supplied form component with the mapped
// status code, instead of the canonical fragment. Used by handlers that want
// to preserve form state (Values, Fields, Error) on a 4xx.
//
// The fallback is only honored for 4xx statuses, since the client routes 4xx
// to the form via `hx-target-4xx` and 5xx to the global banner via
// `hx-target-5xx`. Rendering a form into the global banner would look broken.
func WithFormFallback(form Renderable) Option {
	return func(o *opts) { o.formFallback = form }
}

// RespondHTMX writes an HTMX error response: status code derived from err
// plus an HTML fragment. Use in error paths only.
func (cfg Config) RespondHTMX(c *gin.Context, err error, options ...Option) {
	o := &opts{}
	for _, opt := range options {
		opt(o)
	}

	status, comp := cfg.mapError(err)

	if o.formFallback != nil && isFormPreservable(status) {
		comp = o.formFallback
	}

	c.Set(http_errors.ERROR_MESSAGE_CONTEXT_KEY, err)
	cfg.Render(c, status, comp)
}

// TagError stashes the error in the gin context so the logger middleware
// records it, without writing any response. Use in partial-degradation paths
// that still return 200 (e.g. a dashboard section that renders a degraded
// state instead of failing the whole page).
func (cfg Config) TagError(c *gin.Context, err error) {
	c.Set(http_errors.ERROR_MESSAGE_CONTEXT_KEY, err)
}

// StatusForError returns the HTTP status that would be used for err. Useful
// for callers that build their own response but want a consistent mapping.
func (cfg Config) StatusForError(err error) int {
	s, _ := cfg.mapError(err)
	return s
}

// isFormPreservable reports whether a status code should re-render the form
// (when a fallback is provided) instead of the canonical fragment. Any 4xx
// is preservable — the client routes 4xx to the form via hx-target-4xx.
func isFormPreservable(status int) bool {
	return status >= 400 && status < 500
}

func (cfg Config) mapError(err error) (int, Renderable) {
	switch {
	case errors.Is(err, domain_errors.ErrInvalidInput),
		errors.Is(err, domain_errors.ErrValidation):
		return http.StatusUnprocessableEntity, cfg.Fragments.Validation("Revisa los campos del formulario.")

	case errors.Is(err, domain_errors.ErrNotFound):
		return http.StatusNotFound, cfg.Fragments.NotFound("No encontrado.")

	case errors.Is(err, domain_errors.ErrUnauthorized):
		return http.StatusUnauthorized, cfg.Fragments.Unauthorized()

	case errors.Is(err, domain_errors.ErrForbidden):
		return http.StatusForbidden, cfg.Fragments.Forbidden()

	case errors.Is(err, domain_errors.ErrAlreadyExists),
		errors.Is(err, domain_errors.ErrConflict):
		return http.StatusConflict, cfg.Fragments.Validation("El recurso ya existe o está en conflicto con el estado actual.")

	case errors.Is(err, domain_errors.ErrExternalServiceUnavailable):
		return http.StatusBadGateway, cfg.Fragments.Banner("Servicio externo no disponible. Intenta de nuevo en unos momentos.")
	}

	if cfg.ProjectMapper != nil {
		if status, comp := cfg.ProjectMapper(err); status != 0 {
			return status, comp
		}
	}

	slog.Error("unmapped error in RespondHTMX", "error", err)
	return http.StatusInternalServerError, cfg.Fragments.Banner("Ocurrió un error inesperado. El equipo ya fue notificado.")
}
