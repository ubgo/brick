package site

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// The upload endpoint is served beside the action one, under the base
// path, and its answer is rebased like an action's.
func TestUploadUnderBasePath(t *testing.T) {
	t.Parallel()
	fsys := project("content/up.html", "<Avatar />")
	fsys["components/Avatar.html"] = &fstest.MapFile{Data: []byte("---\nstate:\n  avatar: null\nbrick:\n  uploads:\n    avatar: {accept: [image/png]}\n---\n<div><b>{{ with .State.avatar }}{{ .name }}{{ end }}</b><a href=\"/about/\">about</a></div>")}
	sink := bricktest.NewMemoryUploads()
	s := newSite(t, fsys, Options{BasePath: "/docs/", Engine: brick.Options{Uploads: sink}})
	h := s.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/up/", nil))
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("no state in %s", rec.Body)
	}
	png := []byte("\x89PNG\r\n\x1a\n0000")
	req := httptest.NewRequest(http.MethodPut, "/docs/_brick/upload", bytes.NewReader(png))
	req.Header.Set("Brick-Upload", "avatar")
	req.Header.Set("Brick-Handle", m[1])
	req.Header.Set("Brick-Token", m[2])
	req.Header.Set("Brick-Name", "me.png")
	req.Header.Set("Content-Type", "image/png")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<b>me.png</b>") || !strings.Contains(rec.Body.String(), `href="/docs/about/"`) {
		t.Fatalf("upload under the base: %d %s", rec.Code, rec.Body)
	}
	if sink.Len() != 1 {
		t.Fatalf("sink holds %d files", sink.Len())
	}
	// The runtime asks for the envelope: its HTML is rebased too.
	m = regexp.MustCompile(`data-brick-state=\\?"([^"\\]+)\\?" data-brick-token=\\?"([^"\\]+)`).FindStringSubmatch(rec.Body.String())
	req = httptest.NewRequest(http.MethodPut, "/docs/_brick/upload", bytes.NewReader(png))
	req.Header.Set("Brick-Upload", "avatar")
	req.Header.Set("Brick-Handle", m[1])
	req.Header.Set("Brick-Token", m[2])
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Accept", brick.EffectsContentType)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var env struct{ HTML string }
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || rec.Code != http.StatusOK || !strings.Contains(env.HTML, `href="/docs/about/"`) {
		t.Fatalf("envelope under the base: %d %v %s", rec.Code, err, rec.Body)
	}
}
