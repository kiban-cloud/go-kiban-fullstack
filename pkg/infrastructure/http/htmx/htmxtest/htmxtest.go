// Package htmxtest provides shared test primitives for projects that wire
// the htmx error helper. None of these depend on a-h/templ at the type level
// (we use the htmx.Renderable interface), but they're convenient to call
// from tests that DO render templ components.
package htmxtest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/http/htmx"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// NewCtx returns a gin context backed by a httptest.ResponseRecorder, with a
// minimal POST request attached. Use to drive RespondHTMX / TagError in tests
// without spinning up a router.
func NewCtx(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	return c, rec
}

// Render renders comp to a string. Fails the test on render error.
func Render(t *testing.T, comp htmx.Renderable) string {
	t.Helper()
	var buf strings.Builder
	require.NoError(t, comp.Render(context.Background(), &buf))
	return buf.String()
}

// Marker returns a Renderable that writes the supplied string verbatim.
// Useful as a form-fallback substitute in tests where rendering a real templ
// component would be overkill.
func Marker(s string) htmx.Renderable {
	return markerRenderable(s)
}

type markerRenderable string

func (m markerRenderable) Render(_ context.Context, w io.Writer) error {
	_, err := w.Write([]byte(m))
	return err
}
