package testutils

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"testing"
)

// PostForm sends a form-encoded POST to serverURL+path. The HX-Request header
// is set so HTMX-aware handlers treat it as a fragment request.
func PostForm(t *testing.T, client *http.Client, serverURL, path string, values url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, serverURL+path, bytes.NewBufferString(values.Encode()))
	if err != nil {
		t.Fatalf("PostForm: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PostForm: %v", err)
	}
	return resp
}

// Get sends a GET to serverURL+path.
func Get(t *testing.T, client *http.Client, serverURL, path string) *http.Response {
	t.Helper()
	resp, err := client.Get(serverURL + path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return resp
}

// PostJSON sends a JSON POST with the provided body and optional extra headers.
func PostJSON(t *testing.T, client *http.Client, serverURL, path string, body []byte, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, serverURL+path, bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	return resp
}

// Delete sends an HTTP DELETE. HX-Request header is set to match how HTMX
// issues delete requests from the browser.
func Delete(t *testing.T, client *http.Client, serverURL, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, serverURL+path, nil)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	return resp
}

// SetSessionCookie adds a session_id cookie to the client's jar for serverURL.
func SetSessionCookie(t *testing.T, client *http.Client, serverURL, sessionID string) {
	t.Helper()
	u, _ := url.Parse(serverURL)
	client.Jar.SetCookies(u, []*http.Cookie{
		{Name: "session_id", Value: sessionID, Path: "/"},
	})
}

// ReadBody reads and returns the response body as a string, closing it after.
func ReadBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadBody: %v", err)
	}
	return string(b)
}
