package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The router sees the verb a method link's form asks for: a same-origin
// POST with _method=delete removes the post, and the page links the
// runtime that makes the click send that form.
func TestMethodLinksApp(t *testing.T) {
	a, err := newApp("theme", false)
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	get := func() string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /: %d %s", rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	page := get()
	if !strings.Contains(page, `brick:method="delete"`) || !strings.Contains(page, "/runtime.") {
		t.Fatalf("page:\n%s", page)
	}
	r := httptest.NewRequest(http.MethodPost, "/posts/2", strings.NewReader("_method=delete"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	page = get()
	if strings.Contains(page, `id="post-2"`) || !strings.Contains(page, "DELETE /posts/2") {
		t.Fatalf("after delete:\n%s", page)
	}
}
