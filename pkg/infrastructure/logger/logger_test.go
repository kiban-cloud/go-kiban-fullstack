package logger

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"
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
