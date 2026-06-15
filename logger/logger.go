// Package logger es el sistema de logging compartido de Kiban: slog estructurado
// hacia stdout (Cloud Logging) con salida coloreada en local. Es la ÚNICA fuente
// de verdad; tanto go-kiban-fullstack como go-kiban (y cualquier servicio) lo
// importan en vez de mantener copias. Es gin-free a propósito: el middleware HTTP
// vive en cada repo y solo arma un RequestInfo y llama a LogHttpInfo.
package logger

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/lmittmann/tint"
)

type LOG_LEVEL string

const (
	LOG_LEVEL_DEBUG LOG_LEVEL = "debug"
	LOG_LEVEL_INFO  LOG_LEVEL = "info"
	LOG_LEVEL_WARN  LOG_LEVEL = "warn"
	LOG_LEVEL_ERROR LOG_LEVEL = "error"
)

// LevelCritical es un nivel por encima de slog.LevelError; el handler lo emite
// como severity "CRITICAL" (que Cloud Logging reconoce). slog no tiene un nivel
// crítico nativo. Úsalo para errores fatales (panics, fallos de arranque).
const LevelCritical = slog.Level(12)

var (
	moduleName           string
	isRunningInCloudRun  bool
	isProd               bool
	googleCloudProjectID string
	labelsExtractor      func(ctx context.Context) map[string]string
)

type InitOpts struct {
	LogLevel             LOG_LEVEL
	IsProd               bool
	IsCloudRun           bool
	ModuleName           string
	GoogleCloudProjectID string
	// LabelsExtractor devuelve las labels específicas del proyecto (tenant_id,
	// spaceId, companyId, email, ...) a partir del context del request. Cada
	// servicio lo cablea en su Init según su modelo de autorización.
	LabelsExtractor func(ctx context.Context) map[string]string
}

func Init(opts InitOpts) {
	moduleName = opts.ModuleName
	isRunningInCloudRun = opts.IsCloudRun
	isProd = opts.IsProd
	googleCloudProjectID = opts.GoogleCloudProjectID
	labelsExtractor = opts.LabelsExtractor

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
					if lvl, ok := a.Value.Any().(slog.Level); ok && lvl >= LevelCritical {
						a.Value = slog.StringValue("CRITICAL")
					}
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
		slog.Bool("is_prod", opts.IsProd),
		slog.String("module", opts.ModuleName),
	)
}

func parseLogLevel(lvl LOG_LEVEL, isCloudRun bool) slog.Level {
	if !isCloudRun {
		return slog.LevelDebug
	}

	switch lvl {
	case LOG_LEVEL_DEBUG:
		return slog.LevelDebug
	case LOG_LEVEL_INFO:
		return slog.LevelInfo
	case LOG_LEVEL_WARN:
		return slog.LevelWarn
	case LOG_LEVEL_ERROR:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// FromContext devuelve un *slog.Logger con request_id, trace y las labels del
// proyecto ya inyectados. Es como el código de negocio correlaciona sus logs.
func FromContext(ctx context.Context) *slog.Logger {
	attrs := []any{}
	if requestID := RequestIDFromContext(ctx); requestID != "" {
		attrs = append(attrs, slog.String(requestIDAttr, requestID))
	}

	if trace := TraceFromContext(ctx); trace != "" {
		attrs = append(attrs,
			slog.String("logging.googleapis.com/trace", trace),
			slog.Bool("logging.googleapis.com/trace_sampled", true),
		)
	}

	// Labels stasheadas por el middleware (vía WithLabels) + el extractor opcional
	// del Init (compat: datos-non-stop/reportalos lo usan para tenant_id).
	for k, v := range labelsFromContext(ctx) {
		if v != "" {
			attrs = append(attrs, slog.String(k, v))
		}
	}
	if labelsExtractor != nil {
		for k, v := range labelsExtractor(ctx) {
			if v != "" {
				attrs = append(attrs, slog.String(k, v))
			}
		}
	}

	return slog.Default().With(attrs...)
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
