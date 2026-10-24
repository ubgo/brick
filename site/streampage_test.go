package site

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

// flushLog records what was written before each flush.
type flushLog struct {
	*httptest.ResponseRecorder
	parts []string
	cur   strings.Builder
}

func (f *flushLog) Write(p []byte) (int, error) {
	f.cur.Write(p)
	return f.ResponseRecorder.Write(p)
}

func (f *flushLog) Flush() {
	f.parts = append(f.parts, f.cur.String())
	f.cur.Reset()
	f.ResponseRecorder.Flush()
}

func streamSite(t *testing.T, opts Options) *Site {
	t.Helper()
	return newSite(t, project(
		"content/index.html", `<html><body><p>top</p><brick:boundary><Slow brick:lazy="stream" /><brick:prop name="loading"><i>wait</i></brick:prop></brick:boundary><a href="/x">x</a></body></html>`,
		"components/Slow.html", `<b><a href="/in">in</a></b>`,
	), opts)
}

// A browser document load streams: the page up to </body> is flushed with
// the placeholder, the island follows (its URLs rebased like the page's),
// then </body>. Any other request gets the page rendered in place.
func TestStreamedPage(t *testing.T) {
	t.Parallel()
	s := streamSite(t, Options{BasePath: "/docs"})
	h := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	rec := &flushLog{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("status %d headers %v", rec.Code, rec.Header())
	}
	if len(rec.parts) != 2 {
		t.Fatalf("flushes %d: %q", len(rec.parts), rec.parts)
	}
	head, fill, tail := rec.parts[0], rec.parts[1], rec.cur.String()
	if !strings.Contains(head, `<brick-lazy data-brick-stream="s1"><i>wait</i></brick-lazy><a href="/docs/x">x</a>`) || strings.Contains(head, "</body>") || strings.Contains(head, "<b>") {
		t.Fatalf("head: %s", head)
	}
	if !strings.Contains(fill, `<brick-fill data-brick-fill="s1" hidden><b><a href="/docs/in">in</a></b></brick-fill>`) {
		t.Fatalf("fill: %s", fill)
	}
	if tail != "</body></html>" {
		t.Fatalf("tail: %q", tail)
	}

	for _, mode := range []string{"", "cors"} {
		req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
		if mode != "" {
			req.Header.Set("Sec-Fetch-Mode", mode)
		}
		rec := &flushLog{ResponseRecorder: httptest.NewRecorder()}
		h.ServeHTTP(rec, req)
		body := rec.Body.String()
		if len(rec.parts) != 0 || strings.Contains(body, "brick-lazy") || !strings.Contains(body, `<p>top</p><b><a href="/docs/in">in</a></b><a href="/docs/x">`) {
			t.Fatalf("mode %q must render in place (flushes %d): %s", mode, len(rec.parts), body)
		}
	}
}

// Export renders a stream island in place.
func TestStreamedPageExport(t *testing.T) {
	t.Parallel()
	s := streamSite(t, Options{})
	res, err := s.Render(t.Context(), "/")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.current().eng.Fill(t.Context(), res, brick.WithStaticState())
	if err != nil || !strings.Contains(string(b), "<p>top</p><b>") {
		t.Fatalf("%v %s", err, b)
	}
}
