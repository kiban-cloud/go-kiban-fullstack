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
			name:  "map (request_body for JSON requests) still prints as map",
			attr:  slog.Any("foo", map[string]any{"k": "v"}),
			want:  "foo:",
			notIn: "[107 ", // byte-slice form
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
