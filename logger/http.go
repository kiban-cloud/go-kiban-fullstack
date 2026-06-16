package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// sensitiveFormFields se redactan de los bodies aún en non-prod.
var sensitiveFormFields = map[string]bool{
	"password":         true,
	"password_confirm": true,
	"current_password": true,
	"new_password":     true,
	"card_number":      true,
	"card":             true,
	"cvv":              true,
	"cvc":              true,
	"ssn":              true,
	"token":            true,
	"api_key":          true,
	"apikey":           true,
	"secret":           true,
	"authorization":    true,
}

// sensitiveHeaders se redactan al loguear los headers del request (el valor se
// oculta, la key se conserva). Match case-insensitive.
var sensitiveHeaders = map[string]bool{
	"authorization":      true,
	"x-api-key":          true,
	"x-internal-api-key": true,
	"x-auth-token":       true,
	"cookie":             true,
	"set-cookie":         true,
}

// redactHeaders devuelve una COPIA de los headers con los valores sensibles
// ocultos. No muta el mapa original (es el del request).
func redactHeaders(headers map[string][]string) map[string][]string {
	if headers == nil {
		return nil
	}
	redacted := make(map[string][]string, len(headers))
	for k, v := range headers {
		if sensitiveHeaders[strings.ToLower(k)] {
			redacted[k] = []string{"[REDACTED]"}
		} else {
			redacted[k] = v
		}
	}
	return redacted
}

// htmlResponseBodyMaxPreview limita el preview HTML logueado en errores.
const htmlResponseBodyMaxPreview = 500

// HTMXContext captura los headers HTMX del request para logging estructurado.
type HTMXContext struct {
	IsHTMX      bool
	IsBoosted   bool
	Trigger     string
	TriggerName string
	Target      string
	CurrentURL  string
}

type RequestInfo struct {
	Method       string
	Path         string
	Query        string
	IP           string
	UserAgent    string
	StatusCode   int
	Duration     time.Duration
	RequestBody  string
	ResponseBody string
	Headers      map[string][]string
	ContentType  string
	HTMXContext  *HTMXContext
	Error        error
}

// HTMXFromHeader arma el HTMXContext a partir de los headers del request.
// Devuelve nil si no es un request HTMX.
func HTMXFromHeader(h http.Header) *HTMXContext {
	if h.Get("HX-Request") == "" {
		return nil
	}
	return &HTMXContext{
		IsHTMX:      h.Get("HX-Request") == "true",
		IsBoosted:   h.Get("HX-Boosted") == "true",
		Trigger:     h.Get("HX-Trigger"),
		TriggerName: h.Get("HX-Trigger-Name"),
		Target:      h.Get("HX-Target"),
		CurrentURL:  h.Get("HX-Current-URL"),
	}
}

func IsHTMLContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "text/html")
}

func IsJSONContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "application/json")
}

func IsFormURLEncodedContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "application/x-www-form-urlencoded")
}

func IsMultipartContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "multipart/form-data")
}

func redactFormValues(values url.Values) map[string][]string {
	redacted := make(map[string][]string, len(values))
	for k, v := range values {
		if sensitiveFormFields[strings.ToLower(k)] {
			redacted[k] = []string{"[REDACTED]"}
		} else {
			redacted[k] = v
		}
	}
	return redacted
}

func redactJSONObject(m map[string]any) map[string]any {
	redacted := make(map[string]any, len(m))
	for k, v := range m {
		if sensitiveFormFields[strings.ToLower(k)] {
			redacted[k] = "[REDACTED]"
		} else {
			redacted[k] = v
		}
	}
	return redacted
}

// appendRequestBodyAttrs agrega el request body a attrs, formateado/redactado
// según su Content-Type. Multipart nunca se loguea.
func appendRequestBodyAttrs(attrs []slog.Attr, contentType, body string) []slog.Attr {
	if body == "" {
		return attrs
	}

	switch {
	case IsJSONContentType(contentType):
		var parsedObj map[string]any
		if err := json.Unmarshal([]byte(body), &parsedObj); err == nil {
			if reMarshaled, mErr := json.Marshal(redactJSONObject(parsedObj)); mErr == nil {
				return append(attrs, slog.Any("request_body", json.RawMessage(reMarshaled)))
			}
		}
		var parsedAny any
		if err := json.Unmarshal([]byte(body), &parsedAny); err == nil {
			return append(attrs, slog.Any("request_body", json.RawMessage(body)))
		}
		return append(attrs, slog.String("request_body", body))

	case IsFormURLEncodedContentType(contentType):
		if values, err := url.ParseQuery(body); err == nil {
			return append(attrs, slog.Any("request_body", redactFormValues(values)))
		}
		return append(attrs, slog.String("request_body", body))

	case IsMultipartContentType(contentType):
		return append(attrs,
			slog.String("request_body_type", "multipart/form-data"),
			slog.Int("request_body_size", len(body)),
		)

	default:
		return append(attrs, slog.String("request_body", body))
	}
}

// appendResponseBodyAttrs agrega el response body. HTML se trunca; JSON se loguea completo.
func appendResponseBodyAttrs(attrs []slog.Attr, contentType, body string) []slog.Attr {
	if body == "" {
		return attrs
	}

	if IsHTMLContentType(contentType) {
		preview := body
		if len(preview) > htmlResponseBodyMaxPreview {
			preview = preview[:htmlResponseBodyMaxPreview] + "...[truncated]"
		}
		return append(attrs,
			slog.String("response_body_preview", preview),
			slog.Int("response_body_size", len(body)),
		)
	}

	if IsJSONContentType(contentType) {
		return append(attrs, slog.Any("response_body", json.RawMessage(body)))
	}

	return append(attrs, slog.String("response_body", body))
}

// LogHttpInfo emite UN log estructurado por request, correlacionado vía FromContext.
// Es gin-free: el middleware de cada repo arma el RequestInfo y lo llama.
func LogHttpInfo(ctx context.Context, info RequestInfo, isPanic bool) {
	if isRunningInCloudRun && isProd && info.StatusCode < 400 {
		return
	}

	logger := FromContext(ctx)

	var (
		errorWrapped error
		msg          string
		level        slog.Level
	)
	switch {
	case isPanic:
		if info.Error != nil {
			errorWrapped = info.Error
		} else {
			errorWrapped = fmt.Errorf("panic recovered")
		}
		msg = errorWrapped.Error()
		level = slog.LevelError
	case info.Error != nil:
		errorWrapped = info.Error
		msg = info.Error.Error()
		level = slog.LevelError
	case info.StatusCode >= 500:
		msg = "HTTP server error"
		level = slog.LevelError
	case info.StatusCode >= 400:
		msg = "HTTP client error"
		level = slog.LevelWarn
	default:
		msg = "HTTP request"
		level = slog.LevelInfo
	}

	attrs := []slog.Attr{
		slog.String("duration", fmt.Sprintf("%dms", info.Duration.Milliseconds())),
		slog.String("method", info.Method),
		slog.String("path", info.Path),
		slog.String("ip", info.IP),
		slog.String("user_agent", info.UserAgent),
		slog.Any("headers", redactHeaders(info.Headers)),
		slog.Int("status_code", info.StatusCode),
	}

	if info.Query != "" {
		attrs = append(attrs, slog.String("query", info.Query))
	}

	if info.HTMXContext != nil && info.HTMXContext.IsHTMX {
		attrs = append(attrs, slog.Group("htmx",
			slog.Bool("is_htmx", info.HTMXContext.IsHTMX),
			slog.Bool("is_boosted", info.HTMXContext.IsBoosted),
			slog.String("trigger", info.HTMXContext.Trigger),
			slog.String("trigger_name", info.HTMXContext.TriggerName),
			slog.String("target", info.HTMXContext.Target),
			slog.String("current_url", info.HTMXContext.CurrentURL),
		))
	}

	requestContentType := ""
	if info.Headers != nil {
		if cts, ok := info.Headers["Content-Type"]; ok && len(cts) > 0 {
			requestContentType = cts[0]
		}
	}

	// Bodies solo fuera de prod, para no filtrar PII.
	if !isProd {
		attrs = appendRequestBodyAttrs(attrs, requestContentType, info.RequestBody)
		attrs = appendResponseBodyAttrs(attrs, info.ContentType, info.ResponseBody)
	}

	if errorWrapped != nil {
		stackTrace := ExtractStackTrace(errorWrapped)
		if stackTrace != "" {
			attrs = append(attrs, slog.String("stack_trace", stackTrace))
		}
	}

	logger.LogAttrs(ctx, level, msg, attrs...)
}

func ExtractStackTrace(err error) string {
	type stackTracer interface {
		StackTrace() errors.StackTrace
	}

	var st stackTracer
	current := err
	for current != nil {
		if s, ok := current.(stackTracer); ok {
			st = s
			break
		}
		current = errors.Unwrap(current)
	}

	if st == nil {
		return ""
	}

	var buf bytes.Buffer
	for _, frame := range st.StackTrace() {
		file := fmt.Sprintf("%+s", frame)
		line := fmt.Sprintf("%d", frame)

		if moduleName != "" && strings.Contains(file, moduleName) {
			fmt.Fprintf(&buf, "%s:%s\n", file, line)
		}
	}

	return buf.String()
}

func printLocalAttrs(attrs []slog.Attr) {
	for _, attr := range attrs {
		switch attr.Key {
		case "stack_trace":
			st := slogValueAsPrintableString(attr.Value)
			if st != "" {
				fmt.Printf("\n\033[31mSTACK TRACE:\033[0m\n%s\n", st)
			}
			continue
		}

		if attr.Value.Kind() == slog.KindAny {
			switch attr.Value.Any().(type) {
			case json.RawMessage, []byte:
				fmt.Printf("  \033[34m%s:\033[0m %s\n", attr.Key, slogValueAsPrintableString(attr.Value))
				continue
			}
		}
		fmt.Printf("  \033[34m%s:\033[0m %v\n", attr.Key, attr.Value)
	}
}

func slogValueAsPrintableString(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return fmt.Sprintf("%d", v.Int64())
	case slog.KindUint64:
		return fmt.Sprintf("%d", v.Uint64())
	case slog.KindFloat64:
		return fmt.Sprintf("%g", v.Float64())
	case slog.KindBool:
		return fmt.Sprintf("%t", v.Bool())
	case slog.KindTime:
		return v.Time().String()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindAny:
		switch raw := v.Any().(type) {
		case string:
			return raw
		case json.RawMessage:
			return string(raw)
		case []byte:
			return string(raw)
		default:
			return fmt.Sprint(raw)
		}
	default:
		return v.String()
	}
}
