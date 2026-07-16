package logger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestRawTraceID cubre la extracción del TRACE_ID desde el valor ya formateado
// para Cloud Logging, incluyendo el fallback de ParseTraceContext (header sin
// "/" se devuelve tal cual, sin prefijo de proyecto).
func TestRawTraceID(t *testing.T) {
	cases := []struct {
		name  string
		trace string
		want  string
	}{
		{
			name:  "formateado para cloud logging",
			trace: "projects/my-project/traces/105445aa7843bc8bf206b12000100000",
			want:  "105445aa7843bc8bf206b12000100000",
		},
		{
			name:  "sin prefijo (fallback de ParseTraceContext)",
			trace: "105445aa7843bc8bf206b12000100000",
			want:  "105445aa7843bc8bf206b12000100000",
		},
		{
			name:  "vacio",
			trace: "",
			want:  "",
		},
		{
			name:  "project id con guiones y numeros",
			trace: "projects/kiban-prod-123/traces/abc123",
			want:  "abc123",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rawTraceID(tc.trace); got != tc.want {
				t.Errorf("rawTraceID(%q) = %q, want %q", tc.trace, got, tc.want)
			}
		})
	}
}

// TestTraceIDFromContext_RoundTrip verifica el ciclo completo que corre el
// middleware: header entrante -> ParseTraceContext -> context. TraceFromContext
// debe devolver el valor formateado (el que consume Cloud Logging) y
// TraceIDFromContext el TRACE_ID crudo (el que se propaga como header).
func TestTraceIDFromContext_RoundTrip(t *testing.T) {
	original := googleCloudProjectID
	googleCloudProjectID = "my-project"
	t.Cleanup(func() { googleCloudProjectID = original })

	const traceID = "105445aa7843bc8bf206b12000100000"
	header := traceID + "/1;o=1"

	ctx := WithRequestAndTrace(context.Background(), "req-1", ParseTraceContext(header))

	if got, want := TraceFromContext(ctx), "projects/my-project/traces/"+traceID; got != want {
		t.Errorf("TraceFromContext() = %q, want %q", got, want)
	}
	if got := TraceIDFromContext(ctx); got != traceID {
		t.Errorf("TraceIDFromContext() = %q, want %q", got, traceID)
	}
	if got := RequestIDFromContext(ctx); got != "req-1" {
		t.Errorf("RequestIDFromContext() = %q, want %q", got, "req-1")
	}
}

// TestTraceIDFromContext_SinTrace cubre el caso de un request sin header de
// trace: WithRequestAndTrace no debe guardar nada y los getters devuelven "".
func TestTraceIDFromContext_SinTrace(t *testing.T) {
	ctx := WithRequestAndTrace(context.Background(), "req-1", ParseTraceContext(""))

	if got := TraceFromContext(ctx); got != "" {
		t.Errorf("TraceFromContext() = %q, want %q", got, "")
	}
	if got := TraceIDFromContext(ctx); got != "" {
		t.Errorf("TraceIDFromContext() = %q, want %q", got, "")
	}
}

// TestTraceIDFromContext_ContextVacio garantiza que los getters son seguros
// sobre un context que nunca pasó por el middleware.
func TestTraceIDFromContext_ContextVacio(t *testing.T) {
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Errorf("TraceIDFromContext() = %q, want %q", got, "")
	}
}

// TestResolveTrace cubre la precedencia entre el header propio y el de Cloud
// Run. El caso central es el salto no-HTTP (Cloud Tasks / Pub/Sub push): llegan
// ambos headers, y respetar el de Cloud Run cortaría la traza original.
func TestResolveTrace(t *testing.T) {
	original := googleCloudProjectID
	googleCloudProjectID = "my-project"
	t.Cleanup(func() { googleCloudProjectID = original })

	gin.SetMode(gin.TestMode)

	cases := []struct {
		name        string
		traceIDHdr  string
		cloudRunHdr string
		want        string
	}{
		{
			name:        "solo header de cloud run",
			cloudRunHdr: "abc123/1;o=1",
			want:        "projects/my-project/traces/abc123",
		},
		{
			name:       "solo header propio",
			traceIDHdr: "abc123",
			want:       "projects/my-project/traces/abc123",
		},
		{
			name:        "ambos: gana el propio",
			traceIDHdr:  "original123",
			cloudRunHdr: "generado456/1;o=1",
			want:        "projects/my-project/traces/original123",
		},
		{
			name: "ninguno",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.traceIDHdr != "" {
				c.Request.Header.Set(TRACE_ID_HEADER, tc.traceIDHdr)
			}
			if tc.cloudRunHdr != "" {
				c.Request.Header.Set(TRACE_HEADER, tc.cloudRunHdr)
			}

			if got := resolveTrace(c); got != tc.want {
				t.Errorf("resolveTrace() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTraceContextHeader_RoundTrip verifica que el valor que emitimos para
// TRACE_HEADER es el que ParseTraceContext sabe leer del otro lado: emisor y
// receptor tienen que coincidir para que propagar el header de GCP sirva.
func TestTraceContextHeader_RoundTrip(t *testing.T) {
	original := googleCloudProjectID
	googleCloudProjectID = "my-project"
	t.Cleanup(func() { googleCloudProjectID = original })

	if got := TraceContextHeader(""); got != "" {
		t.Errorf("TraceContextHeader(\"\") = %q, want %q", got, "")
	}

	const traceID = "105445aa7843bc8bf206b12000100000"

	header := TraceContextHeader(traceID)
	if want := traceID + "/1;o=1"; header != want {
		t.Errorf("TraceContextHeader(%q) = %q, want %q", traceID, header, want)
	}

	if got, want := ParseTraceContext(header), TraceFromID(traceID); got != want {
		t.Errorf("ParseTraceContext(TraceContextHeader(...)) = %q, want %q", got, want)
	}
}

// TestTraceFromID_RoundTrip verifica que TraceFromID y rawTraceID son inversas:
// es lo que sostiene la propagación (crudo -> header -> crudo de nuevo).
func TestTraceFromID_RoundTrip(t *testing.T) {
	original := googleCloudProjectID
	googleCloudProjectID = "my-project"
	t.Cleanup(func() { googleCloudProjectID = original })

	if got := TraceFromID(""); got != "" {
		t.Errorf("TraceFromID(\"\") = %q, want %q", got, "")
	}

	const traceID = "105445aa7843bc8bf206b12000100000"
	if got := rawTraceID(TraceFromID(traceID)); got != traceID {
		t.Errorf("rawTraceID(TraceFromID(%q)) = %q, want %q", traceID, got, traceID)
	}
}
