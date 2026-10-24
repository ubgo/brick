package brick

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// answerScripting is a module whose loader answers what its page asks for
// in props["do"]: a redirect, an error status, not found, or headers then
// data. gate(input) is the island guard: signed out (no ctx.user) → 303 to
// /login/, a non-admin → 403, an admin → data.
type answerScripting struct{}

func (answerScripting) Compile(context.Context, ScriptSource) (ScriptModule, error) {
	return answerModule{}, nil
}

type answerModule struct{}

func (answerModule) Functions() []string { return []string{"gate", "one"} }

func (answerModule) Call(ctx context.Context, fn string, args ...any) (any, error) {
	in, _ := args[0].(map[string]any)
	if fn == "gate" {
		c, _ := in["ctx"].(map[string]any)
		switch c["user"] {
		case nil, "":
			return nil, &Redirect{URL: "/login/", Status: http.StatusSeeOther}
		case "admin":
			if err := SetHeader(ctx, "Vary", "Cookie"); err != nil {
				return nil, err
			}
			return map[string]any{"secret": "42"}, nil
		}
		return nil, &HTTPError{Status: http.StatusForbidden, Message: "admins only"}
	}
	pg, _ := in["page"].(map[string]any)
	switch pg["do"] {
	case "redirect":
		return nil, &Redirect{URL: "/new/", Status: http.StatusPermanentRedirect}
	case "error":
		return nil, &HTTPError{Status: http.StatusGone}
	case "header":
		if err := SetHeader(ctx, "cache-control", "public, max-age=60"); err != nil {
			return nil, err
		}
	}
	return map[string]any{"title": "T"}, nil
}

func answerEngine(t *testing.T, mode Mode, reported *[]error) *Engine {
	t.Helper()
	return newEngine(t, Options{
		Mode: mode,
		FS: fstest.MapFS{
			"s/a.star":              {Data: []byte("#")},
			"content/redirect.html": {Data: []byte("---\ndo: redirect\nuse: {a: ~/s/a.star}\nload: a.one\n---\npage")},
			"content/error.html":    {Data: []byte("---\ndo: error\nuse: {a: ~/s/a.star}\nload: a.one\n---\npage")},
			"content/header.html":   {Data: []byte("---\ndo: header\nuse: {a: ~/s/a.star}\nload: a.one\n---\npage")},
			"content/bounded.html":  {Data: []byte("---\ndo: redirect\n---\n<brick:boundary><Moved /><brick:prop name=\"fallback\">F</brick:prop></brick:boundary>")},
			"components/Moved.html": {Data: []byte("---\ndo: redirect\nuse: {a: ~/s/a.star}\nload: a.one\n---\nmoved")},
			"content/admin.html":    {Data: []byte("<h1>Admin</h1><brick:boundary><Panel /><brick:prop name=\"fallback\">F</brick:prop></brick:boundary>")},
			"components/Panel.html": {Data: []byte("---\nbrick: {island: true}\ninject: {user: {default: \"\"}}\nuse: {a: ~/s/a.star}\nload: a.gate\n---\nsecret {{ .Props.secret }}")},
		},
		Scripting: answerScripting{},
		Hooks:     []any{errorRecorder{errs: reported}},
	})
}

// A page loader's redirect and error status fail the render with the
// typed answer, in Dev too, and are never reported to OnError hooks.
func TestLoaderAnswersFailTheRender(t *testing.T) {
	t.Parallel()
	for _, mode := range []Mode{Prod, Dev} {
		var reported []error
		e := answerEngine(t, mode, &reported)
		ctx := context.Background()
		_, err := e.RenderChunks(ctx, "content/redirect.html", nil)
		var rd *Redirect
		if !errors.As(err, &rd) || rd.URL != "/new/" || rd.Status != http.StatusPermanentRedirect {
			t.Fatalf("mode %v redirect: %v", mode, err)
		}
		_, err = e.RenderChunks(ctx, "content/error.html", nil)
		var he *HTTPError
		if !errors.As(err, &he) || he.Status != http.StatusGone {
			t.Fatalf("mode %v error: %v", mode, err)
		}
		if len(reported) != 0 {
			t.Fatalf("mode %v: answers reported as faults: %v", mode, reported)
		}
	}
}

// A boundary never contains an answer: a fallback in its place would send
// the page around it with status 200.
func TestBoundaryDoesNotContainAnswers(t *testing.T) {
	t.Parallel()
	var reported []error
	e := answerEngine(t, Prod, &reported)
	_, err := e.RenderChunks(context.Background(), "content/bounded.html", nil)
	var rd *Redirect
	if !errors.As(err, &rd) {
		t.Fatalf("a boundary contained the redirect: %v", err)
	}
	// brick.not_found() was contained before v15; it now reaches the host.
	nf := newEngine(t, Options{
		FS: fstest.MapFS{
			"s/posts.star":        {Data: []byte("#")},
			"components/P.html":   {Data: []byte("---\nuse: {posts: ~/s/posts.star}\nload: posts.one\n---\np")},
			"content/[slug].html": {Data: []byte("<brick:boundary><P /><brick:prop name=\"fallback\">F</brick:prop></brick:boundary>")},
		},
		Scripting: routeScripting{},
	})
	if _, err := nf.RenderChunks(context.Background(), "content/[slug].html", nil, WithRoute(map[string]string{"slug": "gone"}, nil)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a boundary contained brick.not_found(): %v", err)
	}
}

// An island's loader answers per request through Fill: signed out is a
// redirect, a non-admin a 403, even inside a boundary with a fallback and
// in Dev; an admin gets the island and its loader's header lands in the
// response headers given on ctx, not in the cached Result.
func TestIslandAnswersThroughFill(t *testing.T) {
	t.Parallel()
	for _, mode := range []Mode{Prod, Dev} {
		var reported []error
		e := answerEngine(t, mode, &reported)
		ctx := context.Background()
		res, err := e.RenderChunks(ctx, "content/admin.html", nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.Fill(ctx, res)
		var rd *Redirect
		if !errors.As(err, &rd) || rd.URL != "/login/" {
			t.Fatalf("mode %v signed out: %v", mode, err)
		}
		_, err = e.Fill(ctx, res, Provide("user", "bob"))
		var he *HTTPError
		if !errors.As(err, &he) || he.Status != http.StatusForbidden || he.Message != "admins only" {
			t.Fatalf("mode %v non-admin: %v", mode, err)
		}
		h := http.Header{}
		out, err := e.Fill(WithResponseHeaders(ctx, h), res, Provide("user", "admin"))
		if err != nil || !strings.Contains(string(out), "secret 42") {
			t.Fatalf("mode %v admin: %q %v", mode, out, err)
		}
		if h.Get("Vary") != "Cookie" || res.Headers != nil {
			t.Fatalf("mode %v: island header %v, page headers %v", mode, h, res.Headers)
		}
		if len(reported) != 0 {
			t.Fatalf("mode %v: answers reported as faults: %v", mode, reported)
		}
	}
}

// A page loader's header lands in Result.Headers, canonicalised.
func TestPageLoaderHeader(t *testing.T) {
	t.Parallel()
	var reported []error
	e := answerEngine(t, Prod, &reported)
	res, err := e.RenderChunks(context.Background(), "content/header.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Headers.Get("Cache-Control"); got != "public, max-age=60" {
		t.Fatalf("Cache-Control %q in %v", got, res.Headers)
	}
}

// NewRedirect accepts only 302, 303, 307 and 308 and a path or an http(s)
// URL; every open-redirect trick fails where the loader wrote it.
func TestNewRedirectRefusals(t *testing.T) {
	t.Parallel()
	if rd, err := NewRedirect("/login", 0); err != nil || rd.Status != http.StatusSeeOther {
		t.Fatalf("default: %v %v", rd, err)
	}
	for _, s := range []int{http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		if _, err := NewRedirect("/x", s); err != nil {
			t.Fatalf("%d refused: %v", s, err)
		}
	}
	for _, s := range []int{200, 301, 304, 300, 404} {
		if _, err := NewRedirect("/x", s); err == nil {
			t.Fatalf("status %d accepted", s)
		}
	}
	for _, u := range []string{"", "//evil.example/", "/\\evil.example", "javascript:alert(1)", "data:text/html,x", "evil.example", "https://user@evil.example/", "/a\r\nSet-Cookie: x=1", "ftp://evil.example/"} {
		if _, err := NewRedirect(u, 0); err == nil {
			t.Fatalf("%q accepted", u)
		}
	}
	if _, err := NewRedirect("https://accounts.example/oauth", 0); err != nil {
		t.Fatalf("an absolute URL's shape is fine at call time: %v", err)
	}
}

// RedirectLocation, the response-time lock: a path passes; an absolute URL
// only on the request's own host or Options.RedirectHosts.
func TestRedirectLocation(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: fstest.MapFS{}})
	open := newEngine(t, Options{FS: fstest.MapFS{}, RedirectHosts: []string{"Accounts.Example"}})
	r := httptest.NewRequest(http.MethodGet, "http://site.example/admin/", nil)
	cases := []struct {
		eng  *Engine
		r    *http.Request
		url  string
		pass bool
	}{
		{e, r, "/login/", true},
		{e, r, "https://site.example/login/", true},
		{e, r, "https://SITE.example/login/", true},
		{e, r, "https://accounts.example/oauth", false},
		{e, nil, "https://site.example/login/", false},
		{e, nil, "/login/", true},
		{open, r, "https://accounts.example/oauth", true},
		{open, nil, "https://accounts.example/oauth", true},
		{open, r, "https://other.example/", false},
	}
	for _, c := range cases {
		_, err := c.eng.RedirectLocation(c.r, &Redirect{URL: c.url, Status: http.StatusSeeOther})
		if (err == nil) != c.pass {
			t.Fatalf("%q (request %v): err %v, want pass=%v", c.url, c.r != nil, err, c.pass)
		}
	}
}

// NewHTTPError takes 4xx and 5xx only; 404 is the same answer as
// ErrNotFound.
func TestNewHTTPError(t *testing.T) {
	t.Parallel()
	for _, s := range []int{400, 403, 410, 451, 500, 503, 599} {
		if _, err := NewHTTPError(s, ""); err != nil {
			t.Fatalf("%d refused: %v", s, err)
		}
	}
	for _, s := range []int{0, 200, 302, 399, 600} {
		if _, err := NewHTTPError(s, ""); err == nil {
			t.Fatalf("%d accepted", s)
		}
	}
	he, _ := NewHTTPError(404, "")
	if !errors.Is(he, ErrNotFound) || !IsAnswer(he) {
		t.Fatal("brick.error(404) is not not_found")
	}
	if IsAnswer(errors.New("x")) || IsAnswer(nil) {
		t.Fatal("a fault is an answer")
	}
}

// SetHeader takes only LoaderHeaders, single-line values; Set-Cookie,
// hop-by-hop, engine-owned and Location are refused even with no sink.
func TestSetHeaderAllowlist(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	ctx := WithResponseHeaders(context.Background(), h)
	for _, name := range LoaderHeaders() {
		if err := SetHeader(ctx, strings.ToLower(name), "v"); err != nil {
			t.Fatalf("%s refused: %v", name, err)
		}
	}
	if len(h) != len(LoaderHeaders()) {
		t.Fatalf("collected %v", h)
	}
	for _, name := range []string{"Set-Cookie", "set-cookie", "Connection", "Keep-Alive", "Transfer-Encoding", "Upgrade", "TE", "Trailer", "Proxy-Authenticate", "Content-Type", "Content-Length", "Location", "Access-Control-Allow-Origin", "X-Anything"} {
		for _, c := range []context.Context{ctx, context.Background()} {
			if err := SetHeader(c, name, "v"); err == nil {
				t.Fatalf("%s accepted", name)
			}
		}
	}
	for _, v := range []string{"a\r\nSet-Cookie: x=1", "a\nb", "a\x00", "a\x7f", strings.Repeat("x", maxHeaderValue+1)} {
		if err := SetHeader(ctx, "Cache-Control", v); err == nil {
			t.Fatalf("value %q accepted", v)
		}
	}
	if err := SetHeader(context.Background(), "Vary", "Cookie"); err != nil {
		t.Fatalf("no sink should drop, not fail: %v", err)
	}
	LoaderHeaders()[0] = "Set-Cookie"
	if err := SetHeader(ctx, "Set-Cookie", "v"); err == nil {
		t.Fatal("LoaderHeaders returned the live list")
	}
}

// An action whose component's loader turns the visitor away while it
// re-renders (signed out since the page loaded) answers with that
// redirect as its one effect and no HTML; an error status keeps its
// status on the action response.
func TestActionReRenderAnswers(t *testing.T) {
	t.Parallel()
	sc := ctxScripting{"gate": {
		"guard": func(_ context.Context, a ...any) (any, error) {
			in, _ := a[0].(map[string]any)
			c, _ := in["ctx"].(map[string]any)
			switch c["user"] {
			case "", nil:
				return nil, &Redirect{URL: "/login/", Status: http.StatusSeeOther}
			case "ada":
				return map[string]any{"who": "ada"}, nil
			}
			return nil, &HTTPError{Status: http.StatusForbidden, Message: "no"}
		},
		"noop": func(_ context.Context, a ...any) (any, error) { st, _ := a[0].(map[string]any); return st, nil },
	}}
	e := newEngine(t, Options{
		FS:        theme("components/Box.html", "---\nstate: {n: 0}\ninject: {user: {default: \"\"}}\nload: guard\n---\n<brick:script>gate</brick:script><div>{{ .Props.who }}</div>", "content/a.html", "<Box />"),
		Scripting: sc,
		States:    NewMemoryStates(0, 0),
	})
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := e.Fill(ctx, r, Provide("user", "ada"))
	if err != nil || !strings.Contains(string(page), "ada") {
		t.Fatalf("signed in: %s %v", page, err)
	}
	h, tok := handleOf(t, string(page))
	res, err := e.RunAction(ctx, ActionRequest{Handle: h, Token: tok, Action: "noop"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.HTML) != 0 || len(res.Effects) != 1 || res.Effects[0].Kind != EffectRedirect || res.Effects[0].URL != "/login/" {
		t.Fatalf("signed-out re-render: %+v", res)
	}
	_, err = e.RunAction(ctx, ActionRequest{Handle: h, Token: tok, Action: "noop"}, Provide("user", "bob"))
	if ActionStatus(err) != http.StatusForbidden {
		t.Fatalf("non-admin re-render: status %d, %v", ActionStatus(err), err)
	}
}
