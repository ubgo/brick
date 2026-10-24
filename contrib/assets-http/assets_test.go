package assetshttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

func TestHandler(t *testing.T) {
	t.Parallel()
	e, err := brick.New(brick.Options{FS: fstest.MapFS{
		"components/Tabs.html": {Data: []byte("<brick:client>\ncustomElements.define('x-tabs', class extends HTMLElement {})\n</brick:client>\n<div>{{ children }}</div>")},
		"content/a.html":       {Data: []byte("<Tabs>x</Tabs>{{ assets }}")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	url := regexp.MustCompile(`src="([^"]+/Tabs\.[^"]+)"`).FindStringSubmatch(string(out))
	if url == nil {
		t.Fatalf("no module in %s", out)
	}
	h := Handler(brick.DefaultAssetBase, func() Source { return e })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url[1], nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "customElements") || rec.Header().Get("Cache-Control") != cacheControl || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("serve: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	for _, p := range []string{brick.DefaultAssetBase + "c/Nope.js", "/elsewhere/x.js", brick.DefaultAssetBase + "../x"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, url[1], nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rec.Code)
	}
}
