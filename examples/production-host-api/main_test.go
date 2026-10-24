package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ubgo/brick"
)

// stateAttrs finds the handle and token the page carries for Counter.
var stateAttrs = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

func engine(t *testing.T, l limits) *brick.Engine {
	t.Helper()
	eng, err := newEngine("theme", l)
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

// Render a whole page and one component alone.
func TestRender(t *testing.T) {
	eng := engine(t, limits{})
	ctx := context.Background()
	page, err := eng.Render(ctx, "content/static.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "<h1>Static</h1>\n<p class=\"greeting\">Hello, world!</p>\n"; string(page) != want {
		t.Fatalf("Render:\n%q\nwant\n%q", page, want)
	}
	one, err := eng.RenderComponent(ctx, "Greeting", map[string]any{"name": "Ada"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "<p class=\"greeting\">Hello, Ada!</p>"; string(one) != want {
		t.Fatalf("RenderComponent:\n%q\nwant\n%q", one, want)
	}
}

// The cached page is rendered once; only the island differs per request.
func TestHomeFillsIsland(t *testing.T) {
	h := (&server{eng: engine(t, limits{})}).handler()
	get := func(url string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", url, rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	if ada, guest := get("/?user=Ada"), get("/"); !strings.Contains(ada, "Signed in as Ada") || !strings.Contains(guest, "Signed in as guest") {
		t.Fatalf("island per request:\n%s\n---\n%s", ada, guest)
	}
	if got := get("/preview/Greeting?name=Lin"); got != "<p class=\"greeting\">Hello, Lin!</p>" {
		t.Fatalf("preview: %q", got)
	}
}

// RunAction is an action without HTTP; Instance reads what the store holds.
func TestRunAction(t *testing.T) {
	eng := engine(t, limits{})
	ctx := context.Background()
	page, err := eng.Render(ctx, "content/index.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := stateAttrs.FindStringSubmatch(string(page))
	if m == nil {
		t.Fatalf("no stateful component:\n%s", page)
	}
	res, err := eng.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "bump"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != 1 || !strings.Contains(string(res.HTML), "Clicked 1 times") {
		t.Fatalf("RunAction: version %d\n%s", res.Version, res.HTML)
	}
	view, err := eng.Instance(ctx, m[1])
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := view.Get("count"); view.Component != "Counter" || count != int64(1) {
		t.Fatalf("Instance: %s %#v", view.Component, view.State)
	}
	// A wrong token is refused before any action code runs.
	if _, err := eng.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: "00", Action: "bump"}); !errors.Is(err, brick.ErrForbidden) {
		t.Fatalf("bad token: %v", err)
	}
	if _, err := eng.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "nope"}); err == nil {
		t.Fatal("unknown action ran")
	} else {
		t.Logf("unknown action: %v (status %d)", err, brick.ActionStatus(err))
	}
}

// Same input, same bytes — from many goroutines on one engine.
func TestDeterministicAndConcurrent(t *testing.T) {
	eng := engine(t, limits{})
	ctx := context.Background()
	names := map[string]any{"names": []any{"Ada", "Lin", "Sam"}}
	first, err := eng.Render(ctx, "content/list.html", names)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			got, err := eng.Render(ctx, "content/list.html", names)
			if err != nil || !bytes.Equal(got, first) {
				t.Errorf("concurrent render differs: %v\n%s", err, got)
			}
		})
	}
	wg.Wait()
	t.Logf("list: %s", first)
}

// Limits fail the render with an error naming the limit and the file.
func TestLimits(t *testing.T) {
	ctx := context.Background()
	names := map[string]any{"names": []any{"a", "b", "c", "d", "e", "f"}}
	_, err := engine(t, limits{MaxComponents: 5}).Render(ctx, "content/list.html", names)
	if err == nil || !strings.Contains(err.Error(), "more than MaxComponents (5) components in one render") {
		t.Fatalf("MaxComponents: %v", err)
	}
	t.Logf("MaxComponents: %v", err)
	_, err = engine(t, limits{MaxOutputBytes: 64}).Render(ctx, "content/list.html", names)
	if err == nil || !strings.Contains(err.Error(), "output passed MaxOutputBytes (64)") {
		t.Fatalf("MaxOutputBytes: %v", err)
	}
	t.Logf("MaxOutputBytes: %v", err)
	_, err = engine(t, limits{}).Render(ctx, "content/loop.html", nil)
	var be *brick.Error
	if !errors.As(err, &be) || be.Code != brick.CodeLimit || !strings.Contains(err.Error(), "render depth passed MaxDepth (64)") {
		t.Fatalf("MaxDepth: %v", err)
	}
	t.Logf("MaxDepth: file=%s code=%s chain=%d", be.File, be.Code, len(be.Chain))
}

// WithStats makes Fill add the islands' counts to a copy of the page's.
func TestPageStats(t *testing.T) {
	eng := engine(t, limits{})
	page, err := eng.RenderChunks(context.Background(), teamPage, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := page.Stats
	got, err := pageStats(context.Background(), eng, page)
	if err != nil {
		t.Fatal(err)
	}
	if got.Islands != 1 || got.Components != before.Components+2 {
		t.Fatalf("Fill added nothing: before %+v after %+v", before, got)
	}
	if page.Stats.Components != before.Components {
		t.Fatalf("pageStats changed the cached Result: %+v", page.Stats)
	}
	h := (&server{eng: eng}).handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"Islands":1`) {
		t.Fatalf("/stats %d %s", rec.Code, rec.Body)
	}
	t.Logf("stats: %s", rec.Body)
}

// A pinned Rand and Clock fix the action token across engines; the default
// random source gives every render a new one. The handle is the store's own
// id (random in NewMemoryStates), so only the token is pinned.
func TestPinnedStateTokens(t *testing.T) {
	ctx := context.Background()
	fixed := func() time.Time { return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC) }
	seed := func() io.Reader { return bytes.NewReader(bytes.Repeat([]byte{7}, 1<<12)) }
	token := func(eng *brick.Engine) string {
		t.Helper()
		page, err := eng.Render(ctx, "content/index.html", nil)
		if err != nil {
			t.Fatal(err)
		}
		m := stateAttrs.FindStringSubmatch(string(page))
		if m == nil {
			t.Fatalf("no stateful component:\n%s", page)
		}
		return m[2]
	}
	a, err := newPinnedEngine("theme", sources{Clock: fixed, Rand: seed()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := newPinnedEngine("theme", sources{Clock: fixed, Rand: seed()})
	if err != nil {
		t.Fatal(err)
	}
	if ta, tb := token(a), token(b); ta != tb || ta != "07070707070707070707070707070707" {
		t.Fatalf("pinned tokens: %s %s", ta, tb)
	}
	free := engine(t, limits{})
	first, second := token(free), token(free)
	if first == second {
		t.Fatal("unpinned renders carried the same token; it should be random")
	}
}

// The host's ActionErrors envelope and MaxActionBytes cap answer a bad
// action request.
func TestActionEnvelopeAndCap(t *testing.T) {
	h := (&server{eng: engine(t, limits{MaxActionBytes: 64})}).handler()
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(body))
		req.Header.Set("Brick-Action", "1")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := post(`{"handle":"x","token":"y","action":"bump"}`)
	t.Logf("unknown handle: %d %s", rec.Code, rec.Body)
	if ct := rec.Header().Get("Content-Type"); rec.Code < 400 || ct != "application/json" || !strings.HasPrefix(rec.Body.String(), `{"error":`) {
		t.Fatalf("envelope: %d %q %s", rec.Code, ct, rec.Body)
	}
	// The same request, padded past the 64-byte cap, is refused as a bad
	// request before any state is looked up; with the default cap it is
	// just another unknown handle.
	padded := `{"handle":"x","token":"y","action":"bump","pad":"` + strings.Repeat("a", 100) + `"}`
	big := post(padded)
	t.Logf("oversize: %d %s", big.Code, big.Body)
	if big.Code != http.StatusBadRequest {
		t.Fatalf("oversize body: %d %s", big.Code, big.Body)
	}
	roomy := (&server{eng: engine(t, limits{})}).handler()
	req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(padded))
	req.Header.Set("Brick-Action", "1")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	roomy.ServeHTTP(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("default cap: %d %s", rec.Code, rec.Body)
	}
}

// inspect reads an instance without changing it.
func TestInspect(t *testing.T) {
	eng := engine(t, limits{})
	ctx := context.Background()
	page, err := eng.Render(ctx, "content/index.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := stateAttrs.FindStringSubmatch(string(page))
	if m == nil {
		t.Fatal("no stateful component")
	}
	name, ver, err := inspect(ctx, eng, m[1])
	if err != nil || name != "Counter" || ver != 0 {
		t.Fatalf("inspect: %q %d %v", name, ver, err)
	}
}

// A script call is bounded by steps and by wall-clock time, per call; a
// loader that loops forever fails its render instead of hanging it.
func TestScriptLimits(t *testing.T) {
	ctx := context.Background()
	ok, err := engine(t, limits{}).RenderComponent(ctx, "Spin", map[string]any{"rounds": 100}, "")
	if err != nil || string(ok) != `<p class="spin">4950</p>` {
		t.Fatalf("small loop: %q %v", ok, err)
	}
	_, err = engine(t, limits{ScriptSteps: 10_000}).RenderComponent(ctx, "Spin", map[string]any{"rounds": 1_000_000}, "")
	if err == nil || !strings.Contains(err.Error(), "too many steps") {
		t.Fatalf("step limit: %v", err)
	}
	t.Logf("steps: %v", err)
	_, err = engine(t, limits{ScriptTimeout: time.Millisecond}).RenderComponent(ctx, "Spin", map[string]any{"rounds": 50_000_000}, "")
	if err == nil || !strings.Contains(err.Error(), "time limit") {
		t.Fatalf("time limit: %v", err)
	}
	t.Logf("timeout: %v", err)
}
