package brick

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Tests for: pluggable stores, sealed state, session
// binding, action hooks. BV in a comment marks a break-verified test.

func testKey(b byte) []byte { return bytes.Repeat([]byte{b}, StateKeySize) }

// sealedOpts are the test's SealedStates settings: keys (default one),
// and Rand for the sealer.
type sealedOpts struct {
	SealedOptions
	Keys [][]byte
	Rand io.Reader
}

func sealedStore(t *testing.T, o sealedOpts) *SealedStates {
	t.Helper()
	if o.Keys == nil {
		o.Keys = [][]byte{testKey(1)}
	}
	ring, err := StaticKeys(o.Keys...)
	if err != nil {
		t.Fatal(err)
	}
	opts := o.SealedOptions
	opts.Sealer = NewAESGCMSealer(ring, o.Rand)
	s, err := NewSealedStates(opts)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// v5Page is a counter whose template also prints the request's user.
func v5Theme() map[string]string {
	return map[string]string{
		"components/Counter.html": "---\nstate:\n  count: 0\n  secret: \"hush-0451\"\nprops:\n  step: {type: int, default: 1}\n---\n<brick:script>counter</brick:script>\n<div class=\"counter\"><span>{{ .State.count }}</span><i>{{ .Ctx.user }}</i><button brick:click=\"increment\">+</button></div>",
		"content/a.html":          `<Counter step="2" />`,
	}
}

func v5Engine(t *testing.T, opts Options) *Engine {
	t.Helper()
	files := []string{}
	for k, v := range v5Theme() {
		files = append(files, k, v)
	}
	opts.FS = theme(files...)
	if opts.Scripting == nil {
		opts.Scripting = counterScripting()
	}
	return newEngine(t, opts)
}

// fillPage renders a.html and returns the page and its handle and token.
func fillPage(t *testing.T, e *Engine, opts ...RenderOption) (page, handle, token string) {
	t.Helper()
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Fill(ctx, r, opts...)
	if err != nil {
		t.Fatal(err)
	}
	m := stateRE.FindStringSubmatch(string(b))
	if m == nil {
		t.Fatalf("no handle in %s", b)
	}
	return string(b), m[1], m[2]
}

// postAction calls the action endpoint with the given extra headers.
func postAction(e *Engine, handle, token, action string, headers map[string]string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"handle":%q,"token":%q,"action":%q,"args":[]}`, handle, token, action)
	req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ActionHandler().ServeHTTP(rec, req)
	return rec
}

func TestSealedSurvivesNewEngine(t *testing.T) {
	t.Parallel()
	a := v5Engine(t, Options{States: sealedStore(t, sealedOpts{})})
	_, h, tok := fillPage(t, a)
	if !strings.HasPrefix(h, "default.s1.") {
		t.Fatalf("handle %q: want the store prefix and a sealed blob", h)
	}
	// A second engine — a restart, another server — with the same key.
	b := v5Engine(t, Options{States: sealedStore(t, sealedOpts{})})
	rec := postAction(b, h, tok, "increment", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<span>2</span>") {
		t.Fatalf("action on a new engine: %d %s", rec.Code, rec.Body)
	}
	next := stateRE.FindStringSubmatch(rec.Body.String())
	if next == nil || next[1] == h {
		t.Fatal("a sealed action response must carry the next blob")
	}
	if rec := postAction(a, next[1], tok, "increment", nil); !strings.Contains(rec.Body.String(), "<span>4</span>") {
		t.Fatalf("the next blob carries the new state: %d %s", rec.Code, rec.Body)
	}
}

// BV: break subkey's use of the tag (open without authentication) and
// this fails.
func TestSealedTamper(t *testing.T) {
	t.Parallel()
	s := sealedStore(t, sealedOpts{})
	ctx := WithStateScope(context.Background(), StateScope{Store: "default"})
	h, err := s.Put(ctx, &Instance{Token: "t", Name: "X", State: map[string]any{"n": int64(1)}})
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(h, sealedPrefix))
	if len(blob) == 0 {
		t.Fatal("no blob")
	}
	for i := range blob {
		bad := bytes.Clone(blob)
		bad[i] ^= 0x01
		if inst, ok, err := s.Get(ctx, sealedPrefix+base64.RawURLEncoding.EncodeToString(bad)); ok || inst != nil || err != nil {
			t.Fatalf("byte %d flipped: opened %v %v", i, inst, err)
		}
	}
	if _, ok, _ := s.Get(ctx, h); !ok {
		t.Fatal("the untouched blob must open")
	}
}

// BV: decode the handle leniently in SealedStates.Get and this fails. A
// handle has one spelling: a last character that differs only in the bits
// base64 drops (the lazy-ticket "forged" case, which then opened about one
// time in five) is not found.
func TestSealedHandleOneSpelling(t *testing.T) {
	t.Parallel()
	s := sealedStore(t, sealedOpts{})
	ctx := WithStateScope(context.Background(), StateScope{Store: "default"})
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	// Grow the state a byte at a time until the blob leaves unused bits in
	// the last character (its length is not a multiple of 3).
	for pad := 0; pad < 3; pad++ {
		h, err := s.Put(ctx, &Instance{Token: "t", Name: "X", State: map[string]any{"s": strings.Repeat("x", pad)}})
		if err != nil {
			t.Fatal(err)
		}
		rest := strings.TrimPrefix(h, sealedPrefix)
		blob, _ := base64.RawURLEncoding.DecodeString(rest)
		if len(blob)%3 == 0 {
			continue
		}
		last := strings.IndexByte(alphabet, rest[len(rest)-1])
		alt := sealedPrefix + rest[:len(rest)-1] + string(alphabet[last^1])
		if inst, ok, err := s.Get(ctx, alt); ok || inst != nil || err != nil {
			t.Fatalf("a second spelling of the handle opened: %v %v", inst, err)
		}
		if _, ok, _ := s.Get(ctx, h); !ok {
			t.Fatal("the canonical handle must open")
		}
		return
	}
	t.Fatal("no blob length left unused bits")
}

// BV: drop the store or session from sealAAD and this fails.
func TestSealedAdditionalData(t *testing.T) {
	t.Parallel()
	s := sealedStore(t, sealedOpts{})
	bg := context.Background()
	scope := func(store, sess string) context.Context {
		return WithStateScope(bg, StateScope{Store: store, Session: sess})
	}
	h, err := s.Put(scope("default", "sess-a"), &Instance{Token: "t", Name: "X", Session: "sess-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(scope("default", "sess-a"), h); !ok {
		t.Fatal("same store and session must open")
	}
	if _, ok, _ := s.Get(scope("other", "sess-a"), h); ok {
		t.Fatal("a blob opened under another store name must not open")
	}
	if _, ok, _ := s.Get(scope("default", "sess-b"), h); ok {
		t.Fatal("a blob opened under another session must not open")
	}
	if _, ok, _ := s.Get(scope("default", ""), h); ok {
		t.Fatal("a session-bound blob must not open signed out")
	}
}

// BV: make open try every key regardless of keyID, or seal with the last
// key, and this fails.
func TestSealedKeyRotation(t *testing.T) {
	t.Parallel()
	ctx := WithStateScope(context.Background(), StateScope{Store: "default"})
	old := sealedStore(t, sealedOpts{Keys: [][]byte{testKey(1)}})
	h, _ := old.Put(ctx, &Instance{Token: "t", Name: "X"})
	both := sealedStore(t, sealedOpts{Keys: [][]byte{testKey(2), testKey(1)}})
	if _, ok, _ := both.Get(ctx, h); !ok {
		t.Fatal("a second key must still open")
	}
	h2, _ := both.Put(ctx, &Instance{Token: "t", Name: "X"})
	if _, ok, _ := old.Get(ctx, h2); ok {
		t.Fatal("the first key seals: the old-key-only store must not open it")
	}
	newOnly := sealedStore(t, sealedOpts{Keys: [][]byte{testKey(2)}})
	if _, ok, _ := newOnly.Get(ctx, h); ok {
		t.Fatal("a removed key must not open")
	}
	if _, ok, _ := newOnly.Get(ctx, h2); !ok {
		t.Fatal("the new key opens what it sealed")
	}
	if _, err := StaticKeys([]byte("short")); err == nil {
		t.Fatal("a short key must be refused")
	}
}

// BV: drop the expiry check in open and this fails.
func TestSealedExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	s := sealedStore(t, sealedOpts{SealedOptions: SealedOptions{TTL: time.Hour, Clock: func() time.Time { return now }}})
	ctx := WithStateScope(context.Background(), StateScope{Store: "default"})
	h, _ := s.Put(ctx, &Instance{Token: "t", Name: "X"})
	for _, tc := range []struct {
		later time.Duration
		ok    bool
	}{{59 * time.Minute, true}, {61 * time.Minute, true}, {63 * time.Minute, false}} {
		later := sealedStore(t, sealedOpts{SealedOptions: SealedOptions{TTL: time.Hour, Clock: func() time.Time { return now.Add(tc.later) }}})
		if _, ok, _ := later.Get(ctx, h); ok != tc.ok {
			t.Errorf("after %v: open=%v want %v (2-minute skew allowance)", tc.later, ok, tc.ok)
		}
	}
	// An export's blob never expires; its first action re-seals with a TTL.
	exp, _ := s.Put(WithStateScope(ctx, StateScope{Store: "default", NoExpiry: true}), &Instance{Token: "t", Name: "X"})
	far := sealedStore(t, sealedOpts{SealedOptions: SealedOptions{TTL: time.Hour, Clock: func() time.Time { return now.AddDate(1, 0, 0) }}})
	if _, ok, _ := far.Get(ctx, exp); !ok {
		t.Fatal("a static export's blob must not expire")
	}
}

func TestSealedTooLarge(t *testing.T) {
	t.Parallel()
	s := sealedStore(t, sealedOpts{SealedOptions: SealedOptions{MaxBytes: 200}})
	_, err := s.Put(context.Background(), &Instance{Token: "t", Name: "Feed", State: map[string]any{"rows": strings.Repeat("x", 500)}})
	if !errors.Is(err, ErrStateValue) || !strings.Contains(err.Error(), "<Feed>") {
		t.Fatalf("want a MaxBytes error naming <Feed>: %v", err)
	}
}

// BV: make Put base64 the plaintext without encrypting and this fails.
func TestSealedOpaque(t *testing.T) {
	t.Parallel()
	e := v5Engine(t, Options{States: sealedStore(t, sealedOpts{})})
	page, h, _ := fillPage(t, e)
	if strings.Contains(page, "hush-0451") {
		t.Fatal("state must not be readable in the page")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(h, "default."+sealedPrefix))
	if bytes.Contains(raw, []byte("hush")) || bytes.Contains(raw, []byte("count")) {
		t.Fatal("the blob must be encrypted, not encoded")
	}
}

// Documented behaviour (gotcha 1): AEAD stops changes, not replays. An
// older blob opens and its older state comes back; facts live in the
// database.
func TestSealedReplayIsOldState(t *testing.T) {
	t.Parallel()
	e := v5Engine(t, Options{States: sealedStore(t, sealedOpts{})})
	_, h, tok := fillPage(t, e)
	first := postAction(e, h, tok, "increment", nil)
	if !strings.Contains(first.Body.String(), "<span>2</span>") {
		t.Fatal(first.Body.String())
	}
	replay := postAction(e, h, tok, "increment", nil)
	if !strings.Contains(replay.Body.String(), "<span>2</span>") {
		t.Fatalf("replaying the first blob starts from its state (0), not the latest (2): %s", replay.Body)
	}
}

func TestSealedDeterministic(t *testing.T) {
	t.Parallel()
	now := func() time.Time { return time.Unix(1_800_000_000, 0) }
	seal := func() string {
		s := sealedStore(t, sealedOpts{Rand: bytes.NewReader(bytes.Repeat([]byte{9}, 64)), SealedOptions: SealedOptions{Clock: now}})
		h, err := s.Put(context.Background(), &Instance{Token: "t", Name: "X", State: map[string]any{"a": int64(1)}})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	if a, b := seal(), seal(); a != b {
		t.Fatalf("injected Rand and Now must make sealing deterministic:\n%s\n%s", a, b)
	}
}

func TestHandlePrefixDispatch(t *testing.T) {
	t.Parallel()
	mem := NewMemoryStates(0, 0)
	e := v5Engine(t, Options{States: sealedStore(t, sealedOpts{}), StateStores: map[string]StateStore{"mem": mem}})
	_, h, tok := fillPage(t, e)
	rest := strings.TrimPrefix(h, "default.")
	for name, handle := range map[string]string{
		"unknown store": "gone." + rest,
		"no prefix":     rest,
		"other store":   "mem." + rest,
	} {
		if rec := postAction(e, handle, tok, "increment", nil); rec.Code != http.StatusGone {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	// An unknown encoding version is "not found", never a 500.
	bs := Bytes(&oneBlob{data: []byte(`{"v":2,"token":"t"}`)}, BytesOptions{})
	if inst, ok, err := bs.Get(context.Background(), "k"); ok || inst != nil || err != nil {
		t.Fatalf("v2 blob: %v %v %v", inst, ok, err)
	}
	if _, err := New(Options{States: mem, StateStores: map[string]StateStore{"default": mem}}); err == nil {
		t.Fatal(`StateStores may not reuse "default"`)
	}
	if _, err := New(Options{StateStores: map[string]StateStore{"a.b": mem}}); err == nil {
		t.Fatal("a store name with a dot must be refused")
	}
}

type oneBlob struct{ data []byte }

func (o *oneBlob) Load(context.Context, string) ([]byte, bool, error) { return o.data, true, nil }
func (o *oneBlob) Save(context.Context, string, []byte, time.Duration, string) error {
	return nil
}
func (o *oneBlob) Remove(context.Context, string) error { return nil }

func TestNamedStore(t *testing.T) {
	t.Parallel()
	mem := NewMemoryStates(0, 0)
	e := newEngine(t, Options{Scripting: counterScripting(), States: sealedStore(t, sealedOpts{}), StateStores: map[string]StateStore{"mem": mem}, FS: theme(
		"components/Feed.html", "---\nstate:\n  count: 0\nbrick:\n  store: mem\n---\n<brick:script>counter</brick:script>\n<div><span>{{ .State.count }}</span></div>",
		"components/Bad.html", "---\nstate:\n  count: 0\nbrick:\n  store: redis\n---\n<div>{{ .State.count }}</div>",
		"content/a.html", `<Feed />`,
		"content/bad.html", `<Bad />`,
	)})
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Fill(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if m := stateRE.FindStringSubmatch(string(b)); m == nil || !strings.HasPrefix(m[1], "mem.") || mem.Len() != 1 {
		t.Fatalf("brick: {store: mem} must use the mem store: %s (%d)", b, mem.Len())
	}
	_, err = e.RenderChunks(context.Background(), "content/bad.html", nil)
	if err == nil {
		r, _ := e.RenderChunks(context.Background(), "content/bad.html", nil)
		_, err = e.Fill(context.Background(), r)
	}
	if err == nil || !strings.Contains(err.Error(), `"redis"`) || !strings.Contains(err.Error(), "default, mem") {
		t.Fatalf("an unknown store name is an error listing the names: %v", err)
	}
}

func TestEncodingRoundTrip(t *testing.T) {
	t.Parallel()
	in := &Instance{Token: "t", Store: "redis", Path: "~/components/A.html", Name: "A", ID: "b1",
		State: map[string]any{"i": int64(3), "f": 3.0, "g": 2.5, "s": "<b>&", "l": []any{int64(1), 1.0}, "m": map[string]any{"x": nil}},
		Keys:  []string{"f", "i"}, Session: "abc", Version: 4, Issued: time.Date(2026, 10, 10, 9, 12, 44, 0, time.UTC)}
	b, err := EncodeInstance(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"f":3.0`)) || !bytes.Contains(b, []byte(`"v":1`)) || !bytes.Contains(b, []byte(`"s":"<b>&"`)) {
		t.Fatalf("encoding: %s", b)
	}
	out, err := DecodeInstance(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.State["i"] != int64(3) || out.State["f"] != 3.0 || out.Version != 4 || !out.Issued.Equal(in.Issued) || out.Store != "redis" {
		t.Fatalf("round trip: %#v", out)
	}
	again, _ := EncodeInstance(out)
	if !bytes.Equal(b, again) {
		t.Fatalf("not byte-stable:\n%s\n%s", b, again)
	}
}

func TestPutRejectsGoValue(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: sealedStore(t, sealedOpts{})})
	inst := &Instance{Token: "t", Store: DefaultStoreName, Name: "Profile", State: map[string]any{"joined": time.Now()}}
	err := e.putInstance(context.Background(), inst, false)
	if !errors.Is(err, ErrStateValue) || !strings.Contains(err.Error(), "<Profile>") || !strings.Contains(err.Error(), "state.joined: time.Time is not JSON-compatible") {
		t.Fatalf("want the path and type named: %v", err)
	}
	// The memory store needs no encoding: it accepts, and Dev warns.
	dev := newEngine(t, Options{States: NewMemoryStates(0, 0), Mode: Dev})
	if err := dev.putInstance(context.Background(), &Instance{Token: "t", Store: DefaultStoreName, Name: "Profile", State: map[string]any{"joined": time.Now()}}, false); err != nil {
		t.Fatal(err)
	}
	if w := dev.Warnings(); len(w) != 1 || !strings.Contains(w[0].Message, "state.joined") {
		t.Fatalf("Dev warning: %v", w)
	}
}

func TestShapeMigration(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{})
	c := &compiled{meta: &meta{state: map[string]any{"count": 0, "name": "", "added": true, "list": []any{}}}}
	inst := &Instance{Name: "X", Keys: []string{"count", "name", "removed"}, State: map[string]any{
		"count": "7", "name": int64(5), "removed": "x", "extra": "kept", "list": "not a list",
	}}
	e.migrateState(inst, c)
	want := map[string]any{"count": int64(7), "name": "5", "added": true, "extra": "kept", "list": "not a list"}
	if fmt.Sprint(inst.State) != fmt.Sprint(want) {
		t.Fatalf("migrated: %v\nwant %v", inst.State, want)
	}
	if strings.Join(inst.Keys, ",") != "added,count,list,name" {
		t.Fatalf("keys: %v", inst.Keys)
	}
	reset := &Instance{Keys: []string{"count"}, State: map[string]any{"count": "abc"}}
	e.migrateState(reset, c)
	if reset.State["count"] != 0 {
		t.Fatalf("an uncoercible value resets to the default: %v", reset.State)
	}
}

// BV: store Scope.Ctx() in newInstance (request values included) and this
// fails.
func TestActionSeesCurrentUser(t *testing.T) {
	t.Parallel()
	sealed := sealedStore(t, sealedOpts{})
	e := v5Engine(t, Options{States: sealed, ActionRequest: func(r *http.Request) []RenderOption {
		return []RenderOption{Provide("user", r.Header.Get("X-User"))}
	}})
	page, h, tok := fillPage(t, e, Provide("user", "Sara"))
	if !strings.Contains(page, "<i>Sara</i>") {
		t.Fatal(page)
	}
	inst, ok, _ := sealed.Get(WithStateScope(context.Background(), StateScope{Store: "default"}), strings.TrimPrefix(h, "default."))
	if !ok {
		t.Fatal("blob does not open")
	}
	if _, stored := inst.Ctx["user"]; stored {
		t.Fatalf("request-scoped .Ctx must never be stored: %v", inst.Ctx)
	}
	rec := postAction(e, h, tok, "increment", map[string]string{"X-User": "Omar"})
	if !strings.Contains(rec.Body.String(), "<i>Omar</i>") {
		t.Fatalf("the action sees the user signed in now: %s", rec.Body)
	}
}

// BV: skip the session comparison in action and this fails.
func TestSessionBinding(t *testing.T) {
	t.Parallel()
	for name, store := range map[string]StateStore{"memory": NewMemoryStates(0, 0), "sealed": sealedStore(t, sealedOpts{})} {
		e := v5Engine(t, Options{States: store, Sessions: SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get("X-Session"), nil })})
		_, h, tok := fillPage(t, e, WithSession("sess-1"))
		if rec := postAction(e, h, tok, "increment", map[string]string{"X-Session": "sess-2"}); rec.Code != http.StatusGone {
			t.Errorf("%s: another session must find it gone: %d", name, rec.Code)
		}
		if rec := postAction(e, h, tok, "increment", nil); rec.Code != http.StatusGone {
			t.Errorf("%s: signed out must find it gone: %d", name, rec.Code)
		}
		if rec := postAction(e, h, tok, "increment", map[string]string{"X-Session": "sess-1"}); rec.Code != 200 {
			t.Errorf("%s: the same session works: %d %s", name, rec.Code, rec.Body)
		}
	}
	// RevokeSession ends the memory store's instances for that session.
	mem := NewMemoryStates(0, 0)
	e := v5Engine(t, Options{States: mem, Sessions: SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get("X-Session"), nil })})
	_, h, tok := fillPage(t, e, WithSession("sess-1"))
	if err := e.RevokeSession(context.Background(), "sess-1"); err != nil {
		t.Fatal(err)
	}
	if rec := postAction(e, h, tok, "increment", map[string]string{"X-Session": "sess-1"}); rec.Code != http.StatusGone {
		t.Fatalf("revoked: %d", rec.Code)
	}
}

// refuse is a middleware that checks what it sees and returns err
// without calling next (or calls next when err is nil); seen counts calls
// that came back out of the chain.
func refuse(err error, seen *[]string) ActionMiddleware {
	return func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, a *ActionCall) (*ActionResult, error) {
			if a.Request == nil || a.Component != "Counter" || a.Store != DefaultStoreName {
				return nil, errors.New("middleware must see the live request, component and store")
			}
			res, rerr := func() (*ActionResult, error) {
				if err != nil {
					return nil, err
				}
				return next(ctx, a)
			}()
			*seen = append(*seen, fmt.Sprintf("%s:%v", a.Action, rerr != nil))
			return res, rerr
		}
	}
}

// BV: run the core before the chain and this fails (state changes).
func TestActionMiddlewareRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		code int
	}{
		{&ActionError{Status: http.StatusTooManyRequests, Message: "slow down", RetryAfter: 1500 * time.Millisecond}, 429},
		{ErrForbidden, 403},
		{ErrGone, 410},
		{nil, 200},
	} {
		var seen []string
		mem := NewMemoryStates(0, 0)
		e := v5Engine(t, Options{States: mem, ActionMiddleware: []ActionMiddleware{refuse(tc.err, &seen)}})
		_, h, tok := fillPage(t, e)
		rec := postAction(e, h, tok, "increment", nil)
		if rec.Code != tc.code {
			t.Errorf("%v: %d %s", tc.err, rec.Code, rec.Body)
		}
		if tc.code == 429 && rec.Header().Get("Retry-After") != "2" {
			t.Errorf("Retry-After rounds up: %q", rec.Header().Get("Retry-After"))
		}
		inst, _, _ := mem.Get(context.Background(), strings.TrimPrefix(h, "default."))
		n, _ := toInt(inst.State["count"])
		if tc.err != nil && n != 0 {
			t.Errorf("%v: a refused action must not run (count %d)", tc.err, n)
		}
		if tc.err == nil && n != 2 {
			t.Errorf("an allowed action runs (count %d)", n)
		}
		if len(seen) != 1 {
			t.Errorf("the middleware wraps the call once: %v", seen)
		}
	}
}

// Middleware runs outermost first and may rewrite the call.
func TestActionMiddlewareOrder(t *testing.T) {
	t.Parallel()
	var order []string
	mark := func(name string) ActionMiddleware {
		return func(next ActionFunc) ActionFunc {
			return func(ctx context.Context, a *ActionCall) (*ActionResult, error) {
				order = append(order, name+">")
				res, err := next(ctx, a)
				order = append(order, "<"+name)
				return res, err
			}
		}
	}
	rewrite := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, a *ActionCall) (*ActionResult, error) {
			a.Action, a.Args = "bump", []any{int64(5)}
			return next(ctx, a)
		}
	}
	e := v5Engine(t, Options{States: NewMemoryStates(0, 0), ActionMiddleware: []ActionMiddleware{mark("a"), mark("b"), rewrite}})
	_, h, tok := fillPage(t, e)
	rec := postAction(e, h, tok, "increment", nil)
	if strings.Join(order, " ") != "a> b> <b <a" || !strings.Contains(rec.Body.String(), "<span>5</span>") {
		t.Fatalf("order %v, body %s", order, rec.Body)
	}
	res, err := e.Action(context.Background(), ActionRequest{Handle: h, Token: tok, Action: "$refresh"})
	if err != nil || !strings.Contains(string(res), "<span>") {
		t.Fatalf("direct Action: %v", err)
	}
}

func TestToggle(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
		"components/T.html", "---\nstate:\n  open: false\n  n: 0\n---\n<div><b>{{ .State.open }}</b><button brick:click='$toggle(\"open\")'>t</button></div>",
		"content/a.html", `<T />`,
	)})
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Fill(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	m := stateRE.FindStringSubmatch(string(b))
	body := func(args string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(fmt.Sprintf(`{"handle":%q,"token":%q,"action":"$toggle","args":%s}`, m[1], m[2], args)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Brick-Action", "1")
		rec := httptest.NewRecorder()
		e.ActionHandler().ServeHTTP(rec, req)
		return rec
	}
	if rec := body(`["open"]`); rec.Code != 200 || !strings.Contains(rec.Body.String(), "<b>true</b>") {
		t.Fatalf("toggle: %d %s", rec.Code, rec.Body)
	}
	if rec := body(`["open"]`); !strings.Contains(rec.Body.String(), "<b>false</b>") {
		t.Fatalf("toggle back: %s", rec.Body)
	}
	for _, bad := range []string{`["n"]`, `["nope"]`, `[]`, `[1]`} {
		if rec := body(bad); rec.Code != 400 {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
}

type downStore struct{ StateStore }

func (downStore) Get(context.Context, string) (*Instance, bool, error) {
	return nil, false, errors.New("dial tcp: connection refused")
}

// BV: map a Get error to ErrGone and this fails.
func TestStoreDownIs503(t *testing.T) {
	t.Parallel()
	mem := NewMemoryStates(0, 0)
	e := v5Engine(t, Options{States: mem})
	_, h, tok := fillPage(t, e)
	down := v5Engine(t, Options{States: downStore{mem}})
	rec := postAction(down, h, tok, "increment", nil)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("store outage: %d %q %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body)
	}
	if strings.Contains(rec.Body.String(), "connection refused") {
		t.Fatal("Prod must not leak the store's error text")
	}
}

func TestResultHasState(t *testing.T) {
	t.Parallel()
	e := v5Engine(t, Options{States: NewMemoryStates(0, 0)})
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.HasState() {
		t.Fatal("a page with a stateful island has state")
	}
}

// The root module names no vendor: Redis,
// SQL drivers, KMS SDKs and OpenTelemetry live in contrib modules. Its one
// dependency is the YAML parser.
func TestRootNamesNoVendor(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"go.yaml.in/yaml/v3": true}
	inRequire := false
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 0:
		case f[0] == "require" && len(f) > 1 && f[1] == "(":
			inRequire = true
		case inRequire && f[0] == ")":
			inRequire = false
		case f[0] == "require" && len(f) >= 2:
			if !allowed[f[1]] {
				t.Errorf("root module requires %s; vendor pieces belong in contrib/", f[1])
			}
		case inRequire:
			if !allowed[f[0]] {
				t.Errorf("root module requires %s; vendor pieces belong in contrib/", f[0])
			}
		}
	}
}
