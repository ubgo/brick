package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docsHome is the exact page the hosting guide shows for /docs/.
const docsHome = `<!doctype html>
<html>
<head><meta name="brick-base" content="/docs"><title>Docs</title><link rel="stylesheet" href="/docs/style.css"></head>
<body>
<nav><a href="/docs/">Docs home</a> <a href="/docs/install/">Install</a> <a href="/shop/">Shop</a></nav>
<main><h1>Docs</h1>
<p>Start with <a href="/docs/install/">installing</a>. Links are written as if the site were at the root: <code>/install/</code>.</p>
</main>
</body>
</html>
`

func serve(h http.Handler, url string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func TestMount(t *testing.T) {
	h, err := handler(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(h, "/docs/"); rec.Code != http.StatusOK || rec.Body.String() != docsHome {
		t.Fatalf("/docs/: %d\n%s", rec.Code, rec.Body)
	}
	if rec := serve(h, "/docs"); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/docs/" {
		t.Fatalf("/docs: %d %v", rec.Code, rec.Header())
	}
	if rec := serve(h, "/docs/install/"); !strings.Contains(rec.Body.String(), `<img src="/docs/logo.svg" alt="logo">`) {
		t.Fatalf("/docs/install/:\n%s", rec.Body)
	}
	if rec := serve(h, "/docs/logo.svg"); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("static under the base: %d %v", rec.Code, rec.Header())
	}
	if rec := serve(h, "/shop/"); !strings.Contains(rec.Body.String(), `<nav><a href="/shop/">Shop home</a> <a href="/docs/">Docs</a></nav>`) {
		t.Fatalf("/shop/:\n%s", rec.Body)
	}
	for _, u := range []string{"/", "/install/"} {
		if rec := serve(h, u); rec.Code != http.StatusNotFound {
			t.Fatalf("%s outside every base: %d", u, rec.Code)
		}
	}
}

// Without KeepPaths, a link to the sibling site gets this site's base too.
func TestKeepPathsMatters(t *testing.T) {
	docs, err := newSite(context.Background(), docsDir, docsBase)
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(docs.Handler(), "/docs/"); !strings.Contains(rec.Body.String(), `<a href="/docs/shop/">Shop</a>`) {
		t.Fatalf("without KeepPaths:\n%s", rec.Body)
	}
}

// Export with a base writes the same rewritten HTML, at paths without the
// base: the folder is what the host serves at /docs/.
func TestExportUnderBase(t *testing.T) {
	ctx := context.Background()
	docs, _, err := sites(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := docs.Export(ctx, dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil || string(b) != docsHome {
		t.Fatalf("index.html:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "logo.svg")); err != nil {
		t.Fatal(err)
	}
}
