// Package assetshttp serves brick's component modules and stylesheets over
// net/http (A8).
//
//	var eng atomic.Pointer[brick.Engine]
//	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase,
//	    func() assetshttp.Source { return eng.Load() }))
//
// Asset URLs are content-hashed, so every response is marked immutable and
// cacheable for a year: a changed file gets a new URL, never new bytes at an
// old one. An engine registers a component's assets when it compiles the
// component, so run CompileAll at startup to serve every asset before the
// first page that uses it is rendered.
package assetshttp

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ubgo/brick"
)

// cacheControl is sent with every asset. Safe because URLs are hashed.
const cacheControl = "public, max-age=31536000, immutable"

// Source is what the handler serves from; *brick.Engine satisfies it.
type Source interface {
	Assets() fs.FS
}

// Handler serves assets for the engine mounted at prefix (normally
// Options.AssetBase). Pass a func so a host that swaps engines on Reload
// always serves the current snapshot.
func Handler(prefix string, current func() Source) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, prefix)
		if name == r.URL.Path || !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		f, err := current().Assets().Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		rs, ok := f.(io.ReadSeeker)
		if !ok {
			http.Error(w, "asset is not seekable", http.StatusInternalServerError)
			return
		}
		if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Cache-Control", cacheControl)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, path.Base(name), time.Time{}, rs)
	})
}

var _ Source = (*brick.Engine)(nil)
