package site

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// streamScripting's one action streams two chunks before it answers.
type streamScripting struct{}

func (streamScripting) Compile(context.Context, brick.ScriptSource) (brick.ScriptModule, error) {
	return streamModule{}, nil
}

type streamModule struct{}

func (streamModule) Functions() []string { return []string{"ask"} }
func (streamModule) Call(ctx context.Context, _ string, args ...any) (any, error) {
	s := brick.StreamFrom(ctx)
	_ = s.Write("answer", "Hel", false)
	_ = s.Write("answer", "lo", false)
	return map[string]any{"answer": "Hello"}, nil
}

// Under a base path, the island endpoint is served beside the action
// endpoint and its HTML and asset URLs are rebased; a streamed action is
// passed on line by line with its envelope rebased; the page with a lazy
// island is private.
func TestLazyAndStreamUnderBasePath(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["layouts/default.html"] = &fstest.MapFile{Data: []byte("<html><head></head><body>{{ assets }}{{ children }}</body></html>")}
	fsys["components/Card.html"] = &fstest.MapFile{Data: []byte("<brick:style>.c { color: red }</brick:style><a class=\"c\" href=\"/about/\">{{ .Props.t }}</a>")}
	fsys["components/Chat.html"] = &fstest.MapFile{Data: []byte("---\nstate:\n  answer: \"\"\n---\n<brick:script>chat</brick:script>\n<div><p brick:ref=\"answer\">{{ .State.answer }}</p><a href=\"/about/\">about</a></div>")}
	fsys["content/lazy.html"] = &fstest.MapFile{Data: []byte(`<Card brick:lazy t="hi" />`)}
	fsys["content/chat.html"] = &fstest.MapFile{Data: []byte(`<Chat />`)}
	s := newSite(t, fsys, Options{BasePath: "/docs/", Engine: brick.Options{Scripting: streamScripting{}}})
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/docs/lazy/", nil))
	page := rec.Body.String()
	m := regexp.MustCompile(`data-brick-lazy="([^"]+)"`).FindStringSubmatch(page)
	if m == nil || strings.Contains(page, ">hi<") || rec.Header().Get("Cache-Control") != DefaultStateCacheControl {
		t.Fatalf("lazy page: %v %s", rec.Header(), page)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/docs/_brick/island?t="+m[1], nil))
	var ans struct {
		HTML   string `json:"html"`
		Assets []struct {
			URL string `json:"url"`
		} `json:"assets"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &ans) != nil {
		t.Fatalf("island: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(ans.HTML, `href="/docs/about/">hi</a>`) || len(ans.Assets) == 0 || !strings.HasPrefix(ans.Assets[0].URL, "/docs/_brick/c/") {
		t.Fatalf("island answer not rebased: %s", rec.Body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/docs/chat/", nil))
	sm := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	req := httptest.NewRequest("POST", "/docs/_brick/action", strings.NewReader(`{"handle":"`+sm[1]+`","token":"`+sm[2]+`","action":"ask"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	req.Header.Set("Accept", brick.EffectsContentType+", "+brick.StreamContentType)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if rec.Header().Get("Content-Type") != brick.StreamContentType || !rec.Flushed || len(lines) != 3 {
		t.Fatalf("stream: %v %s", rec.Header(), rec.Body)
	}
	if lines[0] != `{"stream":{"ref":"answer","text":"Hel"}}` || !strings.Contains(lines[2], `href=\"/docs/about/\"`) {
		t.Fatalf("stream lines: %q", lines)
	}
}

// An export renders lazy islands in place and needs no keys for them.
func TestExportLazy(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["components/Card.html"] = &fstest.MapFile{Data: []byte("<b>{{ .Props.t }}</b>")}
	fsys["content/lazy.html"] = &fstest.MapFile{Data: []byte(`<Card brick:lazy t="hi" />`)}
	s := newSite(t, fsys, Options{})
	dir := t.TempDir()
	if _, err := s.Export(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "lazy", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	b := string(raw)
	if strings.Contains(b, "brick-lazy") || !strings.Contains(b, "<b>hi</b>") {
		t.Fatalf("export: %s", b)
	}
}
