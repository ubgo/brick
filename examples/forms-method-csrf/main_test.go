package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// moduleRE finds the PostRow client module on the page.
var moduleRE = regexp.MustCompile(`src="(/_brick/c/PostRow\.[0-9a-f]+\.js)"`)

// deleteForm posts what a method link's hidden form posts: urlencoded,
// same-origin, _method=delete, plus any extra fields the hook added.
func deleteForm(h http.Handler, extra url.Values) *httptest.ResponseRecorder {
	v := url.Values{"_method": {"delete"}}
	for k, vs := range extra {
		v[k] = vs
	}
	r := httptest.NewRequest(http.MethodPost, "/posts/2", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// The route refuses a method link's form until the hook has added the
// token; the page carries the token and the module that adds it. That the
// hook runs on a click, and the form is then submitted, only a browser
// shows: the test posts the form the hook would build.
func TestMethodFormCarriesCSRFToken(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()
	if want := `<meta name="csrf-token" content="` + a.token + `">`; !strings.Contains(page, want) {
		t.Fatalf("page lacks %s:\n%s", want, page)
	}
	if !strings.Contains(page, `<a href="/posts/2" brick:method="delete" brick:confirm="Delete Second post?">Delete</a>`) {
		t.Fatalf("method link missing:\n%s", page)
	}
	m := moduleRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no PostRow module on the page:\n%s", page)
	}
	js := httptest.NewRecorder()
	h.ServeHTTP(js, httptest.NewRequest(http.MethodGet, m[1], nil))
	if !strings.Contains(js.Body.String(), "brick.methodFormWith(") {
		t.Fatalf("module lacks brick.methodFormWith:\n%s", js.Body)
	}

	if rec := deleteForm(h, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("without the token: %d %s", rec.Code, rec.Body)
	}
	if rec := deleteForm(h, url.Values{csrfField: {"wrong"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("with a wrong token: %d %s", rec.Code, rec.Body)
	}
	rec = deleteForm(h, url.Values{csrfField: {a.token}})
	if rec.Code != http.StatusOK || rec.Body.String() != "deleted post 2" {
		t.Fatalf("with the token: %d %s", rec.Code, rec.Body)
	}
}
