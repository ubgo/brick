package brick

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// listenRE reads a listening root's topics and token.
var listenRE = regexp.MustCompile(`data-brick-listen="([^"]+)" data-brick-listen-token="([^"]+)"`)

const listenComponent = "---\nstate: {n: 0}\nprops: {id: {type: int}}\nbrick: {listen: [orders, \"order:{{ .Props.id }}\"]}\n---\n<div>{{ .State.n }}</div>"

func newPush(t *testing.T, o PushOptions) *Push {
	t.Helper()
	p, err := NewPush(o)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// listenPage renders a page with one listening component and returns its
// topics and token.
func listenPage(t *testing.T, e *Engine, opts ...RenderOption) (string, string) {
	t.Helper()
	page, err := e.Render(context.Background(), "content/a.html", nil, opts...)
	if err != nil {
		t.Fatal(err)
	}
	m := listenRE.FindStringSubmatch(string(page))
	if m == nil {
		t.Fatalf("no listen attrs: %s", page)
	}
	return m[1], m[2]
}

func pushEngine(t *testing.T, o Options) *Engine {
	t.Helper()
	if o.States == nil {
		o.States = NewMemoryStates(0, 0)
	}
	o.FS = theme("components/Orders.html", listenComponent, "content/a.html", `<Orders id="42" />`)
	return newEngine(t, o)
}

func TestListenCompileChecks(t *testing.T) {
	t.Parallel()
	push := newPush(t, PushOptions{})
	cases := []struct {
		name, src string
		push      *Push
		want      string
	}{
		{"no push", listenComponent, nil, "server push is off"},
		{"no state", "---\nbrick: {listen: orders}\n---\nx", push, "needs state:"},
		{"bad topic", "---\nstate: {n: 0}\nbrick: {listen: \"has space\"}\n---\nx", push, "not a topic"},
		{"bad shape", "---\nstate: {n: 0}\nbrick: {listen: 3}\n---\nx", push, "must be a topic"},
		{"unknown key", "---\nstate: {n: 0}\nbrick: {listen: {topics: [a], keepalive: true}}\n---\nx", push, "unknown key"},
		{"bad template", "---\nstate: {n: 0}\nbrick: {listen: \"o:{{ .Props.id \"}\n---\nx", push, "listen"},
	}
	for _, c := range cases {
		e := newEngine(t, Options{States: NewMemoryStates(0, 0), Push: c.push, FS: theme("components/X.html", c.src, "content/a.html", `<X id="1" />`)})
		_, err := e.Render(context.Background(), "content/a.html", nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	// A templated topic that renders an invalid name fails the render.
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Push: push, FS: theme(
		"components/X.html", "---\nstate: {n: 0}\nprops: {id: {type: string}}\nbrick: {listen: \"o:{{ .Props.id }}\"}\n---\n<div>x</div>",
		"content/a.html", `<X id="a b" />`)})
	if _, err := e.Render(context.Background(), "content/a.html", nil); err == nil || !strings.Contains(err.Error(), "not a topic") {
		t.Errorf("rendered invalid topic: %v", err)
	}
}

func TestListenAttrsAndToken(t *testing.T) {
	t.Parallel()
	p := newPush(t, PushOptions{})
	e := pushEngine(t, Options{Push: p})
	topics, tok := listenPage(t, e, WithSession("alice"))
	if topics != "orders order:42" {
		t.Fatalf("topics %q", topics)
	}
	got, err := p.openListenToken(context.Background(), tok, hashSession("alice"))
	if err != nil || strings.Join(got.Topics, " ") != "orders order:42" {
		t.Fatalf("token opens to %v, %v", got, err)
	}
	// Another session, a tampered token and a token from another key are
	// refused.
	if _, err := p.openListenToken(context.Background(), tok, hashSession("bob")); !errors.Is(err, ErrForbidden) {
		t.Errorf("other session: %v", err)
	}
	if _, err := p.openListenToken(context.Background(), tok[:len(tok)-2]+"AA", hashSession("alice")); !errors.Is(err, ErrForbidden) {
		t.Errorf("tampered: %v", err)
	}
	if _, err := newPush(t, PushOptions{}).openListenToken(context.Background(), tok, hashSession("alice")); !errors.Is(err, ErrForbidden) {
		t.Errorf("other key: %v", err)
	}
	// keep_alive marks the root.
	e2 := newEngine(t, Options{States: NewMemoryStates(0, 0), Push: p, FS: theme(
		"components/X.html", "---\nstate: {n: 0}\nbrick: {listen: {topics: chat, keep_alive: true}}\n---\n<div>x</div>", "content/a.html", `<X />`)})
	out, err := e2.Render(context.Background(), "content/a.html", nil)
	if err != nil || !strings.Contains(string(out), "data-brick-listen-keep") {
		t.Fatalf("keep_alive: %s %v", out, err)
	}
}

func TestListenTokenExpires(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	p := newPush(t, PushOptions{TokenTTL: time.Minute, Clock: func() time.Time { return now }})
	tok, err := p.listenToken(context.Background(), []string{"a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := p.openListenToken(context.Background(), tok, ""); !errors.Is(err, ErrGone) {
		t.Fatalf("expired token: %v", err)
	}
}

// sseClient opens the events endpoint and reads its lines.
type sseClient struct {
	resp  *http.Response
	lines chan string
}

func openEvents(t *testing.T, srv *httptest.Server, tokens ...string) (*sseClient, int) {
	t.Helper()
	q := url.Values{}
	for _, tok := range tokens {
		q.Add("l", tok)
	}
	resp, err := http.Get(srv.URL + "/_brick/events?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, resp.StatusCode
	}
	c := &sseClient{resp: resp, lines: make(chan string, 64)}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			c.lines <- sc.Text()
		}
		close(c.lines)
	}()
	t.Cleanup(func() { resp.Body.Close() })
	return c, resp.StatusCode
}

// next returns the data of the next refresh event.
func (c *sseClient) next(t *testing.T) string {
	t.Helper()
	timeout := time.After(5 * time.Second)
	event := ""
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				t.Fatal("stream closed")
			}
			if l == "event: "+EventRefresh {
				event = l
			}
			if d, ok := strings.CutPrefix(l, "data: "); ok && event != "" {
				return d
			}
		case <-timeout:
			t.Fatal("no event")
		}
	}
}

// waitFor polls cond for up to 5 seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 500 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func eventsServer(e *Engine) *httptest.Server {
	mux := http.NewServeMux()
	mux.Handle("/_brick/events", e.EventsHandler())
	mux.Handle("/_brick/action", e.ActionHandler())
	return httptest.NewServer(mux)
}

func TestEventsEndToEnd(t *testing.T) {
	t.Parallel()
	b := NewMemoryBroadcaster()
	p := newPush(t, PushOptions{Broadcaster: b})
	e := pushEngine(t, Options{Push: p})
	srv := eventsServer(e)
	defer srv.Close()
	_, tok := listenPage(t, e)
	c, code := openEvents(t, srv, tok)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if ct := c.resp.Header.Get("Content-Type"); ct != EventsContentType {
		t.Fatalf("content type %q", ct)
	}
	waitFor(t, "the subscription", func() bool { return b.subscribers() == 1 })
	ctx := context.Background()
	// A topic the page did not list is not delivered; the listed one is.
	if err := e.Publish(ctx, "order:7"); err != nil {
		t.Fatal(err)
	}
	if err := e.Publish(ctx, "order:42"); err != nil {
		t.Fatal(err)
	}
	if got := c.next(t); got != "order:42" {
		t.Fatalf("event %q", got)
	}
	if err := e.Publish(ctx, "bad topic"); err == nil {
		t.Error("an invalid topic published")
	}
	// Closing the client ends the stream and its subscription.
	c.resp.Body.Close()
	waitFor(t, "cleanup", func() bool { return b.subscribers() == 0 && p.Connections() == 0 })
}

// The events endpoint refuses, with nothing subscribed: no token, a forged
// token, another session's token, a topic AuthorizeTopic denies, and one
// stream over MaxConnections.
func TestEventsRefusals(t *testing.T) {
	t.Parallel()
	b := NewMemoryBroadcaster()
	sessions := SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get("X-User"), nil })
	p := newPush(t, PushOptions{Broadcaster: b, MaxConnections: 1, AuthorizeTopic: func(r *http.Request, topic string) bool {
		return r.URL.Query().Get("deny") != topic
	}})
	e := pushEngine(t, Options{Push: p, Sessions: sessions})
	srv := eventsServer(e)
	defer srv.Close()
	_, tok := listenPage(t, e, WithSession("alice"))
	get := func(query, user string) int {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/_brick/events?"+query, nil)
		req.Header.Set("X-User", user)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, c := range []struct {
		name, query, user string
		want              int
	}{
		{"no token", "", "alice", http.StatusBadRequest},
		{"forged", "l=AAAA", "alice", http.StatusForbidden},
		{"other session", "l=" + url.QueryEscape(tok), "bob", http.StatusForbidden},
		{"no session", "l=" + url.QueryEscape(tok), "", http.StatusForbidden},
		{"denied topic", "l=" + url.QueryEscape(tok) + "&deny=order:42", "alice", http.StatusForbidden},
	} {
		if got := get(c.query, c.user); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
	if b.subscribers() != 0 || p.Connections() != 0 {
		t.Fatalf("a refused request subscribed: %d subscribers, %d connections", b.subscribers(), p.Connections())
	}
	// POST is not a stream.
	resp, err := http.Post(srv.URL+"/_brick/events?l="+url.QueryEscape(tok), "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", resp.StatusCode)
	}
	// The connection cap: the first stream opens, the second is 503.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/_brick/events?l="+url.QueryEscape(tok), nil)
	req.Header.Set("X-User", "alice")
	first, err := http.DefaultClient.Do(req)
	if err != nil || first.StatusCode != http.StatusOK {
		t.Fatalf("first stream: %v %v", first, err)
	}
	defer first.Body.Close()
	waitFor(t, "the first stream", func() bool { return p.Connections() == 1 })
	if got := get("l="+url.QueryEscape(tok), "alice"); got != http.StatusServiceUnavailable {
		t.Errorf("over the cap: %d", got)
	}
}

// An action publishes what it recorded only once it succeeded; a failed
// action publishes nothing.
func TestActionPublishesAfterSuccess(t *testing.T) {
	t.Parallel()
	b := NewMemoryBroadcaster()
	p := newPush(t, PushOptions{Broadcaster: b})
	var fail atomic.Bool
	mw := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, call *ActionCall) (*ActionResult, error) {
			if err := EffectsFrom(ctx).Publish("orders"); err != nil {
				return nil, err
			}
			if err := EffectsFrom(ctx).Publish("orders"); err != nil { // twice: published once
				return nil, err
			}
			if fail.Load() {
				return nil, errors.New("boom")
			}
			return next(ctx, call)
		}
	}
	e := pushEngine(t, Options{Push: p, ActionMiddleware: []ActionMiddleware{mw}})
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := stateRE.FindStringSubmatch(string(page))
	ch, cancel, err := b.Subscribe(context.Background(), []string{"orders"})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	fail.Store(true)
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: m[1], Token: m[2], Action: "$refresh"}); err == nil {
		t.Fatal("the failing action succeeded")
	}
	select {
	case got := <-ch:
		t.Fatalf("a failed action published %q", got)
	case <-time.After(50 * time.Millisecond):
	}
	fail.Store(false)
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: m[1], Token: m[2], Action: "$refresh"}); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got != "orders" {
		t.Fatalf("published %q", got)
	}
	select {
	case got := <-ch:
		t.Fatalf("published twice: %q", got)
	case <-time.After(50 * time.Millisecond):
	}
	// Without Push, Publish is an error, never silent.
	if err := newEngine(t, Options{}).Publish(context.Background(), "orders"); !errors.Is(err, ErrPushOff) {
		t.Errorf("no push: %v", err)
	}
}

// Subscriptions end with their context, and streams with their clients:
// no goroutine outlives either.
func TestPushNoGoroutineLeak(t *testing.T) {
	b := NewMemoryBroadcaster()
	p := newPush(t, PushOptions{Broadcaster: b, Heartbeat: 10 * time.Millisecond})
	e := pushEngine(t, Options{Push: p})
	srv := eventsServer(e)
	_, tok := listenPage(t, e)
	before := runtime.NumGoroutine()
	for range 20 {
		c, code := openEvents(t, srv, tok)
		if code != http.StatusOK {
			t.Fatalf("status %d", code)
		}
		c.resp.Body.Close()
	}
	for range 20 {
		ctx, cancel := context.WithCancel(context.Background())
		if _, _, err := b.Subscribe(ctx, []string{"x"}); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
	waitFor(t, "every subscription to end", func() bool { return b.subscribers() == 0 && p.Connections() == 0 })
	srv.CloseClientConnections()
	srv.Close()
	waitFor(t, "goroutines to settle", func() bool { return runtime.NumGoroutine() <= before+2 })
}

// cancel closes the channel once, however often it is called, and a later
// Publish reaches nobody without failing.
func TestMemoryBroadcasterCancelIdempotent(t *testing.T) {
	t.Parallel()
	b := NewMemoryBroadcaster()
	ch, cancel, err := b.Subscribe(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	_ = b.Publish(context.Background(), "a")
	if got := <-ch; got != "a" {
		t.Fatal(got)
	}
	cancel()
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel open after cancel")
	}
	if err := b.Publish(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
}

// A token that opens but lists a name that is not a topic is refused: the
// endpoint subscribes only to valid topics, whoever sealed the token.
func TestListenTokenInvalidTopicRefused(t *testing.T) {
	t.Parallel()
	p := newPush(t, PushOptions{})
	b, err := json.Marshal(listenClaims{Topics: []string{"ok", "bad topic*"}, Expires: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := p.opts.Sealer.Seal(context.Background(), b, []byte(listenAD))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.openListenToken(context.Background(), base64.RawURLEncoding.EncodeToString(sealed), ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("invalid topic in a sealed token: %v", err)
	}
	// The same payload sealed for another purpose (another additional
	// data, as a state blob is) does not open as a listen token.
	other, err := p.opts.Sealer.Seal(context.Background(), b, []byte("brick:state"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.openListenToken(context.Background(), base64.RawURLEncoding.EncodeToString(other), ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a blob sealed with other data opened: %v", err)
	}
}

// drain reads the stream until the server ends it and returns every line.
func (c *sseClient) drain(t *testing.T) []string {
	t.Helper()
	timeout := time.After(5 * time.Second)
	var lines []string
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				return lines
			}
			lines = append(lines, l)
		case <-timeout:
			t.Fatalf("the stream did not end; read %q", lines)
		}
	}
}

// openEventsAs opens the events endpoint for the user in X-User.
func openEventsAs(t *testing.T, srv *httptest.Server, user, tok string) (*sseClient, int) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/_brick/events?l="+url.QueryEscape(tok), nil)
	req.Header.Set("X-User", user)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, resp.StatusCode
	}
	c := &sseClient{resp: resp, lines: make(chan string, 64)}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			c.lines <- sc.Text()
		}
		close(c.lines)
	}()
	t.Cleanup(func() { resp.Body.Close() })
	return c, resp.StatusCode
}

// An open stream ends when its token expires, after one refresh per topic
// (so the page re-renders its listeners and holds fresh tokens), and the
// same URL is then refused 410: a stale tab stops hearing topics.
func TestEventsEndAtTokenExpiry(t *testing.T) {
	t.Parallel()
	var now atomic.Int64
	now.Store(time.Unix(1_000_000, 0).UnixNano())
	clock := func() time.Time { return time.Unix(0, now.Load()) }
	b := NewMemoryBroadcaster()
	p := newPush(t, PushOptions{Broadcaster: b, TokenTTL: time.Second, Clock: clock})
	e := pushEngine(t, Options{Push: p})
	srv := eventsServer(e)
	// A cleanup, not a defer: it runs after the clients' cleanups close
	// their streams, so a failing test ends instead of waiting in Close.
	t.Cleanup(srv.Close)
	_, tok := listenPage(t, e) // expires at 1_000_001, valid through that second
	// 100ms before the token's last instant.
	now.Store(time.Unix(1_000_001, 900*int64(time.Millisecond)).UnixNano())
	start := time.Now()
	c, code := openEvents(t, srv, tok)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	lines := strings.Join(c.drain(t), "\n")
	if time.Since(start) > 3*time.Second {
		t.Fatalf("the stream outlived its token by %v", time.Since(start))
	}
	for _, want := range []string{"event: refresh\ndata: order:42", "event: refresh\ndata: orders", ": expired"} {
		if !strings.Contains(lines, want) {
			t.Errorf("missing %q in %q", want, lines)
		}
	}
	waitFor(t, "cleanup", func() bool { return b.subscribers() == 0 && p.Connections() == 0 })
	now.Store(time.Unix(1_000_002, 0).UnixNano())
	if _, code := openEvents(t, srv, tok); code != http.StatusGone {
		t.Fatalf("reconnect with the expired token: %d", code)
	}
}

// The recheck ends a stream whose session is no longer answered (signed
// out or revoked in the session store), whose session check fails, or
// whose topic is no longer authorized; a stream still allowed stays open.
func TestEventsRecheckEndsStream(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	live := map[string]bool{"alice": true, "carol": true, "dave": true}
	var denyOrders atomic.Bool
	sessions := SessionFunc(func(r *http.Request) (string, error) {
		u := r.Header.Get("X-User")
		mu.Lock()
		defer mu.Unlock()
		if u == "dave" && !live[u] {
			return "", errors.New("session store down")
		}
		if live[u] {
			return u, nil
		}
		return "", nil
	})
	b := NewMemoryBroadcaster()
	p := newPush(t, PushOptions{Broadcaster: b, Recheck: 20 * time.Millisecond, AuthorizeTopic: func(r *http.Request, topic string) bool {
		return !(denyOrders.Load() && r.Header.Get("X-User") == "carol" && topic == "orders")
	}})
	e := pushEngine(t, Options{Push: p, Sessions: sessions})
	srv := eventsServer(e)
	// A cleanup, not a defer: it runs after the clients' cleanups close
	// their streams, so a failing test ends instead of waiting in Close.
	t.Cleanup(srv.Close)
	open := func(user string) *sseClient {
		_, tok := listenPage(t, e, WithSession(user))
		c, code := openEventsAs(t, srv, user, tok)
		if code != http.StatusOK {
			t.Fatalf("%s: status %d", user, code)
		}
		return c
	}
	alice, carol, dave := open("alice"), open("carol"), open("dave")
	waitFor(t, "three streams", func() bool { return p.Connections() == 3 })
	time.Sleep(80 * time.Millisecond) // several rechecks pass
	if err := e.Publish(context.Background(), "orders"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*sseClient{alice, carol, dave} {
		if got := c.next(t); got != "orders" {
			t.Fatalf("event %q", got)
		}
	}
	mu.Lock()
	live["alice"] = false // signed out
	live["dave"] = false  // the store errors for dave
	mu.Unlock()
	denyOrders.Store(true) // carol lost access to orders
	for name, c := range map[string]*sseClient{"alice": alice, "carol": carol, "dave": dave} {
		if lines := c.drain(t); !slices.Contains(lines, ": revoked") {
			t.Errorf("%s: %q", name, lines)
		}
	}
	waitFor(t, "cleanup", func() bool { return b.subscribers() == 0 && p.Connections() == 0 })
}

// RevokeSession (Engine's and Push's) ends that session's streams at
// once, and only them.
func TestRevokeSessionEndsStreams(t *testing.T) {
	t.Parallel()
	sessions := SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get("X-User"), nil })
	b := NewMemoryBroadcaster()
	p := newPush(t, PushOptions{Broadcaster: b})
	e := pushEngine(t, Options{Push: p, Sessions: sessions})
	srv := eventsServer(e)
	// A cleanup, not a defer: it runs after the clients' cleanups close
	// their streams, so a failing test ends instead of waiting in Close.
	t.Cleanup(srv.Close)
	_, ta := listenPage(t, e, WithSession("alice"))
	_, tb := listenPage(t, e, WithSession("bob"))
	alice, _ := openEventsAs(t, srv, "alice", ta)
	bob, _ := openEventsAs(t, srv, "bob", tb)
	if alice == nil || bob == nil {
		t.Fatal("a stream did not open")
	}
	// Each stream holds its topic subscription and its revoke subscription.
	waitFor(t, "four subscriptions", func() bool { return b.subscribers() == 4 })
	ctx := context.Background()
	// Through Engine.RevokeSession, the one sign-out call that also ends
	// the session's state instances.
	if err := e.RevokeSession(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if lines := alice.drain(t); !slices.Contains(lines, ": revoked") {
		t.Fatalf("alice: %q", lines)
	}
	if err := e.Publish(ctx, "orders"); err != nil {
		t.Fatal(err)
	}
	if got := bob.next(t); got != "orders" {
		t.Fatalf("bob: %q", got)
	}
	if err := p.RevokeSession(ctx, ""); err != nil {
		t.Fatalf("empty session: %v", err)
	}
	waitFor(t, "alice's subscriptions to end", func() bool { return b.subscribers() == 2 && p.Connections() == 1 })
}

// "brick:" topics are the engine's: a component cannot listen to one and
// nothing can publish one, while a Broadcaster still carries them.
func TestEngineTopicsReserved(t *testing.T) {
	t.Parallel()
	ctl := revokeTopic(hashSession("alice"))
	if ValidTopic(ctl) || ValidTopic("brick:orders") {
		t.Error("ValidTopic accepted a brick: topic")
	}
	if !ValidBroadcastTopic(ctl) || !ValidBroadcastTopic("orders") || ValidBroadcastTopic("bad topic") {
		t.Error("ValidBroadcastTopic")
	}
	p := newPush(t, PushOptions{})
	if err := p.Publish(context.Background(), ctl); err == nil {
		t.Error("published a control topic")
	}
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Push: p, FS: theme(
		"components/X.html", "---\nstate: {n: 0}\nbrick: {listen: \"brick:revoke:x\"}\n---\n<div>x</div>", "content/a.html", `<X />`)})
	if _, err := e.Render(context.Background(), "content/a.html", nil); err == nil {
		t.Error("a component listened to a brick: topic")
	}
}

// Effects.Publish names the engine's reserved prefix when an action tries
// a "brick:" topic, instead of calling it malformed: the name is well
// formed, it is just not the action's to send.
func TestEffectsPublishReservedTopic(t *testing.T) {
	t.Parallel()
	e := pushEngine(t, Options{Push: newPush(t, PushOptions{Broadcaster: NewMemoryBroadcaster()})})
	err := (&Effects{eng: e}).Publish("brick:revoke")
	if err == nil || !strings.Contains(err.Error(), "engine's own") {
		t.Fatalf("got %v, want the reserved-prefix error", err)
	}
	if err := (&Effects{eng: e}).Publish("bad topic"); err == nil || !strings.Contains(err.Error(), "is not a topic") {
		t.Fatalf("got %v, want the malformed-topic error", err)
	}
}
