package site

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ubgo/brick"
)

// handleRE finds a stateful root's handle and token in a page.
var handleRE = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([^"]+)"`)

// toggleSite is blog() plus one stateful component on /toggle/.
func toggleSite() fstest.MapFS {
	fsys := blog()
	fsys["components/Toggle.html"] = &fstest.MapFile{Data: []byte("---\nstate:\n  on: false\n---\n<brick:script>toggle</brick:script>\n<div><b>{{ .State.on }}</b><i>{{ with .Ctx.user }}{{ . }}{{ end }}</i><button brick:click=\"flip\">x</button></div>")}
	fsys["content/toggle.html"] = &fstest.MapFile{Data: []byte("<Toggle />")}
	return fsys
}

func testKeys(t *testing.T, b byte) brick.KeyRing {
	t.Helper()
	ring, err := brick.StaticKeys(bytes.Repeat([]byte{b}, brick.StateKeySize))
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

// get fetches url and returns the response and the page's handle and token.
func get(t *testing.T, h http.Handler, url string) (*httptest.ResponseRecorder, string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	m := handleRE.FindStringSubmatch(rec.Body.String())
	if m == nil {
		return rec, "", ""
	}
	return rec, m[1], m[2]
}

// act posts the flip action, with user as the X-User header.
func act(h http.Handler, handle, token, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(`{"handle":"`+handle+`","token":"`+token+`","action":"flip"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	req.Header.Set("X-User", user)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func userFromHeader(r *http.Request) []brick.RenderOption {
	return []brick.RenderOption{brick.Provide("user", r.Header.Get("X-User"))}
}

// A response holding a stateful component is private and varies on the
// recovery header; the cached page never holds a handle, so two visitors
// get two instances.
func TestStatefulResponseIsPrivate(t *testing.T) {
	t.Parallel()
	s := newSite(t, toggleSite(), Options{StateKeys: testKeys(t, 1), Engine: brick.Options{Scripting: flipScripting{}}})
	h := s.Handler()
	rec, h1, _ := get(t, h, "/toggle/")
	if h1 == "" {
		t.Fatalf("no handle: %s", rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != DefaultStateCacheControl {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, brick.RecoverHeader) {
		t.Errorf("Vary = %q", got)
	}
	if _, h2, _ := get(t, h, "/toggle/"); h2 == "" || h2 == h1 {
		t.Errorf("second visitor got the first one's handle: %q", h2)
	}
	res, err := s.Render(context.Background(), "/toggle/")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Chunks {
		if bytes.Contains(c.Bytes, []byte("data-brick-state")) {
			t.Fatalf("the cached page holds a handle: %s", c.Bytes)
		}
	}
	if rec, _, _ := get(t, h, "/about/"); rec.Header().Get("Cache-Control") != "" {
		t.Errorf("a page with no state is marked %q", rec.Header().Get("Cache-Control"))
	}
	custom := newSite(t, toggleSite(), Options{StateKeys: testKeys(t, 1), StateCacheControl: "no-store", Engine: brick.Options{Scripting: flipScripting{}}})
	if rec, _, _ := get(t, custom.Handler(), "/toggle/"); rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("StateCacheControl ignored: %q", rec.Header().Get("Cache-Control"))
	}
}

// With StateKeys, state is sealed into the page: a new Site (a restart, a
// second server) with the same keys answers an action on a handle the old
// one issued, and one with other keys does not.
func TestSealedStateSurvivesNewSite(t *testing.T) {
	t.Parallel()
	opts := Options{StateKeys: testKeys(t, 1), Engine: brick.Options{Scripting: flipScripting{}}}
	_, handle, token := get(t, newSite(t, toggleSite(), opts).Handler(), "/toggle/")
	if handle == "" {
		t.Fatal("no handle")
	}
	if rec := act(newSite(t, toggleSite(), opts).Handler(), handle, token, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<b>true</b>") {
		t.Fatalf("restarted site: %d %s", rec.Code, rec.Body)
	}
	other := Options{StateKeys: testKeys(t, 2), Engine: brick.Options{Scripting: flipScripting{}}}
	if rec := act(newSite(t, toggleSite(), other).Handler(), handle, token, ""); rec.Code != http.StatusGone {
		t.Fatalf("other keys: %d %s", rec.Code, rec.Body)
	}
}

// Options.Request reaches actions too: the re-render sees the user of the
// request making the action, not the one who first loaded the page
// .
func TestActionSeesCurrentRequest(t *testing.T) {
	t.Parallel()
	s := newSite(t, toggleSite(), Options{StateKeys: testKeys(t, 1), Request: userFromHeader, Engine: brick.Options{Scripting: flipScripting{}}})
	h := s.Handler()
	req := httptest.NewRequest(http.MethodGet, "/toggle/", nil)
	req.Header.Set("X-User", "sara")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	m := handleRE.FindStringSubmatch(rec.Body.String())
	if m == nil || !strings.Contains(rec.Body.String(), "<i>sara</i>") {
		t.Fatalf("page: %s", rec.Body)
	}
	if rec := act(h, m[1], m[2], "omar"); !strings.Contains(rec.Body.String(), "<i>omar</i>") {
		t.Fatalf("action re-render: %d %s", rec.Code, rec.Body)
	}
}

// Export refuses a stateful page when its handles would die with this
// process, and with keys writes a page whose blob a live server opens.
func TestExportStatefulPages(t *testing.T) {
	t.Parallel()
	mem := newSite(t, toggleSite(), Options{Engine: brick.Options{Scripting: flipScripting{}}})
	if _, err := mem.Export(context.Background(), filepath.Join(t.TempDir(), "out")); !errors.Is(err, ErrExportNeedsKeys) {
		t.Fatalf("memory store export: %v", err)
	}
	opts := Options{StateKeys: testKeys(t, 1), Engine: brick.Options{Scripting: flipScripting{}}}
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := newSite(t, toggleSite(), opts).Export(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "toggle", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	m := handleRE.FindSubmatch(b)
	if m == nil {
		t.Fatalf("exported page has no handle: %s", b)
	}
	if rec := act(newSite(t, toggleSite(), opts).Handler(), string(m[1]), string(m[2]), ""); rec.Code != http.StatusOK {
		t.Fatalf("action on an exported handle: %d %s", rec.Code, rec.Body)
	}
}

// A site with neither keys nor a store warns once, on the first stateful
// response, that state lives in the process.
func TestMemoryStateWarnsOnce(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	s := newSite(t, toggleSite(), Options{Logger: slog.New(slog.NewTextHandler(&logs, nil)), Engine: brick.Options{Scripting: flipScripting{}}})
	h := s.Handler()
	get(t, h, "/about/")
	if strings.Contains(logs.String(), "keep state in this process") {
		t.Fatal("warned before any stateful response")
	}
	get(t, h, "/toggle/")
	get(t, h, "/toggle/")
	if n := strings.Count(logs.String(), "keep state in this process"); n != 1 {
		t.Fatalf("warned %d times: %s", n, logs.String())
	}
}

// Engine.Sessions binds what a response renders to the visitor's session:
// an action from another session (sign-out rotated the id) is refused as
// gone, so it recovers under the new session.
func TestSessionBindsRenders(t *testing.T) {
	t.Parallel()
	sess := brick.SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get("X-Sess"), nil })
	s := newSite(t, toggleSite(), Options{StateKeys: testKeys(t, 1), Engine: brick.Options{Scripting: flipScripting{}, Sessions: sess}})
	h := s.Handler()
	req := httptest.NewRequest(http.MethodGet, "/toggle/", nil)
	req.Header.Set("X-Sess", "s-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	m := handleRE.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("page: %s", rec.Body)
	}
	post := func(session string) int {
		req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(`{"handle":"`+m[1]+`","token":"`+m[2]+`","action":"flip"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Brick-Action", "1")
		req.Header.Set("X-Sess", session)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post("s-2"); code != http.StatusGone {
		t.Fatalf("another session: %d", code)
	}
	if code := post("s-1"); code != http.StatusOK {
		t.Fatalf("same session: %d", code)
	}
}

// pruneStore is a store that needs pruning (a SQL table).
type pruneStore struct {
	brick.StateStore
	calls int
}

func (p *pruneStore) Prune(context.Context, time.Time) (int, error) { p.calls++; return 2, nil }

// Prune reaches every store that needs it, default and named.
func TestPruneReachesStores(t *testing.T) {
	t.Parallel()
	def, named := &pruneStore{StateStore: brick.NewMemoryStates(0, 0)}, &pruneStore{StateStore: brick.NewMemoryStates(0, 0)}
	s := newSite(t, blog(), Options{Engine: brick.Options{States: def, StateStores: map[string]brick.StateStore{"sql": named}}})
	if n, err := s.Prune(context.Background()); err != nil || n != 4 || def.calls != 1 || named.calls != 1 {
		t.Fatalf("prune: %d %v %d %d", n, err, def.calls, named.calls)
	}
}

// expiringBytes is a BytesStore that expires on its own (Redis) and so is
// not a Pruner; only the methods Prune reaches matter here.
type expiringBytes struct{}

func (expiringBytes) Load(context.Context, string) ([]byte, bool, error) { return nil, false, nil }
func (expiringBytes) Save(context.Context, string, []byte, time.Duration, string) error {
	return nil
}
func (expiringBytes) Remove(context.Context, string) error { return nil }

// A Bytes store over a store that cannot prune is skipped, not reported:
// brick.Bytes answers ErrUnsupported, which is not a failure.
func TestPruneSkipsStoresThatExpireThemselves(t *testing.T) {
	t.Parallel()
	named := &pruneStore{StateStore: brick.NewMemoryStates(0, 0)}
	s := newSite(t, blog(), Options{Engine: brick.Options{
		States:      brick.Bytes(expiringBytes{}, brick.BytesOptions{}),
		StateStores: map[string]brick.StateStore{"sql": named},
	}})
	if n, err := s.Prune(context.Background()); err != nil || n != 2 || named.calls != 1 {
		t.Fatalf("prune: %d %v %d", n, err, named.calls)
	}
}
