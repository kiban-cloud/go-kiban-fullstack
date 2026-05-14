package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/commons"
	infrastructure_common "github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/common"

	"github.com/gin-gonic/gin"
	"github.com/lmittmann/tint"
	"github.com/pkg/errors"
)

var (
	moduleName           string
	isRunningInCloudRun  bool
	env                  commons.ENV
	googleCloudProjectID string
	tenantIDExtractor    func(ctx context.Context) string
)

// sensitiveFormFields are redacted from request bodies even in non-prod.
// Add fields as needed for your domain.
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

// htmlResponseBodyMaxPreview caps the HTML preview logged on errors.
// Full HTML bodies on every 4xx/5xx would balloon Cloud Logging costs.
const htmlResponseBodyMaxPreview = 500

type InitOpts struct {
	LogLevel             commons.LOG_LEVEL
	Env                  commons.ENV
	IsCloudRun           bool
	ModuleName           string
	GoogleCloudProjectID string
	TenantIDExtractor    func(ctx context.Context) string
}

// HTMXContext captures the HTMX-specific request headers for structured logging.
// Use slog.Group("htmx", ...) to emit these as a nested object in Cloud Logging.
type HTMXContext struct {
	IsHTMX      bool
	IsBoosted   bool
	Trigger     string
	TriggerName string
	Target      string
	CurrentURL  string
}

type RequestErrorInfo struct {
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

func Init(opts InitOpts) {
	moduleName = opts.ModuleName
	isRunningInCloudRun = opts.IsCloudRun
	env = opts.Env
	googleCloudProjectID = opts.GoogleCloudProjectID
	tenantIDExtractor = opts.TenantIDExtractor

	level := parseLogLevel(opts.LogLevel, opts.IsCloudRun)

	var handler slog.Handler
	if opts.IsCloudRun {
		handlerOpts := &slog.HandlerOptions{
			Level:     level,
			AddSource: false,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				switch a.Key {
				case slog.LevelKey:
					a.Key = "severity"
				case slog.MessageKey:
					a.Key = "message"
				}
				return a
			},
		}
		handler = slog.NewJSONHandler(os.Stdout, handlerOpts)
	} else {
		handler = tint.NewHandler(os.Stdout, &tint.Options{
			Level:      level,
			TimeFormat: time.Kitchen,
			AddSource:  false,
			NoColor:    false,
		})
	}

	interceptor := &MyInterceptor{next: handler}
	logger := slog.New(interceptor)
	slog.SetDefault(logger)

	slog.Info("Logger initialized",
		slog.Bool("is_cloud_run", opts.IsCloudRun),
		slog.String("log_level", string(opts.LogLevel)),
		slog.String("environment", string(opts.Env)),
		slog.String("module", opts.ModuleName),
	)
}

func parseLogLevel(lvl commons.LOG_LEVEL, isCloudRun bool) slog.Level {
	if !isCloudRun {
		return slog.LevelDebug
	}

	switch lvl {
	case commons.LOG_LEVELS.DEBUG:
		return slog.LevelDebug
	case commons.LOG_LEVELS.INFO:
		return slog.LevelInfo
	case commons.LOG_LEVELS.WARN:
		return slog.LevelWarn
	case commons.LOG_LEVELS.ERROR:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func FromContext(ctx context.Context) *slog.Logger {
	attrs := []any{}
	if requestID, ok := ctx.Value(infrastructure_common.REQUEST_ID).(string); ok {
		attrs = append(attrs, slog.String(infrastructure_common.REQUEST_ID, requestID))
	}

	if trace, ok := ctx.Value(infrastructure_common.TRACE_KEY).(string); ok && trace != "" {
		attrs = append(attrs,
			slog.String("logging.googleapis.com/trace", trace),
			slog.Bool("logging.googleapis.com/trace_sampled", true),
		)
	}

	if tenantIDExtractor != nil {
		if tid := tenantIDExtractor(ctx); tid != "" {
			attrs = append(attrs, slog.String("tenant_id", tid))
		}
	}

	return slog.Default().With(attrs...)
}

// IsHTMLContentType returns true if the content type indicates an HTML response.
func IsHTMLContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "text/html")
}

// IsJSONContentType returns true if the content type indicates a JSON request/response.
func IsJSONContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "application/json")
}

// IsFormURLEncodedContentType returns true for traditional form submissions.
func IsFormURLEncodedContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "application/x-www-form-urlencoded")
}

// IsMultipartContentType returns true for multipart form data (file uploads).
func IsMultipartContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "multipart/form-data")
}

// redactFormValues redacts sensitive keys from parsed form values.
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

// redactJSONObject redacts sensitive keys from a parsed JSON object map.
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

// appendRequestBodyAttrs adds the request body to attrs, formatted/redacted
// according to its Content-Type. Multipart is never logged.
func appendRequestBodyAttrs(attrs []slog.Attr, contentType, body string) []slog.Attr {
	if body == "" {
		return attrs
	}

	switch {
	case IsJSONContentType(contentType):
		var parsed map[string]any
		if err := json.Unmarshal([]byte(body), &parsed); err == nil {
			return append(attrs, slog.Any("request_body", redactJSONObject(parsed)))
		}
		// Fallback if it's a JSON array or scalar that doesn't unmarshal to map
		return append(attrs, slog.String("request_body", body))

	case IsFormURLEncodedContentType(contentType):
		if values, err := url.ParseQuery(body); err == nil {
			return append(attrs, slog.Any("request_body", redactFormValues(values)))
		}
		return append(attrs, slog.String("request_body", body))

	case IsMultipartContentType(contentType):
		// Never log multipart bodies — they contain binary data and possibly files
		return append(attrs,
			slog.String("request_body_type", "multipart/form-data"),
			slog.Int("request_body_size", len(body)),
		)

	default:
		return append(attrs, slog.String("request_body", body))
	}
}

// appendResponseBodyAttrs adds the response body to attrs.
// HTML responses are truncated since they can be very large.
// JSON responses are logged in full as raw JSON.
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

func LogHTTPError(c *gin.Context, ctx context.Context, errCtx RequestErrorInfo, isPanic bool) {
	logger := FromContext(ctx)

	var errorWrapped error
	if errCtx.Error != nil {
		errorWrapped = errCtx.Error
	} else {
		if isPanic {
			errorWrapped = fmt.Errorf("panic recovered")
		} else {
			errorWrapped = fmt.Errorf("HTTP error")
		}
	}

	attrs := []slog.Attr{
		slog.String("duration", fmt.Sprintf("%dms", errCtx.Duration.Milliseconds())),
		slog.String("method", errCtx.Method),
		slog.String("path", errCtx.Path),
		slog.String("ip", errCtx.IP),
		slog.String("user_agent", errCtx.UserAgent),
		slog.Any("headers", errCtx.Headers),
		slog.Int("status_code", errCtx.StatusCode),
	}

	if errCtx.Query != "" {
		attrs = append(attrs, slog.String("query", errCtx.Query))
	}

	// HTMX context (only emitted for HTMX requests)
	if errCtx.HTMXContext != nil && errCtx.HTMXContext.IsHTMX {
		attrs = append(attrs, slog.Group("htmx",
			slog.Bool("is_htmx", errCtx.HTMXContext.IsHTMX),
			slog.Bool("is_boosted", errCtx.HTMXContext.IsBoosted),
			slog.String("trigger", errCtx.HTMXContext.Trigger),
			slog.String("trigger_name", errCtx.HTMXContext.TriggerName),
			slog.String("target", errCtx.HTMXContext.Target),
			slog.String("current_url", errCtx.HTMXContext.CurrentURL),
		))
	}

	// Request/response bodies — only in non-prod to avoid PII leakage
	requestContentType := ""
	if errCtx.Headers != nil {
		if cts, ok := errCtx.Headers["Content-Type"]; ok && len(cts) > 0 {
			requestContentType = cts[0]
		}
	}

	if isRunningInCloudRun && env != commons.ENVS.PROD {
		attrs = appendRequestBodyAttrs(attrs, requestContentType, errCtx.RequestBody)
		attrs = appendResponseBodyAttrs(attrs, errCtx.ContentType, errCtx.ResponseBody)
	} else if !isRunningInCloudRun {
		// Local dev: log everything for easier debugging
		attrs = appendRequestBodyAttrs(attrs, requestContentType, errCtx.RequestBody)
		attrs = appendResponseBodyAttrs(attrs, errCtx.ContentType, errCtx.ResponseBody)
	}

	if stackTrace := ExtractStackTrace(errorWrapped); stackTrace != "" {
		attrs = append(attrs, slog.String("stack_trace", stackTrace))
	}

	logger.LogAttrs(ctx, slog.LevelError, errorWrapped.Error(), attrs...)
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

func ParseTraceContext(header string) (traceID string) {
	if header == "" {
		return ""
	}

	parts := strings.Split(header, "/")
	if len(parts) < 2 {
		return header
	}
	traceID = parts[0]

	return fmt.Sprintf("projects/%s/traces/%s", googleCloudProjectID, traceID)
}

type MyInterceptor struct {
	next        slog.Handler
	prefixAttrs []slog.Attr
}

func (h *MyInterceptor) Handle(ctx context.Context, r slog.Record) error {
	if isRunningInCloudRun {
		return h.next.Handle(ctx, r)
	}

	var attrs []slog.Attr
	if len(h.prefixAttrs) > 0 {
		attrs = append(attrs, h.prefixAttrs...)
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	clean := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	if err := h.next.Handle(ctx, clean); err != nil {
		return err
	}

	printLocalAttrs(attrs)
	return nil
}

func printLocalAttrs(attrs []slog.Attr) {
	for _, attr := range attrs {
		switch attr.Key {
		case "stack_trace":
			st := slogValueAsPrintableString(attr.Value)
			if st != "" {
				fmt.Printf("\n\033[31mSTACK TRACE:\033[0m\n%s\n", st)
			}
		case "request_body":
			fmt.Printf("  \033[34mrequest_body:\033[0m %s\n", slogValueAsPrintableString(attr.Value))
		default:
			// []byte / json.RawMessage values (e.g. response_body for JSON
			// responses, see appendResponseBodyAttrs) are stored as
			// slog.KindAny. The default `%v` formatter prints them as a
			// slice of integers (`[123 34 ...]`) which is unreadable. Show
			// them as the underlying string instead.
			if attr.Value.Kind() == slog.KindAny {
				switch v := attr.Value.Any().(type) {
				case json.RawMessage:
					fmt.Printf("  \033[34m%s:\033[0m %s\n", attr.Key, string(v))
					continue
				case []byte:
					fmt.Printf("  \033[34m%s:\033[0m %s\n", attr.Key, string(v))
					continue
				}
			}
			fmt.Printf("  \033[34m%s:\033[0m %v\n", attr.Key, attr.Value)
		}
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
		if s, ok := v.Any().(string); ok {
			return s
		}
		return fmt.Sprint(v.Any())
	default:
		return v.String()
	}
}

func (h *MyInterceptor) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *MyInterceptor) WithAttrs(attrs []slog.Attr) slog.Handler {
	if isRunningInCloudRun {
		return &MyInterceptor{next: h.next.WithAttrs(attrs)}
	}
	prefix := make([]slog.Attr, 0, len(h.prefixAttrs)+len(attrs))
	prefix = append(prefix, h.prefixAttrs...)
	prefix = append(prefix, attrs...)
	return &MyInterceptor{next: h.next, prefixAttrs: prefix}
}

func (h *MyInterceptor) WithGroup(name string) slog.Handler {
	if isRunningInCloudRun {
		return &MyInterceptor{next: h.next.WithGroup(name)}
	}
	return &MyInterceptor{
		next:        h.next.WithGroup(name),
		prefixAttrs: slices.Clone(h.prefixAttrs),
	}
}