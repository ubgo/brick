package main

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The exact bodies the protected pages guide shows.
const (
	adminPage = `<!doctype html>
<html>
<head><title>Admin</title></head>
<body>
<main><h1>Admin</h1>
<section>Welcome, admin. Secret numbers: 42.</section>
</main>
</body>
</html>
`
	forbiddenPage = `<!doctype html>
<html>
<head><title>Error</title></head>
<body>
<main><h1>Error 403</h1>
<p>Admins only.</p>
</main>
</body>
</html>
`
	exportedAdmin = `<!doctype html><html><head><meta charset="utf-8"><title>Redirecting…</title><meta name="robots" content="noindex"><link rel="canonical" href="/login/"><meta http-equiv="refresh" content="0; url=/login/"></head><body><a href="/login/">/login/</a></body></html>
`
)

// as requests url as user ("" for signed out) through the real handler.
func as(t *testing.T, h http.Handler, url, user string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if user != "" {
		req.AddCookie(&http.Cookie{Name: userCookie, Value: user})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminIsland(t *testing.T) {
	s, err := newSite(context.Background(), themeDir)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	signedOut := func(when string) {
		t.Helper()
		rec := as(t, h, "/admin/", "")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login/" || rec.Header().Get("Cache-Control") != "private, no-store" || strings.Contains(rec.Body.String(), "Secret") {
			t.Fatalf("%s: %d %v %q", when, rec.Code, rec.Header(), rec.Body)
		}
	}
	signedOut("before")
	if rec := as(t, h, "/admin/", "bob"); rec.Code != http.StatusForbidden || rec.Body.String() != forbiddenPage || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("bob: %d %v\n%s", rec.Code, rec.Header(), rec.Body)
	}
	if rec := as(t, h, "/admin/", "admin"); rec.Code != http.StatusOK || rec.Body.String() != adminPage {
		t.Fatalf("admin: %d\n%s", rec.Code, rec.Body)
	}
	// The page shell is cached; the admin's island never is.
	signedOut("after the admin's visit")
}

func TestPerRequestPage(t *testing.T) {
	s, err := newSite(context.Background(), themeDir)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	for _, c := range []struct{ user, want string }{{"ada", "<p>Hello, ada.</p>"}, {"", "<p>Hello, guest.</p>"}, {"bob", "<p>Hello, bob.</p>"}} {
		rec := as(t, h, "/dashboard/", c.user)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), c.want) || rec.Header().Get("Cache-Control") != "private, no-cache" {
			t.Fatalf("%q: %d %v\n%s", c.user, rec.Code, rec.Header(), rec.Body)
		}
	}
	for _, r := range s.Routes() {
		if r.URL == "/dashboard/" && !r.PerRequest {
			t.Fatal("Route.PerRequest is false for the dashboard")
		}
	}
}

// Export has no visitor: the guarded page becomes a redirect page and the
// per-request page is skipped.
func TestExportProtected(t *testing.T) {
	ctx := context.Background()
	s, err := newSite(ctx, themeDir)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	rep, err := s.Export(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pages != 2 || !slices.Equal(rep.Skipped, []string{"/dashboard/"}) {
		t.Fatalf("report %+v", rep)
	}
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() != ".brick-export" {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	})
	if want := []string{"404.html", "admin/index.html", "login/index.html"}; !slices.Equal(files, want) {
		t.Fatalf("files %q", files)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "admin/index.html"))
	if string(b) != exportedAdmin {
		t.Fatalf("admin/index.html:\n%s", b)
	}
}
