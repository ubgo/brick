package brick

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// TestMethodLinkMatchesRuntime keeps the runtime's verbs and field name
// equal to the ones the compiler checks and MethodOverride reads.
func TestMethodLinkMatchesRuntime(t *testing.T) {
	src, err := os.ReadFile("runtime/runtime.js")
	if err != nil {
		t.Fatal(err)
	}
	if want := MethodJS(); !strings.Contains(string(src), want) {
		t.Fatalf("runtime/runtime.js does not carry the method-link table; paste this line into it:\n%s", want)
	}
}

// brick:method is checked at compile time in any file: on an <a> with an
// href only, a verb from methodVerbs (any case) unless filled at render
// time, no modifiers; a brick:confirm or brick:disable-with beside it is
// allowed without state but takes no modifier. Start tags split by {{ }}
// are checked too, and a component call's verb is checked at the call.
func TestMethodLinkGrammar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		markup string
		err    string // "" = compiles
	}{
		{`<a href="/p/1" brick:method="delete">x</a>`, ""},
		{`<a href="/p/1" brick:method="post">x</a>`, ""},
		{`<a href="/p/1" brick:method="PUT">x</a>`, ""},
		{`<a href="/p/1" brick:method="Patch">x</a>`, ""},
		{`<A HREF="/p/1" brick:method="delete">x</A>`, ""},
		{`<a href="/p/{{ .Params.id }}" brick:method="delete">x</a>`, ""},
		{`<a brick:method="delete" href="/p/{{ .Params.id }}">x</a>`, ""},
		{`<a href="/p/1" brick:method="{{ .Params.verb }}">x</a>`, ""},
		{`<a href="/p/1" brick:method="delete" brick:confirm="Sure?" brick:disable-with="Deleting…">x</a>`, ""},
		{`<a href="/p/1" brick:method="delete" brick:disable-with>x</a>`, ""},
		{`<a href="/p/1" brick:method="get">x</a>`, `the verb is one of post, put, patch, delete`},
		{`<a href="/p/1" brick:method="remove">x</a>`, `the verb is one of`},
		{`<a href="/p/1" brick:method>x</a>`, `the verb is one of`},
		{`<a href="/p/{{ .Params.id }}" brick:method="nuke">x</a>`, `brick:method="nuke"`},
		{`<a href="/p/1" brick:method.now="delete">x</a>`, `takes no modifiers`},
		{`<button brick:method="delete">x</button>`, `goes on an <a href> link, not <button>`},
		{`<form brick:method="delete"></form>`, `not <form>`},
		{`<a brick:method="delete">x</a>`, `needs an href`},
		{`<a data-href="/x" brick:method="delete">x</a>`, `needs an href`},
		{`<a href="/p/1" brick:method="delete" brick:confirm.prompt="Why?">x</a>`, `brick:confirm.prompt: on a method link`},
		{`<a href="/p/1" brick:method="delete" brick:disable-with.x="…">x</a>`, `brick:disable-with.x: on a method link`},
		// Without brick:method the companions stay action attributes.
		{`<a href="/p/1" brick:confirm="Sure?">x</a>`, `declares state`},
		{`<LinkBtn href="/p/1" brick:method="delete" brick:confirm="Sure?">x</LinkBtn>`, ""},
		{`<LinkBtn href="/p/1" brick:method="{{ .Params.verb }}">x</LinkBtn>`, ""},
		{`<LinkBtn href="/p/1" brick:method="get">x</LinkBtn>`, `the verb is one of`},
		{`<LinkBtn href="/p/1" brick:method="delete" brick:confirm.prompt="Why?">x</LinkBtn>`, `on a method link`},
		{`<LinkBtn href="/p/1" brick:confirm="Sure?">x</LinkBtn>`, `reserved`},
	}
	for _, tc := range cases {
		e := newEngine(t, Options{FS: theme("content/a.html", tc.markup, "components/LinkBtn.html", `<a {{ attrs }}>{{ children }}</a>`)})
		res, err := e.Render(context.Background(), "content/a.html", map[string]any{"id": 1})
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%s: %v", tc.markup, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%s: got %v, want an error containing %q", tc.markup, err, tc.err)
		case tc.err == "" && !strings.Contains(string(res), "brick:method="):
			t.Errorf("%s: brick:method did not reach the page: %s", tc.markup, res)
		}
	}
}

// A method link's click is the runtime's, so a page with one links the
// runtime even with no client code and navigation off; a page without one
// does not. The link may come from the page, its layout or a component.
func TestMethodLinkLoadsRuntime(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, page string
		want       bool
	}{
		{"plain page", `<a href="/x">x</a>{{ assets }}`, false},
		{"page link", `<a href="/x" brick:method="delete">x</a>{{ assets }}`, true},
		{"component call", `<LinkBtn href="/x" brick:method="delete">x</LinkBtn>{{ assets }}`, true},
		{"inside a component", `<Del />{{ assets }}`, true},
		{"layout", "---\nlayout: ~/layouts/m.html\n---\nbody", true},
	}
	for _, tc := range cases {
		e := newEngine(t, Options{FS: theme(
			"content/a.html", tc.page,
			"components/LinkBtn.html", `<a {{ attrs }}>{{ children }}</a>`,
			"components/Del.html", `<a href="/x" brick:method="delete">x</a>`,
			"layouts/m.html", `{{ children }}<a href="/logout" brick:method="post">out</a>{{ assets }}`,
		)})
		res, err := e.Render(context.Background(), "content/a.html", nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := strings.Contains(string(res), "/runtime."); got != tc.want {
			t.Errorf("%s: runtime linked = %v, want %v:\n%s", tc.name, got, tc.want, res)
		}
	}
}

// MethodOverride turns only a same-origin, form-encoded POST whose
// _method names PUT, PATCH or DELETE into that verb; every other request
// reaches the handler as it came.
func TestMethodOverride(t *testing.T) {
	t.Parallel()
	const form = "application/x-www-form-urlencoded"
	cases := []struct {
		name, method, ctype, body, site, origin string
		want                                    string
	}{
		{"delete", "POST", form, "_method=delete", "same-origin", "", "DELETE"},
		{"put", "POST", form, "_method=put", "same-origin", "", "PUT"},
		{"patch", "POST", form, "_method=patch", "same-origin", "", "PATCH"},
		{"upper case", "POST", form, "_method=DELETE", "same-origin", "", "DELETE"},
		{"mixed case and spaces", "POST", form, "_method=+PaTcH+", "same-origin", "", "PATCH"},
		{"charset param", "POST", form + "; charset=utf-8", "_method=delete", "same-origin", "", "DELETE"},
		{"with other fields", "POST", form, "title=x&_method=put&csrf=t", "same-origin", "", "PUT"},
		{"origin header fallback", "POST", form, "_method=delete", "", "http://example.com", "DELETE"},
		{"post without _method", "POST", form, "title=x", "same-origin", "", "POST"},
		{"_method=post", "POST", form, "_method=post", "same-origin", "", "POST"},
		{"_method=get", "POST", form, "_method=get", "same-origin", "", "POST"},
		{"_method=head", "POST", form, "_method=head", "same-origin", "", "POST"},
		{"_method=connect", "POST", form, "_method=connect", "same-origin", "", "POST"},
		{"unknown verb", "POST", form, "_method=nuke", "same-origin", "", "POST"},
		{"empty verb", "POST", form, "_method=", "same-origin", "", "POST"},
		{"GET with _method in the query", "GET", "", "", "same-origin", "", "GET"},
		{"PUT stays PUT", "PUT", form, "_method=delete", "same-origin", "", "PUT"},
		{"cross-site", "POST", form, "_method=delete", "cross-site", "", "POST"},
		{"same-site subdomain", "POST", form, "_method=delete", "same-site", "", "POST"},
		{"foreign origin", "POST", form, "_method=delete", "", "http://evil.test", "POST"},
		{"no origin at all", "POST", form, "_method=delete", "", "", "POST"},
		{"multipart is never parsed", "POST", "multipart/form-data; boundary=x", "--x\r\nContent-Disposition: form-data; name=\"_method\"\r\n\r\ndelete\r\n--x--\r\n", "same-origin", "", "POST"},
		{"json", "POST", "application/json", `{"_method":"delete"}`, "same-origin", "", "POST"},
		{"query only", "POST", form, "", "same-origin", "", "POST"},
	}
	for _, tc := range cases {
		var got string
		h := MethodOverride(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Method }))
		target := "http://example.com/posts/7"
		if tc.name == "GET with _method in the query" || tc.name == "query only" {
			target += "?" + url.Values{MethodField: {"delete"}}.Encode()
		}
		r := httptest.NewRequest(tc.method, target, strings.NewReader(tc.body))
		if tc.ctype != "" {
			r.Header.Set("Content-Type", tc.ctype)
		}
		if tc.site != "" {
			r.Header.Set("Sec-Fetch-Site", tc.site)
		}
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		h.ServeHTTP(httptest.NewRecorder(), r)
		if got != tc.want {
			t.Errorf("%s: handler saw %s, want %s", tc.name, got, tc.want)
		}
	}
}

// The field stays readable to the handler after the override, and other
// fields arrive untouched.
func TestMethodOverrideKeepsForm(t *testing.T) {
	t.Parallel()
	var title, field string
	h := MethodOverride(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		title, field = r.PostFormValue("title"), r.PostFormValue(MethodField)
	}))
	r := httptest.NewRequest("POST", "/posts/7", strings.NewReader("title=Hi&_method=patch"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if title != "Hi" || field != "patch" {
		t.Fatalf("title=%q _method=%q", title, field)
	}
}
