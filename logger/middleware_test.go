package logger

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// tenantRouter monta el middleware de logging y, detrás, un "auth" que deja el
// tenant en el gin context, igual que en los repos. Labels lo lee de ahí.
func tenantRouter(labelsCalls *int, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Middleware(MiddlewareConfig{
		Labels: func(c *gin.Context) map[string]string {
			*labelsCalls++
			companyId, _ := c.Get("companyId")
			s, _ := companyId.(string)
			return map[string]string{"companyId": s}
		},
	}))
	router.Use(func(c *gin.Context) {
		c.Set("companyId", "2068")
		c.Next()
	})
	router.GET("/", handler)
	return router
}

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var sink bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&sink, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &sink
}

func TestMiddleware_LosLogsDelHandlerLlevanLasLabelsDelAuth(t *testing.T) {
	sink := captureSlog(t)
	calls := 0

	router := tenantRouter(&calls, func(c *gin.Context) {
		FromContext(c.Request.Context()).Info("dentro del handler")
		c.Status(http.StatusOK)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	linea := strings.SplitN(sink.String(), "\n", 2)[0]
	if !strings.Contains(linea, `"msg":"dentro del handler"`) {
		t.Fatalf("la primera línea no es la del handler:\n%s", sink.String())
	}
	if !strings.Contains(linea, `"companyId":"2068"`) {
		t.Errorf("la línea del handler no trae companyId:\n%s", linea)
	}
}

func TestMiddleware_LaLineaDeAccesoNoRepiteLasLabels(t *testing.T) {
	sink := captureSlog(t)
	calls := 0

	router := tenantRouter(&calls, func(c *gin.Context) { c.Status(http.StatusOK) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if n := strings.Count(sink.String(), `"companyId":`); n != 1 {
		t.Errorf("companyId debe salir una vez en la línea de acceso, salió %d:\n%s", n, sink.String())
	}
}

func TestMiddleware_DespuesDelRequestUsaLaCopiaCongelada(t *testing.T) {
	sink := captureSlog(t)
	calls := 0

	var ctxDelHandler context.Context
	router := tenantRouter(&calls, func(c *gin.Context) {
		ctxDelHandler = c.Request.Context()
		c.Status(http.StatusOK)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	// Una goroutine que loguea cuando el request ya terminó: gin ya puede haber
	// reciclado el Context, así que no se debe volver a leer de él.
	callsAlTerminar := calls
	sink.Reset()
	FromContext(ctxDelHandler).Info("despues del request")

	if calls != callsAlTerminar {
		t.Errorf("después del request se volvió a leer el gin context (%d lecturas más)", calls-callsAlTerminar)
	}
	if !strings.Contains(sink.String(), `"companyId":"2068"`) {
		t.Errorf("la línea de después del request perdió companyId:\n%s", sink.String())
	}
}
