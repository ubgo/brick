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

// The exact responses the feeds and sitemap guide shows.
const (
	feed = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0">
<channel>
<title>Demo Blog</title>
<link>https://example.com/</link>
<item><title>Routing by file name</title><link>https://example.com/blog/routing/</link><description>Where URLs come from</description></item>
<item><title>Hello &amp; welcome</title><link>https://example.com/blog/hello/</link><description>The first post</description></item>
</channel>
</rss>
`
	robots = `User-agent: *
Disallow: /private/
Sitemap: https://example.com/sitemap.xml
`
	webFeed = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0">
<channel>
<title>Demo Blog: web</title>
<item><title>Routing by file name</title><link>https://example.com/blog/routing/</link></item>
</channel>
</rss>
`
	sitemap = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">
  <url>
    <loc>https://example.com/</loc>
  </url>
  <url>
    <loc>https://example.com/blog/hello/</loc>
    <lastmod>2026-10-01</lastmod>
  </url>
  <url>
    <loc>https://example.com/blog/routing/</loc>
    <lastmod>2026-10-09</lastmod>
  </url>
</urlset>
`
)

func TestOutputPages(t *testing.T) {
	s, err := newSite(context.Background(), themeDir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ url, ctype, body string }{
		{"/feed.xml", "application/xml; charset=utf-8", feed},
		{"/robots.txt", "text/plain; charset=utf-8", robots},
		{"/tags/web/feed.xml", "application/xml; charset=utf-8", webFeed},
		{"/sitemap.xml", "application/xml; charset=utf-8", sitemap},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.url, nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != c.ctype || rec.Body.String() != c.body {
			t.Errorf("%s: %d %s\n%s", c.url, rec.Code, rec.Header().Get("Content-Type"), rec.Body)
		}
	}
	// A tag not on the paths: list, and a file URL with a slash, are 404s.
	for _, u := range []string{"/tags/rust/feed.xml", "/tags/go/feed.xml/", "/feed.xml/"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", u, rec.Code)
		}
	}
}

// Export writes output pages at their file names, one feed per tag, and
// sitemap.xml; private.html is exported but left out of the sitemap.
func TestExportFeeds(t *testing.T) {
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
	want := []string{"blog/hello/index.html", "blog/routing/index.html", "feed.xml", "index.html", "private/index.html", "robots.txt", "sitemap.xml", "tags/go/feed.xml", "tags/web/feed.xml"}
	if !slices.Equal(files, want) || !rep.Sitemap {
		t.Fatalf("report %+v files:\n%s", rep, strings.Join(files, "\n"))
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "sitemap.xml")); string(b) != sitemap {
		t.Fatalf("sitemap.xml:\n%s", b)
	}
}
