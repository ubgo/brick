package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// gateScripting is the protected-site module: one(input) answers what the
// page's `do:` asks (a redirect, an error status, a header), and gate(input)
// is the island guard — signed out (no ctx.user) → 303 /login/, a
// non-admin → 403 "Admins only.", an admin → the secret.
type gateScripting struct{ target string }

func (g gateScripting) Compile(context.Context, brick.ScriptSource) (brick.ScriptModule, error) {
	return g, nil
}

func (gateScripting) Functions() []string { return []string{"gate", "one"} }

func (g gateScripting) Call(ctx context.Context, fn string, args ...any) (any, error) {
	in, _ := args[0].(map[string]any)
	if fn == "gate" {
		c, _ := in["ctx"].(map[string]any)
		switch c["user"] {
		case nil, "":
			return nil, &brick.Redirect{URL: "/login/", Status: http.StatusSeeOther}
		case "admin":
			return map[string]any{"secret": "42"}, brick.SetHeader(ctx, "Vary", "Cookie")
		}
		return nil, &brick.HTTPError{Status: http.StatusForbidden, Message: "Admins only."}
	}
	pg, _ := in["page"].(map[string]any)
	switch pg["do"] {
	case "moved":
		return nil, &brick.Redirect{URL: "/new/", Status: http.StatusPermanentRedirect}
	case "away":
		return nil, &brick.Redirect{URL: g.target, Status: http.StatusFound}
	case "gone":
		return nil, &brick.HTTPError{Status: http.StatusGone, Message: "Removed."}
	case "missing":
		return nil, brick.ErrNotFound
	case "cached":
		return map[string]any{}, brick.SetHeader(ctx, "Cache-Control", "public, max-age=300")
	}
	return map[string]any{}, nil
}

// gateSite is a site with a protected admin page (an island guard), a
// moved page, a removed page, a page that sets Cache-Control, a page that
// redirects to g.target, and an error page.
func gateSite(extra ...string) fstest.MapFS {
	front := func(do string) string {
		return "---\ndo: " + do + "\nuse: {g: ~/s/g.star}\nload: g.one\n---\n"
	}
	return project(append([]string{
		"s/g.star", "# fake",
		"layouts/default.html", "<html>{{ children }}</html>",
		"components/Panel.html", "---\nbrick: {island: true}\ninject: {user: {default: \"\"}}\nuse: {g: ~/s/g.star}\nload: g.gate\n---\nsecret {{ .Props.secret }}",
		"content/admin.html", "<h1>Admin</h1><Panel />",
		"content/login.html", "sign in",
		"content/old.html", front("moved") + "old",
		"content/gone.html", front("gone") + "gone",
		"content/cached.html", front("cached") + "cached",
		"content/away.html", front("away") + "away",
		"content/error.html", "<h1>Error {{ .Ctx.error.status }}</h1><p>{{ .Ctx.error.message }}</p>",
	}, extra...)...)
}

// cookieUser is the host's session: the "user" cookie, as a Provide.
func cookieUser(r *http.Request) []brick.RenderOption {
	if c, err := r.Cookie("user"); err == nil {
		return []brick.RenderOption{brick.Provide("user", c.Value)}
	}
	return nil
}

func getAs(h http.Handler, url, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if user != "" {
		req.AddCookie(&http.Cookie{Name: "user", Value: user})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The protected page: signed out → 303 /login/, a non-admin → 403 through
// content/error.html, an admin → the page; both answers carry no-store,
// and the admin's page never reaches the next signed-out visitor.
func TestProtectedPageOverHTTP(t *testing.T) {
	t.Parallel()
	s := newSite(t, gateSite(), Options{Engine: brick.Options{Scripting: gateScripting{}}, Request: cookieUser})
	h := s.Handler()
	steps := []struct {
		user, loc, body, cc string
		status              int
	}{
		{"", "/login/", "", noStore, http.StatusSeeOther},
		{"bob", "", "<h1>Error 403</h1><p>Admins only.</p>", noStore, http.StatusForbidden},
		{"admin", "", "secret 42", "", http.StatusOK},
		{"", "/login/", "", noStore, http.StatusSeeOther},
	}
	for i, st := range steps {
		rec := getAs(h, "/admin/", st.user)
		if rec.Code != st.status || rec.Header().Get("Location") != st.loc || !strings.Contains(rec.Body.String(), st.body) || rec.Header().Get("Cache-Control") != st.cc {
			t.Fatalf("step %d (%q): %d loc %q cc %q body %q", i, st.user, rec.Code, rec.Header().Get("Location"), rec.Header().Get("Cache-Control"), rec.Body)
		}
		if st.user != "admin" && strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("step %d leaked the protected page: %s", i, rec.Body)
		}
		if st.user == "admin" && rec.Header().Get("Vary") != "Cookie" {
			t.Fatalf("island header missing: %v", rec.Header())
		}
	}
}

// Page-level answers: a moved page's 308, a removed page's 410 through the
// error page, a loader header on a 200 — on every request, cached or not —
// and the error page is not a page of the site.
func TestPageAnswersOverHTTP(t *testing.T) {
	t.Parallel()
	s := newSite(t, gateSite(), Options{Engine: brick.Options{Scripting: gateScripting{}}})
	h := s.Handler()
	for range 2 {
		if rec := getAs(h, "/old/", ""); rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != "/new/" {
			t.Fatalf("moved: %d %v", rec.Code, rec.Header())
		}
		if rec := getAs(h, "/gone/", ""); rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "<h1>Error 410</h1><p>Removed.</p>") {
			t.Fatalf("gone: %d %s", rec.Code, rec.Body)
		}
		if rec := getAs(h, "/cached/", ""); rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "public, max-age=300" {
			t.Fatalf("cached: %d %v", rec.Code, rec.Header())
		}
	}
	if rec := getAs(h, "/error/", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("/error/ is a page: %d", rec.Code)
	}
	// Without an error page the status is plain text with the message.
	bare := gateSite()
	delete(bare, "content/error.html")
	s2 := newSite(t, bare, Options{Engine: brick.Options{Scripting: gateScripting{}}})
	if rec := getAs(s2.Handler(), "/gone/", ""); rec.Code != http.StatusGone || strings.TrimSpace(rec.Body.String()) != "Removed." {
		t.Fatalf("bare gone: %d %q", rec.Code, rec.Body)
	}
	// Check reports no diagnostic for a page that answers.
	diags, err := s.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		if d.Severity == brick.SeverityError {
			t.Fatalf("check flagged an answer: %v", d)
		}
	}
}

// The open-redirect lock at the response: an external target is a 500,
// never followed, unless brick.Options.RedirectHosts names it; a target on
// the request's own host passes.
func TestLoaderRedirectHosts(t *testing.T) {
	t.Parallel()
	ext := gateScripting{target: "https://evil.example/phish"}
	closed := newSite(t, gateSite(), Options{Engine: brick.Options{Scripting: ext}})
	if rec := getAs(closed.Handler(), "/away/", ""); rec.Code != http.StatusInternalServerError || rec.Header().Get("Location") != "" {
		t.Fatalf("external redirect followed: %d %v", rec.Code, rec.Header())
	}
	open := newSite(t, gateSite(), Options{Engine: brick.Options{Scripting: ext, RedirectHosts: []string{"evil.example"}}})
	if rec := getAs(open.Handler(), "/away/", ""); rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://evil.example/phish" {
		t.Fatalf("allowed host refused: %d %v", rec.Code, rec.Header())
	}
	same := newSite(t, gateSite(), Options{Engine: brick.Options{Scripting: gateScripting{target: "http://example.com/x/"}}})
	if rec := getAs(same.Handler(), "/away/", ""); rec.Code != http.StatusFound || rec.Header().Get("Location") != "http://example.com/x/" {
		t.Fatalf("same-origin refused: %d %v", rec.Code, rec.Header())
	}
}

// Under a BasePath a path target gets the base, like every site URL.
func TestLoaderRedirectBasePath(t *testing.T) {
	t.Parallel()
	s := newSite(t, gateSite(), Options{BasePath: "/docs", Engine: brick.Options{Scripting: gateScripting{}}, Request: cookieUser})
	for url, loc := range map[string]string{"/docs/admin/": "/docs/login/", "/docs/old/": "/docs/new/"} {
		if rec := getAs(s.Handler(), url, ""); rec.Header().Get("Location") != loc {
			t.Fatalf("%s: %d %v", url, rec.Code, rec.Header())
		}
	}
}

// Export: a redirect (the page's or an island's) is a meta-refresh page
// with nothing of the page in it; an error status stops the export.
func TestExportLoaderAnswers(t *testing.T) {
	t.Parallel()
	fsys := gateSite()
	delete(fsys, "content/gone.html")
	delete(fsys, "content/away.html")
	s := newSite(t, fsys, Options{Engine: brick.Options{Scripting: gateScripting{}}})
	dir := t.TempDir()
	if _, err := s.Export(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	for file, target := range map[string]string{"admin/index.html": "/login/", "old/index.html": "/new/"} {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		got := string(b)
		if !strings.Contains(got, `http-equiv="refresh" content="0; url=`+target+`"`) || strings.Contains(got, "Admin") || strings.Contains(got, "secret") {
			t.Fatalf("%s: %s", file, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "error", "index.html")); err == nil {
		t.Fatal("the error page was exported as a page")
	}
	withGone := gateSite()
	delete(withGone, "content/away.html")
	gone := newSite(t, withGone, Options{Engine: brick.Options{Scripting: gateScripting{}}})
	_, err := gone.Export(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "answered 410, which a static file cannot send") {
		t.Fatalf("export of a 410 page: %v", err)
	}
}
