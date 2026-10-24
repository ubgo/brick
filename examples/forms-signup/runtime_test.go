package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The page links the browser runtime with default Options: a stateful
// component is enough, so the deferred fields, brick:submit and
// brick:confirm work in a browser without any client module of its own.
func TestSignupLinksRuntime(t *testing.T) {
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler(eng).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), `<script type="module" src="/_brick/c/runtime.`) {
		t.Fatalf("the page does not link the runtime:\n%s", rec.Body)
	}
}
