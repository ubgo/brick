// Command tooling-bricktest is a small theme (a validated sign-up form,
// an avatar upload and a price with a loader) whose tests in main_test.go
// drive it with bricktest: clicks, submits, uploads and effects, all in
// process with no browser. The program itself only serves the page.
//
//	go run ./tooling-bricktest      then open /
//	go test ./tooling-bricktest     run the theme's tests
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/upload/disk"
)

// page is the one page this app serves.
const page = "content/index.html"

// uploadsURL is where stored files are served; the disk sink writes it
// into each file's URL, so the two must match.
const uploadsURL = "/uploads/"

// newEngine builds the engine the program serves and the tests drive.
// uploads is where files go: a disk sink here, bricktest.MemoryUploads in
// a test, so a test never writes to disk.
func newEngine(dir string, uploads brick.UploadSink) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		Uploads:   uploads,
	})
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8087"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "tooling-bricktest/theme", "the project directory")
	files := flag.String("uploads", os.TempDir(), "where uploaded files are written")
	flag.Parse()
	sink, err := disk.New(disk.Options{Dir: *files, URLPrefix: uploadsURL})
	if err != nil {
		log.Fatal(err)
	}
	eng, err := newEngine(*dir, sink)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return eng }))
	mux.Handle(brick.DefaultAssetBase+brick.UploadEndpoint, eng.UploadHandler())
	mux.Handle(uploadsURL, http.StripPrefix(uploadsURL, sink.Handler()))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		html, err := eng.Render(r.Context(), page, nil)
		if err != nil {
			log.Printf("render %s: %v", page, err)
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
