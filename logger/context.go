package logger

import (
	"context"
	"fmt"
	"strings"
)

// Headers de correlación. Cloud Run inyecta TRACE_HEADER automáticamente; el
// middleware de cada repo genera REQUEST_ID_HEADER y lo devuelve al cliente.
const (
	REQUEST_ID_HEADER = "X-Request-ID"
	TRACE_HEADER      = "X-Cloud-Trace-Context"
)

// requestIDAttr es el nombre del atributo en el log (no la llave del context).
const requestIDAttr = "request_id"

// ERROR_MESSAGE_CONTEXT_KEY es la llave (gin/context) donde los handlers dejan el
// error real para que el middleware de logging lo registre una sola vez, en vez
// de que cada capa haga su propio log.
const ERROR_MESSAGE_CONTEXT_KEY = "ERROR_MESSAGE"

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyTrace
	keyLabels
)

// WithRequestAndTrace propaga request_id y trace en el context para que los logs
// de negocio (FromContext) se correlacionen con el request que los originó.
func WithRequestAndTrace(ctx context.Context, requestID, trace string) context.Context {
	ctx = context.WithValue(ctx, keyRequestID, requestID)
	if trace != "" {
		ctx = context.WithValue(ctx, keyTrace, trace)
	}
	return ctx
}

// WithLabels guarda labels por-request (tenant/auth tags) en el context. El
// middleware las setea desde su hook Labels; FromContext las adjunta al log.
// Cada proyecto decide cómo extraerlas (de gin context, de request.Context, etc.).
func WithLabels(ctx context.Context, labels map[string]string) context.Context {
	if len(labels) == 0 {
		return ctx
	}
	return context.WithValue(ctx, keyLabels, labels)
}

func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(keyRequestID).(string)
	return v
}

func TraceFromContext(ctx context.Context) string {
	v, _ := ctx.Value(keyTrace).(string)
	return v
}

func labelsFromContext(ctx context.Context) map[string]string {
	v, _ := ctx.Value(keyLabels).(map[string]string)
	return v
}

// ParseTraceContext convierte el header X-Cloud-Trace-Context ("TRACE_ID/SPAN_ID;o=1")
// al formato "projects/PROJECT_ID/traces/TRACE_ID" que Cloud Logging correlaciona con Trace.
func ParseTraceContext(header string) string {
	if header == "" {
		return ""
	}

	parts := strings.Split(header, "/")
	if len(parts) < 2 {
		return header
	}
	traceID := parts[0]

	return fmt.Sprintf("projects/%s/traces/%s", googleCloudProjectID, traceID)
}
