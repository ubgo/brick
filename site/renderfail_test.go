package site

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRenderFailureIsLogged pins that a page whose component fails to
// compile is logged with its file:line in both modes: Prod sends the
// visitor a bare 500, so the log is the only place the cause appears, and
// Dev shows it inline as well. Covers a plain component and an island.
func TestRenderFailureIsLogged(t *testing.T) {
	comps := map[string]string{
		"plain":  "{{ if }}",
		"island": "---\nstate:\n  n: 0\n---\n<div>{{ if }}</div>",
	}
	for name, comp := range comps {
		for _, dev := range []bool{false, true} {
			var logs bytes.Buffer
			s := newSite(t, project(
				"content/index.html", "<h1>x</h1><Bad id=\"c1\"></Bad>",
				"components/Bad.html", comp,
			), Options{Dev: dev, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("%s dev=%v: status %d", name, dev, rec.Code)
			}
			if !strings.Contains(logs.String(), "site: render failed") || !strings.Contains(logs.String(), "components/Bad.html:") {
				t.Fatalf("%s dev=%v: the failure was not logged with its file:line: %q", name, dev, logs.String())
			}
			body := rec.Body.String()
			if dev && !strings.Contains(body, "components/Bad.html:") {
				t.Fatalf("%s: Dev page does not show file:line: %q", name, body)
			}
			if !dev && strings.Contains(body, "Bad.html") {
				t.Fatalf("%s: Prod leaked the error to the visitor: %q", name, body)
			}
		}
	}
}

// TestNotFoundIsNotLogged pins that a plain 404 is not reported as a
// render failure, so the error log stays a list of real faults.
func TestNotFoundIsNotLogged(t *testing.T) {
	var logs bytes.Buffer
	s := newSite(t, blog(), Options{Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.Contains(logs.String(), "render failed") {
		t.Fatalf("a 404 was logged as a failure: %q", logs.String())
	}
}
