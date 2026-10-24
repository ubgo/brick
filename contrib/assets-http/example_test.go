package assetshttp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing/fstest"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
)

// assetTheme has one component that ships browser code with
// <brick:client>, and a page that prints its {{ assets }} tags.
var assetTheme = fstest.MapFS{
	"components/Tabs.html": {Data: []byte("<brick:client>\ncustomElements.define('x-tabs', class extends HTMLElement {})\n</brick:client>\n<div>{{ children }}</div>")},
	"content/index.html":   {Data: []byte("<Tabs>one</Tabs>{{ assets }}")},
}

// moduleURL finds the content-hashed URL of the Tabs module in a rendered page.
var moduleURL = regexp.MustCompile(`src="([^"]+/Tabs\.[^"]+)"`)

// Mount the handler at the engine's asset base and the page's module URL
// is served with a year-long immutable cache header. The engine sits in an
// atomic.Pointer so a Reload can swap it without remounting the handler.
func Example() {
	var current atomic.Pointer[brick.Engine]
	eng, err := brick.New(brick.Options{FS: assetTheme})
	if err != nil {
		fmt.Println(err)
		return
	}
	current.Store(eng)

	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase,
		func() assetshttp.Source { return current.Load() }))

	page, err := eng.Render(context.Background(), "content/index.html", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	m := moduleURL.FindStringSubmatch(string(page))
	if m == nil {
		fmt.Println("no module tag in the page")
		return
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, m[1], nil))
	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Cache-Control"))
	fmt.Println(rec.Header().Get("X-Content-Type-Options"))

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, m[1], nil))
	fmt.Println(rec.Code)
	// Output:
	// 200
	// public, max-age=31536000, immutable
	// nosniff
	// 405
}
