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

// helloPage is the exact page the dynamic routes guide shows.
const helloPage = `<!doctype html>
<html>
<head><title>Hello world</title></head>
<body>
<main><h1>Hello world</h1>
<p>The first post.</p>
<p class="slug">slug: hello</p>
</main>
</body>
</html>
`

func serve(t *testing.T, h http.Handler, url string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec.Code, rec.Body.String()
}

func TestDynamicRoutes(t *testing.T) {
	s, err := newSite(context.Background(), themeDir)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	if code, body := serve(t, h, "/posts/hello/"); code != http.StatusOK || body != helloPage {
		t.Fatalf("/posts/hello/: %d\n%s", code, body)
	}
	cases := []struct {
		url  string
		code int
		want string
	}{
		{"/posts/tides/", 200, "<p class=\"slug\">slug: tides</p>"},
		{"/posts/nope/", 404, "<h1>Nothing here</h1>"},              // not on the paths: list
		{"/posts/archive/", 200, "<h1>Archive</h1>"},                // a static page beats the pattern
		{"/docs/guide/routing/", 200, "<p>path: guide/routing</p>"}, // a catch-all takes several segments
		{"/docs/guide/other/", 404, "<h1>Nothing here</h1>"},        // listed values only
		{"/docs/", 404, "<h1>Nothing here</h1>"},                    // [...path] needs at least one segment
	}
	for _, c := range cases {
		code, body := serve(t, h, c.url)
		if code != c.code || !strings.Contains(body, c.want) {
			t.Errorf("%s: %d, want %d with %q\n%s", c.url, code, c.code, c.want, body)
		}
	}
	var pats []string
	for _, r := range s.Patterns() {
		pats = append(pats, r.URL+" "+strings.Join(r.Params, ","))
	}
	if want := []string{"/posts/[slug]/ slug", "/docs/[...path]/ path"}; !slices.Equal(pats, want) {
		t.Fatalf("patterns %q", pats)
	}
}

// Export writes one file per paths: entry.
func TestExportDynamic(t *testing.T) {
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
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() != ".brick-export" {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	})
	want := []string{"404.html", "docs/guide/export/index.html", "docs/guide/routing/index.html", "docs/install/index.html", "index.html", "posts/archive/index.html", "posts/hello/index.html", "posts/tides/index.html"}
	if !slices.Equal(files, want) || rep.Pages != 7 {
		t.Fatalf("report %+v files:\n%s", rep, strings.Join(files, "\n"))
	}
	b, _ := os.ReadFile(filepath.Join(dir, "posts/hello/index.html"))
	if string(b) != helloPage {
		t.Fatalf("exported hello:\n%s", b)
	}
}
