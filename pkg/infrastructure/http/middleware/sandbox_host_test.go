package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestIsSandboxHost(t *testing.T) {
	cases := map[string]bool{
		"sandbox.api.consultasnonstop.com":                   true,
		"develop.sandbox.api.consultasnonstop.kibantest.com": true,
		"develop.sandbox.api.datosnonstop.kibantest.com":     true,
		"sandbox.develop.consulta.kibantest.com":             true,
		"sandbox.localhost:8093":                             true,
		"sandbox.test":                                       true,
		"SANDBOX.api.example.com":                            true,
		"api.consultasnonstop.com":                           false,
		"develop.api.consultasnonstop.kibantest.com":         false,
		"dashboard.consultasnonstop.com":                     false,
		"localhost:8093":                                     false,
		"sandboxes.example.com":                              false,
		"mysandbox.example.com":                              false,
		"api.example.com:443":                                false,
	}
	for host, want := range cases {
		assert.Equal(t, want, IsSandboxHost(host), host)
	}
}

func TestStripSandboxHost(t *testing.T) {
	cases := map[string]string{
		"sandbox.api.consultasnonstop.com":                   "api.consultasnonstop.com",
		"develop.sandbox.api.consultasnonstop.kibantest.com": "develop.api.consultasnonstop.kibantest.com",
		"sandbox.localhost:8093":                             "localhost:8093",
		"api.consultasnonstop.com":                           "api.consultasnonstop.com",
		"localhost:8093":                                     "localhost:8093",
	}
	for host, want := range cases {
		assert.Equal(t, want, StripSandboxHost(host), host)
	}
}

// Regresión: el middleware exigía el prefijo "sandbox.", así que en
// develop.sandbox.api.<producto>.kibantest.com una key sandbox daba 401 y una
// key de producción pasaba como producción.
func TestApiKeyMiddleware_SandboxLabelNotPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const host = "develop.sandbox.api.datosnonstop.kibantest.com"

	run := func(sandboxKey bool) (int, bool) {
		var gotSandbox bool
		m := NewApiKeyMiddleware(
			func(ctx context.Context, key string) (*ApiKeyData, error) {
				return &ApiKeyData{TenantID: "t1", Sandbox: sandboxKey}, nil
			},
			func(ctx context.Context, tenantID string, sandbox bool) (context.Context, error) {
				gotSandbox = sandbox
				return ctx, nil
			},
		)
		r := gin.New()
		r.GET("/v1/x", m.Middleware(), func(c *gin.Context) { c.Status(http.StatusOK) })
		req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
		req.Host = host
		req.Header.Set("x-api-key", "k")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code, gotSandbox
	}

	code, sandbox := run(true)
	assert.Equal(t, http.StatusOK, code, "key sandbox en host sandbox")
	assert.True(t, sandbox)

	code, _ = run(false)
	assert.Equal(t, http.StatusUnauthorized, code, "key de producción en host sandbox")
}
