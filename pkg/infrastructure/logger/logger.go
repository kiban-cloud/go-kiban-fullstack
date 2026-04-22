package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

type InitOpts struct {
	LogLevel             commons.LOG_LEVEL
	Env                  commons.ENV
	IsCloudRun           bool
	ModuleName           string
	GoogleCloudProjectID string
	// TenantIDExtractor, if non-nil, is invoked by FromContext to add a
	// tenant_id slog attribute. Lets projects wire tenant enrichment without
	// coupling the shared logger to their Tenant type.
	TenantIDExtractor func(ctx context.Context) string
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

	interceptor := &MyInterceptor{
		next: handler,
	}

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

// FromContext returns a logger enriched with request_id and trace from ctx.
// Projects that want to add tenant_id or other fields should call
// FromContext(ctx).With(slog.String("tenant_id", ...)) themselves.
func FromContext(ctx context.Context) *slog.Logger {
	attrs := []any{}
	if requestID, ok := ctx.Value(infrastructure_common.REQUEST_ID).(string); ok {
		attrs = append(attrs, slog.String(infrastructure_common.REQUEST_ID, requestID))
	}

	if trace, ok := ctx.Value(infrastructure_common.TRACE_KEY).(string); ok && trace != "" {
		attrs = append(attrs, slog.String("logging.googleapis.com/trace", trace), slog.Bool("logging.googleapis.com/trace_sampled", true))
	}

	if tenantIDExtractor != nil {
		if tid := tenantIDExtractor(ctx); tid != "" {
			attrs = append(attrs, slog.String("tenant_id", tid))
		}
	}

	return slog.Default().With(attrs...)
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

	stackTrace := ExtractStackTrace(errorWrapped)

	if errCtx.Query != "" {
		attrs = append(attrs, slog.String("query", errCtx.Query))
	}

	if isRunningInCloudRun && env != commons.ENVS.PROD && errCtx.RequestBody != "" {
		if strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
			var requestBody map[string]any
			if err := json.Unmarshal([]byte(errCtx.RequestBody), &requestBody); err != nil {
				attrs = append(attrs, slog.String("request_body", errCtx.RequestBody))
			} else {
				attrs = append(attrs, slog.Any("request_body", requestBody))
			}
		} else {
			attrs = append(attrs, slog.String("request_body", errCtx.RequestBody))
		}
	}

	if isRunningInCloudRun && env != commons.ENVS.PROD && errCtx.ResponseBody != "" {
		attrs = append(attrs, slog.Any("response_body", json.RawMessage(errCtx.ResponseBody)))
	}

	if !isRunningInCloudRun && errCtx.RequestBody != "" {
		attrs = append(attrs, slog.String("request_body", errCtx.RequestBody))
	}

	if stackTrace != "" {
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
