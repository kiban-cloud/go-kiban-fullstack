package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// ansiEscape matches any CSI escape sequence (e.g. \x1b[34m). printLocalAttrs
// emits ANSI colors for the local tint handler, so tests strip them before
// substring matching.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// TestPrintLocalAttrs_JSONRawMessage covers the regression where a
// `response_body` stored as `json.RawMessage` (a []byte alias) was being
// printed by the tint local handler as a slice of integers, e.g.
// `response_body: [123 34 109 ...]`, instead of as readable JSON text.
//
// The fix special-cases json.RawMessage and []byte in printLocalAttrs so they
// render as their underlying string. Cloud-run JSON output is unaffected.
func TestPrintLocalAttrs_JSONRawMessage(t *testing.T) {
	cases := []struct {
		name  string
		attr  slog.Attr
		want  string // substring expected in stdout
		notIn string // substring that must NOT appear (the regression form)
	}{
		{
			name:  "json.RawMessage prints as string",
			attr:  slog.Any("response_body", json.RawMessage(`{"message":"unauthorized"}`)),
			want:  `response_body:` + " " + `{"message":"unauthorized"}`,
			notIn: "[123 34 109", // the byte-slice form that was the regression
		},
		{
			name:  "raw []byte prints as string",
			attr:  slog.Any("response_body", []byte(`{"ok":true}`)),
			want:  `response_body:` + " " + `{"ok":true}`,
			notIn: "[123",
		},
		{
			name:  "regular string still prints normally",
			attr:  slog.String("path", "/api/v1/banxico/cep-pdf"),
			want:  "path: /api/v1/banxico/cep-pdf",
			notIn: "[",
		},
		{
			name:  "arbitrary map (non-body attrs) still prints as map[...]",
			attr:  slog.Any("headers", map[string][]string{"X-Api-Key": {"redacted"}}),
			want:  "map[X-Api-Key:",
			notIn: "[120 ", // byte-slice form
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := captureStdout(t, func() {
				printLocalAttrs([]slog.Attr{tc.attr})
			})
			out := ansiEscape.ReplaceAllString(raw, "")
			if !strings.Contains(out, tc.want) {
				t.Errorf("expected output to contain %q, got:\n%s", tc.want, out)
			}
			if tc.notIn != "" && strings.Contains(out, tc.notIn) {
				t.Errorf("output should NOT contain %q (regression marker), got:\n%s", tc.notIn, out)
			}
		})
	}
}

// TestAppendRequestBodyAttrs_JSONRendersAsRawMessage covers the alignment of
// request_body and response_body output: a JSON request body must end up as
// a json.RawMessage in the slog.Attr (not as a map), so local dev shows it
// as `{"k":"v"}` and Cloud Logging emits it as a nested JSON object. The
// previous version stored a map[string]any which printed as `map[k:v ...]`.
//
// Redaction is verified end-to-end: sensitive keys are stripped from the
// final JSON string.
func TestAppendRequestBodyAttrs_JSONRendersAsRawMessage(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		wantRaw     []string // substrings that must appear in the resulting JSON
		notIn       []string // substrings that must NOT appear (e.g. secret values)
	}{
		{
			name:        "JSON object → RawMessage with redaction applied",
			contentType: "application/json",
			body:        `{"email":"a@b.com","password":"hunter2"}`,
			wantRaw:     []string{`"email":"a@b.com"`, `"password":"[REDACTED]"`},
			notIn:       []string{"hunter2", "map["},
		},
		{
			name:        "JSON object → no map[...] notation",
			contentType: "application/json",
			body:        `{"folio":"F-001","total":3441.79}`,
			wantRaw:     []string{`"folio":"F-001"`, `"total":3441.79`},
			notIn:       []string{"map[", "[102", "[123"}, // byte-slice regression
		},
		{
			name:        "JSON array → passed through verbatim",
			contentType: "application/json",
			body:        `[{"id":1},{"id":2}]`,
			wantRaw:     []string{`[{"id":1},{"id":2}]`},
			notIn:       []string{"map[", "[91 "},
		},
		{
			name:        "Malformed JSON → falls back to plain string",
			contentType: "application/json",
			body:        `{not-json`,
			wantRaw:     []string{`{not-json`},
			notIn:       []string{"[123"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attrs := appendRequestBodyAttrs(nil, tc.contentType, tc.body)
			if len(attrs) == 0 {
				t.Fatalf("expected an attribute, got none")
			}

			// Render via printLocalAttrs so we exercise the same code path
			// users see in local dev.
			raw := captureStdout(t, func() { printLocalAttrs(attrs) })
			out := ansiEscape.ReplaceAllString(raw, "")

			for _, want := range tc.wantRaw {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q, got:\n%s", want, out)
				}
			}
			for _, bad := range tc.notIn {
				if strings.Contains(out, bad) {
					t.Errorf("output should NOT contain %q (regression marker), got:\n%s", bad, out)
				}
			}
		})
	}
}

// TestAppendRequestBodyAttrs_FormStillUsesMap confirms form-urlencoded
// requests still get the map rendering — the JSON alignment only applies to
// `application/json`. Form bodies are url.Values (map[string][]string) and
// `map[k:[v] ...]` is the readable format for those.
func TestAppendRequestBodyAttrs_FormStillUsesMap(t *testing.T) {
	attrs := appendRequestBodyAttrs(nil, "application/x-www-form-urlencoded", "email=a%40b.com&password=hunter2")
	raw := captureStdout(t, func() { printLocalAttrs(attrs) })
	out := ansiEscape.ReplaceAllString(raw, "")

	if !strings.Contains(out, "map[") {
		t.Errorf("expected form body to render as map[...], got:\n%s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected password to be redacted, got:\n%s", out)
	}
	if strings.Contains(out, "hunter2") {
		t.Errorf("password should not appear in output, got:\n%s", out)
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns what
// was written. printLocalAttrs writes to stdout directly via fmt.Printf, so
// the simplest test setup is to swap stdout for a pipe and read it back.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()

	_ = w.Close()
	<-done
	os.Stdout = orig
	_ = r.Close()
	return buf.String()
}

// TestLogHttpInfo_NoNilDeref_OnSuccess is the regression test for the bug
// reported as "Postman sees 200 but the log shows ERR runtime error: nil
// pointer dereference / status 500". Root cause: when the handler returned
// successfully and nothing tagged an error, info.Error was nil and
// isPanic was false, but the post-handler code unconditionally called
// errorWrapped.Error() — panic on nil interface. The deferred recover then
// emitted a confusing fake 500.
//
// This test calls LogHttpInfo with the exact shape of a successful 200
// (no Error, no panic) and asserts:
//   - no panic
//   - the emitted log is at LevelInfo, not LevelError
func TestLogHttpInfo_NoNilDeref_OnSuccess(t *testing.T) {
	// Build a logger that writes JSON to a buffer so we can inspect the level.
	var sink bytes.Buffer
	handler := slog.NewJSONHandler(&sink, &slog.HandlerOptions{Level: slog.LevelDebug})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(prev)

	// Simulate non-prod runtime so the early-return at the top of LogHttpInfo
	// (`isRunningInCloudRun && PROD && status<400`) doesn't bypass the buggy
	// code path.
	isRunningInCloudRun = false
	defer func() { isRunningInCloudRun = false }()

	info := RequestInfo{
		Method:       "POST",
		Path:         "/api/v1/sepomex/codigo-postal",
		IP:           "::1",
		UserAgent:    "PostmanRuntime/7.54.0",
		StatusCode:   200,
		Duration:     150 * time.Millisecond,
		Headers:      map[string][]string{"Content-Type": {"application/json"}},
		RequestBody:  `{"codigoPostal":"06700"}`,
		ResponseBody: `{"ok":true}`,
		ContentType:  "application/json",
		Error:        nil, // critical: no tagged error
	}

	// Must not panic.
	LogHttpInfo(context.Background(), info, false)

	out := sink.String()
	if !strings.Contains(out, `"level":"INFO"`) {
		t.Errorf("expected a LevelInfo log for status 200, got:\n%s", out)
	}
	if strings.Contains(out, "nil pointer") || strings.Contains(out, "runtime error") {
		t.Errorf("log should not contain panic markers, got:\n%s", out)
	}
	if strings.Contains(out, `"status_code":500`) {
		t.Errorf("status_code should be 200, not a fake 500, got:\n%s", out)
	}
}

// TestLogHttpInfo_LevelMapping covers the level-by-status policy so future
// changes don't accidentally swap them.
func TestLogHttpInfo_LevelMapping(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		err       error
		isPanic   bool
		wantLevel string
	}{
		{"200 no error → INFO", 200, nil, false, `"level":"INFO"`},
		{"302 redirect → INFO", 302, nil, false, `"level":"INFO"`},
		{"404 → WARN", 404, nil, false, `"level":"WARN"`},
		{"500 → ERROR", 500, nil, false, `"level":"ERROR"`},
		{"any status with tagged err → ERROR", 200, fmtErr("tagged"), false, `"level":"ERROR"`},
		{"panic → ERROR", 500, nil, true, `"level":"ERROR"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sink bytes.Buffer
			handler := slog.NewJSONHandler(&sink, &slog.HandlerOptions{Level: slog.LevelDebug})
			prev := slog.Default()
			slog.SetDefault(slog.New(handler))
			defer slog.SetDefault(prev)

			isRunningInCloudRun = false

			info := RequestInfo{
				Method:     "GET",
				Path:       "/x",
				StatusCode: tc.status,
				Headers:    map[string][]string{"Content-Type": {"application/json"}},
				Error:      tc.err,
			}
			LogHttpInfo(context.Background(), info, tc.isPanic)

			if !strings.Contains(sink.String(), tc.wantLevel) {
				t.Errorf("expected %s, got:\n%s", tc.wantLevel, sink.String())
			}
		})
	}
}

// TestFromContext_NoDuplicateCorrelationFromLabels es la regresión del trace
// duplicado en el log grueso: FromContext adjunta request_id/trace desde el
// context, y si las labels stasheadas (p.ej. los tags de auth) también traen
// esas keys, no deben re-agregarse (Cloud Logging las fusionaba → "traceXtraceX").
func TestFromContext_NoDuplicateIPFromLabels(t *testing.T) {
	var sink bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&sink, nil)))
	defer slog.SetDefault(prev)

	// Los tags de auth traen "ip" como label; el log grueso (LogHttpInfo) emite
	// su propio "ip" explícito. Sin el skip, la key saldría dos veces.
	ctx := WithLabels(context.Background(), map[string]string{
		"ip":        "10.0.0.1",
		"tenant_id": "t-1",
	})

	FromContext(ctx).LogAttrs(ctx, slog.LevelInfo, "http request", slog.String("ip", "9.9.9.9"))
	out := sink.String()

	if n := strings.Count(out, `"ip":`); n != 1 {
		t.Errorf("expected ip key exactly once, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, `"ip":"9.9.9.9"`) {
		t.Errorf("expected the explicit gross-log ip to win:\n%s", out)
	}
	if !strings.Contains(out, `"tenant_id":"t-1"`) {
		t.Errorf("expected tenant_id label preserved:\n%s", out)
	}
}

func TestFromContext_NoDuplicateCorrelationFromLabels(t *testing.T) {
	var sink bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&sink, nil)))
	defer slog.SetDefault(prev)

	trace := "projects/p/traces/abc123"
	ctx := WithRequestAndTrace(context.Background(), "req-1", trace)
	// Simula que las labels (tags de auth) también traen las keys de correlación.
	ctx = WithLabels(ctx, map[string]string{
		"logging.googleapis.com/trace": trace,
		"request_id":                   "req-1",
		"tenant_id":                    "t-1",
	})

	FromContext(ctx).Info("hello")
	out := sink.String()

	if n := strings.Count(out, `"logging.googleapis.com/trace":`); n != 1 {
		t.Errorf("expected trace key exactly once, got %d:\n%s", n, out)
	}
	if n := strings.Count(out, `"request_id":`); n != 1 {
		t.Errorf("expected request_id exactly once, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, `"tenant_id":"t-1"`) {
		t.Errorf("expected tenant_id label preserved:\n%s", out)
	}
}

// TestLogHttpInfo_RedactsSensitiveHeaders asegura que el log grueso oculta el
// valor de headers sensibles (Authorization, X-Api-Key, …) conservando la key.
func TestLogHttpInfo_RedactsSensitiveHeaders(t *testing.T) {
	var sink bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&sink, nil)))
	defer slog.SetDefault(prev)

	isRunningInCloudRun = false

	info := RequestInfo{
		Method:     "POST",
		Path:       "/api/v1/curp/validate",
		StatusCode: 200,
		Headers: map[string][]string{
			"Authorization": {"Bearer supersecret"},
			"X-Api-Key":     {"KEY-1234-PLAIN"},
			"Content-Type":  {"application/json"},
		},
	}
	LogHttpInfo(context.Background(), info, false)
	out := sink.String()

	if strings.Contains(out, "supersecret") || strings.Contains(out, "KEY-1234-PLAIN") {
		t.Errorf("se filtró el valor de un header sensible:\n%s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("se esperaba [REDACTED] para headers sensibles:\n%s", out)
	}
	if !strings.Contains(out, "application/json") {
		t.Errorf("un header no sensible (Content-Type) debe conservarse:\n%s", out)
	}
}

// fmtErr returns a simple error value for tests.
func fmtErr(s string) error { return &simpleErr{s} }

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }
