package middleware

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/kiban-cloud/go-kiban-fullstack/logger"
	"github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/commons"
	"github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/appContext"
	http_errors "github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/http/errors"

	"github.com/gin-gonic/gin"
)

const (
	TRACE_HEADER      = "X-Cloud-Trace-Context"
	REQUEST_ID_HEADER = "X-Request-ID"

	// HTMX request headers
	HTMX_REQUEST_HEADER      = "HX-Request"
	HTMX_BOOSTED_HEADER      = "HX-Boosted"
	HTMX_TRIGGER_HEADER      = "HX-Trigger"
	HTMX_TRIGGER_NAME_HEADER = "HX-Trigger-Name"
	HTMX_TARGET_HEADER       = "HX-Target"
	HTMX_CURRENT_URL_HEADER  = "HX-Current-URL"

	// Internal context key to avoid double-logging when a panic already logged
	alreadyLoggedKey = "__middleware_already_logged"
)

// Paths fully matched are skipped entirely (no log, no panic recovery instrumentation).
// Health checks and favicons add noise without value.
var skipLoggingPaths = map[string]bool{
	"/health":      true,
	"/healthz":     true,
	"/readyz":      true,
	"/liveness":    true,
	"/_ah/health":  true,
	"/favicon.ico": true,
}

// Prefixes that are skipped — typically static assets.
var skipLoggingPrefixes = []string{
	"/static/",
	"/assets/",
	"/public/",
}

func shouldSkipLogging(path string) bool {
	if skipLoggingPaths[path] {
		return true
	}
	for _, prefix := range skipLoggingPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// wantsHTMLResponse decides what format an error response should be in
// when the middleware itself has to write the body (e.g. on panic).
// HTMX requests, browser navigations, or anything explicitly accepting HTML get HTML.
// API clients accepting JSON get JSON.
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

type LoggerMiddleware struct{}

func NewLoggerMiddleware() *LoggerMiddleware {
	return &LoggerMiddleware{}
}

type responseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *responseWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// extractHTMXContext reads HTMX headers into a logger.HTMXContext.
// Returns nil if the request is not an HTMX request.
func extractHTMXContext(c *gin.Context) *logger.HTMXContext {
	if c.GetHeader(HTMX_REQUEST_HEADER) != "true" {
		return nil
	}
	return &logger.HTMXContext{
		IsHTMX:      true,
		IsBoosted:   c.GetHeader(HTMX_BOOSTED_HEADER) == "true",
		Trigger:     c.GetHeader(HTMX_TRIGGER_HEADER),
		TriggerName: c.GetHeader(HTMX_TRIGGER_NAME_HEADER),
		Target:      c.GetHeader(HTMX_TARGET_HEADER),
		CurrentURL:  c.GetHeader(HTMX_CURRENT_URL_HEADER),
	}
}

func (m *LoggerMiddleware) Middleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// Skip static assets and health checks entirely
		if shouldSkipLogging(ctx.Request.URL.Path) {
			ctx.Next()
			return
		}

		start := time.Now()
		requestID := commons.NewUUID()
		ctx.Header(REQUEST_ID_HEADER, requestID)

		traceHeader := ctx.GetHeader(TRACE_HEADER)
		traceID := logger.ParseTraceContext(traceHeader)

		newCtx := appContext.WithRequestAndTraceID(ctx.Request.Context(), requestID, traceID)
		ctx.Request = ctx.Request.WithContext(newCtx)

		htmxCtx := extractHTMXContext(ctx)

		var requestBody []byte
		if ctx.Request.Body != nil {
			var err error
			requestBody, err = io.ReadAll(ctx.Request.Body)
			if err != nil {
				log.Printf("[ERROR] Failed to read request body: %v", err)
			} else {
				ctx.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
			}
		}

		defer func() {
			if err := recover(); err != nil {
				responseContentType := ctx.Writer.Header().Get("Content-Type")

				errCtx := logger.RequestInfo{
					Method:      ctx.Request.Method,
					Path:        ctx.Request.URL.Path,
					Query:       ctx.Request.URL.RawQuery,
					IP:          ctx.ClientIP(),
					UserAgent:   ctx.Request.UserAgent(),
					StatusCode:  http.StatusInternalServerError,
					Duration:    time.Since(start),
					Headers:     ctx.Request.Header,
					RequestBody: string(requestBody),
					ContentType: responseContentType,
					HTMXContext: htmxCtx,
				}

				if errError, ok := err.(error); ok {
					errCtx.Error = errError
				} else {
					errCtx.Error = fmt.Errorf("panic: %v", err)
				}

				logger.LogHttpInfo(ctx.Request.Context(), errCtx, true)
				ctx.Set(alreadyLoggedKey, true)

				if !ctx.Writer.Written() {
					if wantsHTMLResponse(ctx) {
						// HTMX or browser navigation: return an HTML fragment.
						// The client can render this in an error banner via hx-target-5xx.
						ctx.Header("Content-Type", "text/html; charset=utf-8")
						ctx.String(http.StatusInternalServerError, renderHTMLErrorFragment(requestID))
					} else {
						// JSON API client
						ctx.JSON(http.StatusInternalServerError,
							http_errors.NewInternalServerErrorResponse(ctx, errCtx.Error, "panic"))
					}
				}

				ctx.Abort()
			}
		}()

		responseBody := &bytes.Buffer{}
		writer := &responseWriter{
			ResponseWriter: ctx.Writer,
			body:           responseBody,
		}
		ctx.Writer = writer

		ctx.Next()

		// If the panic recovery already logged, skip the post-handler logging.
		if alreadyLogged, _ := ctx.Get(alreadyLoggedKey); alreadyLogged == true {
			return
		}

		statusCode := ctx.Writer.Status()

		// Safe type assertion: callers (htmx.TagError, RespondHTMX) set the
		// key with their `err` parameter, which could be a nil interface or
		// a non-error value if a caller misuses the API. A naked `.(error)`
		// would panic, and that panic would be caught by the recover above
		// — which logs it as a 500 even though the handler already wrote a
		// 2xx. Use comma-ok so misuse becomes a silent no-op instead of a
		// confusing fake 500 in the logs.
		var errorInContextError error
		if v, ok := ctx.Get(http_errors.ERROR_MESSAGE_CONTEXT_KEY); ok {
			if e, isErr := v.(error); isErr {
				errorInContextError = e
			}
		}

		requestInfo := logger.RequestInfo{
			Method:       ctx.Request.Method,
			Path:         ctx.Request.URL.Path,
			Query:        ctx.Request.URL.RawQuery,
			IP:           ctx.ClientIP(),
			UserAgent:    ctx.Request.UserAgent(),
			StatusCode:   statusCode,
			Duration:     time.Since(start),
			Headers:      ctx.Request.Header,
			RequestBody:  string(requestBody),
			ResponseBody: responseBody.String(),
			ContentType:  ctx.Writer.Header().Get("Content-Type"),
			HTMXContext:  htmxCtx,
			Error:        errorInContextError,
		}

		logger.LogHttpInfo(ctx.Request.Context(), requestInfo, false)
	}
}

// renderHTMLErrorFragment returns a minimal HTML fragment for unrecoverable errors.
// Replace with a Templ component in your project if you want consistent styling.
func renderHTMLErrorFragment(requestID string) string {
	return fmt.Sprintf(`<div class="error-banner" role="alert">
  <p>Ocurrió un error inesperado. El equipo ya fue notificado.</p>
  <p class="error-request-id">Referencia: %s</p>
</div>`, requestID)
}
