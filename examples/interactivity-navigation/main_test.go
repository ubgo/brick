package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fetch serves one GET, with the Brick-Nav header nav.js sends when it
// navigates (empty for a first visit).
func fetch(t *testing.T, h http.Handler, path, nav string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if nav != "" {
		req.Header.Set("Brick-Nav", nav)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body)
	}
	return rec
}

// A first visit gets the whole page with nav.js and the layout's
// fingerprint; a navigation that shows the same layout gets only the page.
func TestFragmentNavigation(t *testing.T) {
	s, err := newSite(context.Background(), "theme")
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	first := fetch(t, h, "/docs/intro/", "")
	page := first.Body.String()
	fp := first.Header().Get("Brick-Layout")
	if fp == "" || !strings.Contains(page, `<meta name="brick-layout" content="`+fp+`">`) || !strings.Contains(page, `/_brick/c/nav.`) {
		t.Fatalf("first visit (fingerprint %q):\n%s", fp, page)
	}
	if !strings.Contains(page, `<script type="speculationrules">`) {
		t.Fatalf("a page with brick:prefetch links carries speculation rules:\n%s", page)
	}

	next := fetch(t, h, "/docs/", fp)
	if next.Header().Get("Brick-Fragment") != "1" {
		t.Fatalf("same layout, full page:\n%s", next.Body)
	}
	body := next.Body.String()
	if !strings.HasPrefix(body, "<!doctype html><html><head><title>Docs</title></head><body><brick-assets>") || !strings.HasSuffix(body, "<h1>Docs</h1>\n</body></html>") {
		t.Fatalf("fragment:\n%s", body)
	}
	if strings.Contains(body, "<header>") || strings.Contains(body, `brick:persist`) {
		t.Fatalf("the layout travelled:\n%s", body)
	}

	stale := fetch(t, h, "/docs/", "0123456789abcdef0123456789abcdef")
	if stale.Header().Get("Brick-Fragment") != "" || !strings.Contains(stale.Body.String(), "<header>") {
		t.Fatalf("another layout's fingerprint must get the full page:\n%s", stale.Body)
	}
}
