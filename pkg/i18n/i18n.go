// Package i18n is the shared UI-string localization machinery for every Kiban
// htmx app (kiban-cloud, workfloo, rekon, crm, klin, link). It lives in
// go-kiban-fullstack — the common base every service already imports — so the
// plumbing is written once and each app supplies only its own message bundles.
//
// # Split of responsibilities
//
//   - This package owns the MACHINERY: bundle loading, message lookup by the
//     request's language, the language↔context plumbing, and a gin middleware
//     that resolves the logged-out language from the shared kiban_lang cookie.
//   - Each app owns its CONTENT: it embeds its own locales/*.toml and builds a
//     *Catalog once via NewCatalog.
//
// # How a request gets its language
//
// The language for a request is carried in its context.Context as an ordered
// list of preference strings (e.g. ["en","es"]). Two injectors set it:
//
//   - Authenticated pages: each app's auth middleware calls WithLanguage with
//     the signed-in user's preference (the app knows its own user model).
//   - Logged-out pages: Catalog.Middleware reads the kiban_lang cookie /
//     Accept-Language and calls WithLanguage.
//
// Because the context carries plain language strings (not a bundle-bound
// localizer), any Catalog can localize against the same context — so an app
// with several catalogs, or a shared component rendered inside another app,
// all resolve to the one language the request settled on.
//
// # Cross-app propagation
//
// kiban_lang is a non-HttpOnly, same-origin cookie set by kiban-cloud from the
// user's saved language (at login and whenever they change it in their
// profile). Every sibling app is served under the same host, so the cookie
// reaches all of them and is readable by React micro-frontends' i18next too —
// making the kiban-cloud profile selection the single control point.
package i18n

import (
	"context"
	"io/fs"

	"github.com/BurntSushi/toml"
	"github.com/gin-gonic/gin"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// CookieLang is the cross-app UI-language cookie name. Defined here so every
// service references one source of truth rather than re-declaring the string.
const CookieLang = "kiban_lang"

// Catalog is one app's loaded message store plus the languages it supports.
// Safe for concurrent use: go-i18n's Bundle is read-only after loading and a
// fresh Localizer is created per lookup.
type Catalog struct {
	bundle    *goi18n.Bundle
	supported []language.Tag
}

// NewCatalog loads <dir>/<tag>.toml from fsys for each supported tag (highest
// priority / default first) and returns a ready Catalog. Intended to be called
// once at package init with an embed.FS:
//
//	//go:embed locales/*.toml
//	var localesFS embed.FS
//	var Catalog = i18n.NewCatalog(localesFS, "locales", language.Spanish, language.English)
//
// Panics on a missing/malformed bundle or an empty supported list — both are
// build-time mistakes that should fail loudly at startup, not degrade at
// runtime.
func NewCatalog(fsys fs.FS, dir string, supported ...language.Tag) *Catalog {
	if len(supported) == 0 {
		panic("i18n: NewCatalog requires at least one supported language")
	}
	b := goi18n.NewBundle(supported[0])
	b.RegisterUnmarshalFunc("toml", toml.Unmarshal)
	for _, tag := range supported {
		name := dir + "/" + tag.String() + ".toml"
		if _, err := b.LoadMessageFileFS(fsys, name); err != nil {
			panic("i18n: loading bundle " + name + ": " + err.Error())
		}
	}
	return &Catalog{bundle: b, supported: supported}
}

type ctxKey struct{}

// WithLanguage returns a copy of ctx carrying the given language preferences,
// highest priority first (e.g. "en", "es"). Empty entries are dropped. This is
// the single injection point both the app auth middleware (authenticated) and
// Catalog.Middleware (logged-out) call.
func WithLanguage(ctx context.Context, prefs ...string) context.Context {
	clean := make([]string, 0, len(prefs))
	for _, p := range prefs {
		if p != "" {
			clean = append(clean, p)
		}
	}
	return context.WithValue(ctx, ctxKey{}, clean)
}

func languagesFrom(ctx context.Context) []string {
	if v, ok := ctx.Value(ctxKey{}).([]string); ok && len(v) > 0 {
		return v
	}
	return nil
}

// T resolves message id to the request's language. A missing id returns the id
// itself (not "") so an untranslated key is visible in the UI and grep-able,
// and a context that never ran an injector falls back to the default language
// — T is total and never panics.
func (c *Catalog) T(ctx context.Context, id string) string {
	return c.localize(ctx, id, nil)
}

// Tf is T with template data for interpolation ("Hola {{.Name}}").
func (c *Catalog) Tf(ctx context.Context, id string, data map[string]any) string {
	return c.localize(ctx, id, data)
}

func (c *Catalog) localize(ctx context.Context, id string, data map[string]any) string {
	langs := languagesFrom(ctx)
	if langs == nil {
		langs = []string{c.supported[0].String()}
	}
	msg, err := goi18n.NewLocalizer(c.bundle, langs...).Localize(&goi18n.LocalizeConfig{
		MessageID:    id,
		TemplateData: data,
	})
	if err != nil {
		return id
	}
	return msg
}

// DefaultLanguage is the catalog's fallback language base subtag (e.g. "es").
func (c *Catalog) DefaultLanguage() string {
	base, _ := c.supported[0].Base()
	return base.String()
}

// IsSupported reports whether lang matches one of the catalog's languages by
// base subtag ("es", "en"). Use it to validate an explicit preference (a saved
// user language, a cookie value) before trusting it.
func (c *Catalog) IsSupported(lang string) bool {
	t, err := language.Parse(lang)
	if err != nil {
		return false
	}
	base, _ := t.Base()
	for _, s := range c.supported {
		sb, _ := s.Base()
		if base == sb {
			return true
		}
	}
	return false
}

// Match picks the best supported language for ordered preferences (an
// Accept-Language header, say). Unlike IsSupported it always returns a
// supported language — the default when nothing matches — so it's the right
// call for the "browser preference" resolution step.
func (c *Catalog) Match(prefs ...string) string {
	tags := make([]language.Tag, 0, len(prefs))
	for _, p := range prefs {
		if p == "" {
			continue
		}
		parsed, _, err := language.ParseAcceptLanguage(p)
		if err != nil {
			continue
		}
		tags = append(tags, parsed...)
	}
	_, idx, _ := language.NewMatcher(c.supported).Match(tags...)
	base, _ := c.supported[idx].Base()
	return base.String()
}

// Middleware resolves the language for LOGGED-OUT requests (cookie → Accept-
// Language → default) and injects it into the request context so templ views
// render translated copy. Authenticated routes don't use this — their auth
// middleware injects the signed-in user's language via WithLanguage instead.
//
// cookieName is almost always CookieLang; it's a parameter so a service can
// point at a different cookie in a bespoke setup.
func (c *Catalog) Middleware(cookieName string) gin.HandlerFunc {
	return func(gc *gin.Context) {
		lang := c.resolveFromRequest(gc, cookieName)
		gc.Request = gc.Request.WithContext(WithLanguage(gc.Request.Context(), lang, c.DefaultLanguage()))
		gc.Next()
	}
}

func (c *Catalog) resolveFromRequest(gc *gin.Context, cookieName string) string {
	if cookie, err := gc.Cookie(cookieName); err == nil && c.IsSupported(cookie) {
		return cookie
	}
	if header := gc.GetHeader("Accept-Language"); header != "" {
		return c.Match(header)
	}
	return c.DefaultLanguage()
}
