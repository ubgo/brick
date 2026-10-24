// Command forms-uploads serves a profile form with an avatar and a small
// photo list. Each file streams to /_brick/upload as the $upload
// pseudo-action and lands in a disk sink under a temporary directory; an
// UploadCheck refuses PNGs wider than maxPNGWidth from their first bytes;
// and the same form posts multipart with JavaScript switched off. With
// -presign the engine uses an in-memory store the browser PUTs to
// directly instead (see presign.go).
//
//	go run ./forms-uploads -addr :8087      then open /profile
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/upload/disk"
)

// page is the file the /profile route renders.
const page = "content/profile.html"

// uploadsPrefix is where the disk sink's files are served, and the URL
// prefix it writes into each ref, so <img src="{{ .url }}"> works.
// ds:def id=uploadsprefix-h7xzu2h7
const uploadsPrefix = "/uploads/"

// maxPNGWidth is the widest PNG the avatar check lets through: a 72px
// avatar never needs a poster-sized original.
// ds:def id=maxpngwidth-pthmetbn
const maxPNGWidth = 4096

// pngHeader is the PNG signature followed by the IHDR chunk's length and
// type; the image width is the big-endian uint32 right after it.
// ds:def id=pngheader-9davw5f9
const pngHeader = "\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"

// checkPNGWidth is the host's own per-file check. It sees the sniffed type
// and the file's first brick.UploadCheckBytes bytes, after the uploads:
// limits and before the sink stores anything, so a refused file is never
// written. head is nil for a presigned upload (brick never sees those
// bytes), so the check lets those through.
// ds:def id=checkpngwidth-nffedgqa
func checkPNGWidth(_ context.Context, m brick.UploadMeta, head []byte) error {
	if m.Type != "image/png" || len(head) < len(pngHeader)+4 || string(head[:len(pngHeader)]) != pngHeader {
		return nil
	}
	if w := binary.BigEndian.Uint32(head[len(pngHeader):]); w > maxPNGWidth {
		return brick.RefuseUpload(fmt.Sprintf("The image is %d pixels wide; the most is %d.", w, maxPNGWidth))
	}
	return nil
}

// app is the engine and the sink it stores files in.
// ds:def id=app-7p5jxeq6
type app struct {
	eng  *brick.Engine
	sink brick.UploadSink
	// files serves the stored bytes back under uploadsPrefix.
	files http.Handler
}

// newApp builds the engine over the theme in dir. With presign false the
// files go to a disk sink in uploadDir; with presign true to an
// in-memory store the browser uploads to directly.
// ds:def id=newapp-3sd49eb4
func newApp(dir, uploadDir string, presign bool) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	a := &app{}
	if presign {
		ps := newPresignStore(uploadsPrefix)
		a.sink, a.files = ps, ps
	} else {
		d, err := disk.New(disk.Options{Dir: uploadDir, URLPrefix: uploadsPrefix})
		if err != nil {
			return nil, err
		}
		a.sink, a.files = d, d.Handler()
	}
	a.eng, err = brick.New(brick.Options{
		FS:          root.FS(),
		Scripting:   sc,
		States:      brick.NewMemoryStates(0, 0),
		Uploads:     a.sink,
		UploadCheck: checkPNGWidth,
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// handler mounts the action and upload endpoints, the runtime assets, the
// stored files and the page, which also answers its own no-JavaScript
// form posts.
// ds:def id=app-handler-34zqazvz
func (a *app) handler() http.Handler {
	eng := a.eng
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase+brick.UploadEndpoint, eng.UploadHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return eng }))
	mux.Handle(uploadsPrefix, http.StripPrefix(uploadsPrefix, a.files))
	mux.HandleFunc("GET /profile", func(w http.ResponseWriter, r *http.Request) {
		a.write(w, r, http.StatusOK, eng.FormResumeOption(w, r))
	})
	mux.HandleFunc("POST /profile", func(w http.ResponseWriter, r *http.Request) {
		if !eng.IsFormPost(r) {
			http.Error(w, "not a form post", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		out, err := eng.FormPost(r)
		if err != nil {
			eng.FormError(w, r, err)
			return
		}
		if !out.Failed && (out.Resume.Handle == "" || eng.SetFormResume(w, r, out.Resume)) {
			http.Redirect(w, r, out.Location, http.StatusSeeOther)
			return
		}
		status := http.StatusOK
		if out.Failed {
			status = http.StatusUnprocessableEntity
		}
		a.write(w, r, status, brick.WithFormResume(out.Resume))
	})
	return mux
}

// write renders the page with opts and answers with status.
func (a *app) write(w http.ResponseWriter, r *http.Request, status int, opts ...brick.RenderOption) {
	html, err := a.eng.Render(r.Context(), page, nil, opts...)
	if err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(html)
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8087"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "forms-uploads/theme", "the project directory")
	presign := flag.Bool("presign", false, "upload straight to an in-memory store instead of through the engine")
	flag.Parse()
	if err := run(*addr, *dir, *presign); err != nil {
		log.Fatal(err)
	}
}

// run serves until the listener fails. Files go to a fresh temporary
// directory, removed on the way out: uploads are temporary until an app
// keeps them, and this demo keeps nothing.
// ds:def id=run-grwa57pb
func run(addr, dir string, presign bool) error {
	tmp, err := os.MkdirTemp("", "brick-uploads-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	a, err := newApp(dir, tmp, presign)
	if err != nil {
		return err
	}
	log.Printf("serving on %s, files in %s", addr, tmp)
	return http.ListenAndServe(addr, a.handler())
}
