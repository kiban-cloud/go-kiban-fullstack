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
	// TRACE_ID_HEADER lo mandan nuestros propios servicios para propagar el
	// TRACE_ID del request que originó la cadena. Lleva el TRACE_ID crudo, sin
	// span ni flags. Se manda SIEMPRE, junto con TRACE_HEADER, por dos motivos:
	//
	//  - Los saltos que no son HTTP directo (Cloud Tasks, Pub/Sub push) llegan
	//    con un TRACE_HEADER nuevo que genera la infra al entregar; respetarlo
	//    cortaría la traza original. Por eso este header gana (ver resolveTrace).
	//  - Es un contrato nuestro, no de GCP: si algún día la infra cambia de nube,
	//    la correlación sigue funcionando sin depender de TRACE_HEADER.
	//
	// TRACE_HEADER se sigue propagando en paralelo porque, mientras estemos en
	// GCP, es lo que engancha además el request log que Cloud Run emite solo.
	TRACE_ID_HEADER = "X-Kiban-Trace-Id"
)

// requestIDAttr es el nombre del atributo en el log (no la llave del context).
const requestIDAttr = "request_id"

// tracePathSeparator es el tramo que separa el prefijo del proyecto del TRACE_ID
// en el formato que consume Cloud Logging ("projects/<id>/traces/<TRACE_ID>").
const tracePathSeparator = "/traces/"

// ERROR_MESSAGE_CONTEXT_KEY es la llave (gin/context) donde los handlers dejan el
// error real para que el middleware de logging lo registre una sola vez, en vez
// de que cada capa haga su propio log.
const ERROR_MESSAGE_CONTEXT_KEY = "ERROR_MESSAGE"

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyTrace
	keyTraceID
	keyLabels
)

// WithRequestAndTrace propaga request_id y trace en el context para que los logs
// de negocio (FromContext) se correlacionen con el request que los originó.
func WithRequestAndTrace(ctx context.Context, requestID, trace string) context.Context {
	ctx = context.WithValue(ctx, keyRequestID, requestID)
	if trace != "" {
		ctx = context.WithValue(ctx, keyTrace, trace)
		ctx = context.WithValue(ctx, keyTraceID, rawTraceID(trace))
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

// TraceIDFromContext devuelve el TRACE_ID crudo (sin el prefijo
// "projects/.../traces/"), que es el formato que viaja en los headers de
// correlación hacia otros servicios. TraceFromContext, en cambio, devuelve el
// valor ya formateado para Cloud Logging: ese NO sirve como header.
func TraceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(keyTraceID).(string)
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

	return TraceFromID(parts[0])
}

// TraceFromID formatea un TRACE_ID crudo al valor que Cloud Logging correlaciona
// con Trace. Es la inversa de rawTraceID, y la usa quien recibe un TRACE_ID
// propagado (header propio, mensaje de Pub/Sub) en vez del header de Cloud Run.
func TraceFromID(traceID string) string {
	if traceID == "" {
		return ""
	}

	return fmt.Sprintf("projects/%s%s%s", googleCloudProjectID, tracePathSeparator, traceID)
}

// TraceContextHeader arma el valor de TRACE_HEADER para propagar traceID a otro
// servicio, en el formato que espera GCP ("TRACE_ID/SPAN_ID;o=TRACE_TRUE").
// El SPAN_ID es fijo: no manejamos spans, lo que se propaga es la traza y no la
// jerarquía de llamadas. o=1 la marca como sampleada para que Cloud Trace la
// exporte.
func TraceContextHeader(traceID string) string {
	if traceID == "" {
		return ""
	}

	return traceID + "/1;o=1"
}

// rawTraceID invierte el formateo de ParseTraceContext para recuperar el
// TRACE_ID. Contempla su fallback: cuando el header no trae "/", ParseTraceContext
// devuelve el header tal cual, sin prefijo, y entonces ya es el ID.
func rawTraceID(trace string) string {
	if i := strings.LastIndex(trace, tracePathSeparator); i != -1 {
		return trace[i+len(tracePathSeparator):]
	}
	return trace
}
