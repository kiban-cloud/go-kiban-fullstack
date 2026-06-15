package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/kiban-cloud/go-kiban-fullstack/logger"
	http_errors "github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/http/errors"

	"github.com/gin-gonic/gin"
)

const HTMX_REQUEST_HEADER = "HX-Request"

// Rutas/prefijos excluidos del log de acceso (health checks, assets).
var skipLoggingPaths = map[string]bool{
	"/health":      true,
	"/healthz":     true,
	"/readyz":      true,
	"/liveness":    true,
	"/_ah/health":  true,
	"/favicon.ico": true,
}

var skipLoggingPrefixes = []string{"/static/", "/assets/", "/public/"}

type LoggerMiddleware struct{}

func NewLoggerMiddleware() *LoggerMiddleware {
	return &LoggerMiddleware{}
}

// Middleware delega en el middleware compartido del módulo logger; sólo aporta
// lo específico de fullstack: rutas a saltar y cómo responder un panic (HTML
// para HTMX/navegación, JSON para clientes API).
func (m *LoggerMiddleware) Middleware() gin.HandlerFunc {
	return logger.Middleware(logger.MiddlewareConfig{
		SkipPaths:    skipLoggingPaths,
		SkipPrefixes: skipLoggingPrefixes,
		OnPanic:      onPanic,
	})
}

func onPanic(c *gin.Context, requestID string, err error) {
	if wantsHTMLResponse(c) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusInternalServerError, renderHTMLErrorFragment(requestID))
		return
	}
	c.JSON(http.StatusInternalServerError, http_errors.NewInternalServerErrorResponse(c, err, "panic"))
}

// wantsHTMLResponse decide el formato de la respuesta de error cuando el
// middleware tiene que escribir el body (panic). HTMX o navegación → HTML.
func wantsHTMLResponse(c *gin.Context) bool {
	if c.GetHeader(HTMX_REQUEST_HEADER) == "true" {
		return true
	}
	accept := c.GetHeader("Accept")
	if strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json") {
		return true
	}
	return false
}

// renderHTMLErrorFragment es un fragmento mínimo para errores irrecuperables.
func renderHTMLErrorFragment(requestID string) string {
	return fmt.Sprintf(`<div class="error-banner" role="alert">
  <p>Ocurrió un error inesperado. El equipo ya fue notificado.</p>
  <p class="error-request-id">Referencia: %s</p>
</div>`, requestID)
}
