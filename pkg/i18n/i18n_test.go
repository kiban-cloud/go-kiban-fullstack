package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/language"
)

func init() { gin.SetMode(gin.TestMode) }

// testCatalog builds a two-language catalog from an in-memory FS so the
// machinery is exercised without real embed files.
func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	fsys := fstest.MapFS{
		"locales/es.toml": {Data: []byte(`"greet" = "Hola"` + "\n" + `"welcome" = "Hola {{.Name}}"`)},
		"locales/en.toml": {Data: []byte(`"greet" = "Hello"` + "\n" + `"welcome" = "Hello {{.Name}}"`)},
	}
	return NewCatalog(fsys, "locales", language.Spanish, language.English)
}

func TestT_PerLanguageAndFallback(t *testing.T) {
	c := testCatalog(t)

	if got := c.T(WithLanguage(context.Background(), "en"), "greet"); got != "Hello" {
		t.Errorf("en greet = %q, want Hello", got)
	}
	if got := c.T(WithLanguage(context.Background(), "es"), "greet"); got != "Hola" {
		t.Errorf("es greet = %q, want Hola", got)
	}
	// Bare context (no injector ran) → default language, never panic/empty.
	if got := c.T(context.Background(), "greet"); got != "Hola" {
		t.Errorf("bare-context greet = %q, want default Hola", got)
	}
	// Missing id echoes back.
	if got := c.T(context.Background(), "nope"); got != "nope" {
		t.Errorf("missing id = %q, want echoed id", got)
	}
}

func TestTf_Interpolates(t *testing.T) {
	c := testCatalog(t)
	got := c.Tf(WithLanguage(context.Background(), "en"), "welcome", map[string]any{"Name": "Ana"})
	if got != "Hello Ana" {
		t.Errorf("Tf = %q, want Hello Ana", got)
	}
}

func TestIsSupportedAndDefault(t *testing.T) {
	c := testCatalog(t)
	if c.DefaultLanguage() != "es" {
		t.Errorf("DefaultLanguage = %q, want es", c.DefaultLanguage())
	}
	for _, ok := range []string{"es", "en", "en-US", "es-MX"} {
		if !c.IsSupported(ok) {
			t.Errorf("IsSupported(%q) = false, want true", ok)
		}
	}
	for _, no := range []string{"fr", "", "zzz"} {
		if c.IsSupported(no) {
			t.Errorf("IsSupported(%q) = true, want false", no)
		}
	}
}

func TestMatch(t *testing.T) {
	c := testCatalog(t)
	cases := []struct{ in, want string }{
		{"en-US,en;q=0.9,es;q=0.8", "en"},
		{"es-MX,es;q=0.9", "es"},
		{"fr-FR", "es"}, // unsupported → default
		{"", "es"},
	}
	for _, tc := range cases {
		if got := c.Match(tc.in); got != tc.want {
			t.Errorf("Match(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMiddleware_ResolvesAndInjects(t *testing.T) {
	c := testCatalog(t)
	cases := []struct {
		name       string
		cookie     string
		acceptLang string
		wantGreet  string
	}{
		{"cookie en", "en", "es-MX", "Hello"},
		{"unsupported cookie → header", "fr", "en-US,en;q=0.9", "Hello"},
		{"no cookie → header es", "", "es-MX,es;q=0.9", "Hola"},
		{"nothing → default", "", "", "Hola"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: CookieLang, Value: tc.cookie})
			}
			if tc.acceptLang != "" {
				req.Header.Set("Accept-Language", tc.acceptLang)
			}
			gc, _ := gin.CreateTestContext(httptest.NewRecorder())
			gc.Request = req

			var got string
			c.Middleware(CookieLang)(gc)
			got = c.T(gc.Request.Context(), "greet")
			if got != tc.wantGreet {
				t.Fatalf("after middleware, greet = %q, want %q", got, tc.wantGreet)
			}
		})
	}
}
