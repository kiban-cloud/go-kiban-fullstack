package logger

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// MiddlewareConfig configura el middleware HTTP compartido. La plomería
// (request_id, trace, captura de body, recover, emisión del log por request) es
// común; los hooks cubren lo que difiere por proyecto.
type MiddlewareConfig struct {
	// SkipPaths / SkipPrefixes: rutas excluidas del log (health, assets, …).
	SkipPaths    map[string]bool
	SkipPrefixes []string
	// Labels devuelve las labels del request (tenant/auth tags) desde el gin
	// context. Opcional; se stashean para que FromContext las adjunte.
	Labels func(c *gin.Context) map[string]string
	// OnPanic escribe la respuesta cuando se recupera un panic. Opcional; por
	// defecto responde 500 sin body. El log del panic lo emite el middleware.
	OnPanic func(c *gin.Context, requestID string, err error)
}

func (cfg MiddlewareConfig) shouldSkip(path string) bool {
	if cfg.SkipPaths[path] {
		return true
	}
	for _, prefix := range cfg.SkipPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *responseBodyWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// Middleware emite UN log estructurado por request (vía LogHttpInfo), genera
// request_id, parsea trace, captura el body y recupera panics. Es el único
// middleware de logging; cada repo lo monta con su propia config.
func Middleware(cfg MiddlewareConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.shouldSkip(c.Request.URL.Path) {
			c.Next()
			return
		}

		start := time.Now()

		requestID := uuid.NewString()
		c.Header(REQUEST_ID_HEADER, requestID)
		trace := ParseTraceContext(c.GetHeader(TRACE_HEADER))
		c.Request = c.Request.WithContext(WithRequestAndTrace(c.Request.Context(), requestID, trace))

		var requestBody []byte
		if c.Request.Body != nil {
			var buf bytes.Buffer
			tee := io.TeeReader(c.Request.Body, &buf)
			requestBody, _ = io.ReadAll(tee)
			c.Request.Body = io.NopCloser(&buf)
		}

		w := &responseBodyWriter{body: &bytes.Buffer{}, ResponseWriter: c.Writer}
		c.Writer = w

		// El logueo va en defer para correr haya o no panic.
		defer func() {
			ctx := c.Request.Context()
			if cfg.Labels != nil {
				ctx = WithLabels(ctx, cfg.Labels(c))
			}

			info := RequestInfo{
				Method:       c.Request.Method,
				Path:         c.Request.URL.Path,
				Query:        c.Request.URL.RawQuery,
				IP:           c.ClientIP(),
				UserAgent:    c.Request.UserAgent(),
				StatusCode:   w.Status(),
				Duration:     time.Since(start),
				Headers:      c.Request.Header,
				RequestBody:  string(requestBody),
				ResponseBody: w.body.String(),
				ContentType:  c.Writer.Header().Get("Content-Type"),
				HTMXContext:  HTMXFromHeader(c.Request.Header),
			}

			if r := recover(); r != nil {
				err, ok := r.(error)
				if !ok {
					err = fmt.Errorf("panic: %v", r)
				}
				if !c.Writer.Written() {
					if cfg.OnPanic != nil {
						cfg.OnPanic(c, requestID, err)
					} else {
						c.Status(http.StatusInternalServerError)
					}
				}
				c.Abort()
				info.StatusCode = http.StatusInternalServerError
				info.Error = err
				LogHttpInfo(ctx, info, true)
				return
			}

			// Sin panic: el handler pudo dejar el error real en el context
			// (patrón retorna-error → middleware loguea una vez).
			if v, ok := c.Get(ERROR_MESSAGE_CONTEXT_KEY); ok {
				if e, isErr := v.(error); isErr {
					info.Error = e
				}
			}
			LogHttpInfo(ctx, info, false)
		}()

		c.Next()
	}
}
