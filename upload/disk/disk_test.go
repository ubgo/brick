package disk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ubgo/brick"
)

func newSink(t *testing.T, now *time.Time) *Sink {
	t.Helper()
	s, err := New(Options{Dir: t.TempDir(), URLPrefix: "/uploads/", Clock: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func put(t *testing.T, s *Sink, name, typ string, data []byte) brick.UploadRef {
	t.Helper()
	w, err := s.Open(context.Background(), brick.UploadMeta{Name: name, Type: typ})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	ref, err := w.Close()
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestWriteServeRemove(t *testing.T) {
	now := time.Now()
	s := newSink(t, &now)
	ref := put(t, s, "../../etc/me.png", "image/png", []byte("PNGDATA"))
	if !idRE.MatchString(ref.ID) || ref.Name != "me.png" || ref.Size != 7 || ref.URL != "/uploads/"+ref.ID {
		t.Fatalf("ref = %+v", ref)
	}
	rec := httptest.NewRecorder()
	http.StripPrefix("/uploads/", s.Handler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ref.URL, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "PNGDATA" || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Content-Disposition") != "inline" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("serve: %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	for _, p := range []string{"/uploads/../disk.go", "/uploads/nothex"} {
		rec = httptest.NewRecorder()
		http.StripPrefix("/uploads/", s.Handler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", p, rec.Code)
		}
	}
	if err := s.Remove(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.OpenFile(ref.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after remove: %v", err)
	}
}

func TestNonImagesDownload(t *testing.T) {
	now := time.Now()
	s := newSink(t, &now)
	ref := put(t, s, "a.html", "text/html", []byte("<script>"))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+ref.ID, nil))
	if rec.Header().Get("Content-Disposition") != "attachment" {
		t.Fatalf("disposition %q", rec.Header().Get("Content-Disposition"))
	}
}

func TestAbortLeavesNothing(t *testing.T) {
	now := time.Now()
	s := newSink(t, &now)
	w, err := s.Open(context.Background(), brick.UploadMeta{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("partial"))
	if err := w.Abort(io.ErrUnexpectedEOF); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(s.opts.Dir); len(entries) != 0 {
		t.Fatalf("left %d files", len(entries))
	}
}

func TestSweepRemovesOld(t *testing.T) {
	now := time.Now()
	s := newSink(t, &now)
	old := put(t, s, "old.txt", "text/plain", []byte("o"))
	past := now.Add(-2 * DefaultTTL)
	for _, p := range []string{s.path(old.ID), s.path(old.ID) + metaExt} {
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}
	fresh := put(t, s, "new.txt", "text/plain", []byte("n"))
	// A file the sink did not write is never touched.
	other := filepath.Join(s.opts.Dir, "keep.me")
	if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(other, past, past)
	n, err := s.Sweep()
	if err != nil || n != 1 {
		t.Fatalf("swept %d, %v", n, err)
	}
	if _, _, err := s.OpenFile(old.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("old upload kept")
	}
	if f, _, err := s.OpenFile(fresh.ID); err != nil {
		t.Fatal("fresh upload swept")
	} else {
		f.Close()
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("sweep removed a file it did not write")
	}
}

// The sink works end to end behind an engine.
func TestBehindEngine(t *testing.T) {
	now := time.Now()
	s := newSink(t, &now)
	fsys := map[string]string{
		"components/A.html": "---\nstate:\n  avatar: null\nbrick:\n  uploads:\n    avatar: {accept: [image/png]}\n---\n<div><i>{{ with .State.avatar }}{{ .url }}{{ end }}</i></div>",
		"content/a.html":    "<A />",
	}
	eng := engineFor(t, fsys, s)
	page, err := eng.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleIn(t, string(page))
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 20)...)
	res, err := eng.RunUpload(context.Background(), brick.ActionRequest{Handle: h, Token: tok}, brick.UploadFile{Field: "avatar", Name: "a.png", Type: "image/png", Size: int64(len(png)), Body: bytes.NewReader(png)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(res.HTML, []byte("<i>/uploads/")) {
		t.Fatalf("html %s", res.HTML)
	}
}
