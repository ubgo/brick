package pushredis

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ubgo/brick"
)

// The integration tests run against a real Redis only when
// BRICK_REDIS_TEST=1 (task test:redis), so the default gate needs no
// server. The defaults match the shared local Redis 8. Every run uses a
// fresh channel prefix; pub/sub writes no keys, so nothing needs deleting.
const (
	integrationEnv = "BRICK_REDIS_TEST"
	defaultAddr    = "localhost:7012"
	waitEvent      = 5 * time.Second
)

func integrationClient(t *testing.T) (*redis.Client, string) {
	t.Helper()
	if os.Getenv(integrationEnv) != "1" {
		t.Skipf("set %s=1 to run against a real Redis", integrationEnv)
	}
	addr := os.Getenv("BRICK_REDIS_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	c := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("BRICK_REDIS_PASSWORD")})
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("Redis at %s: %v", addr, err)
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	t.Cleanup(func() { _ = c.Close() })
	return c, "brick-test:" + hex.EncodeToString(b[:]) + ":"
}

func newB(t *testing.T, c *redis.Client, prefix string) *Broadcaster {
	t.Helper()
	b, err := New(context.Background(), c, Options{Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func recv(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(waitEvent):
		t.Fatal("no topic delivered")
		return ""
	}
}

// Two broadcasters on one prefix are two servers: a publish on either
// reaches both; another prefix hears nothing.
func TestIntegrationAcrossServers(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	a, b := newB(t, c, prefix), newB(t, c, prefix)
	other := newB(t, c, prefix+"other:")
	chA, cancelA, err := a.Subscribe(ctx, []string{"orders"})
	if err != nil {
		t.Fatal(err)
	}
	defer cancelA()
	chB, cancelB, err := b.Subscribe(ctx, []string{"orders", "order:42"})
	if err != nil {
		t.Fatal(err)
	}
	defer cancelB()
	chO, cancelO, err := other.Subscribe(ctx, []string{"orders"})
	if err != nil {
		t.Fatal(err)
	}
	defer cancelO()

	if err := a.Publish(ctx, "order:42"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, chB); got != "order:42" {
		t.Fatalf("server B got %q", got)
	}
	if err := b.Publish(ctx, "orders"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, chA); got != "orders" {
		t.Fatalf("server A got %q", got)
	}
	if got := recv(t, chB); got != "orders" {
		t.Fatalf("server B got %q", got)
	}
	select {
	case got := <-chO:
		t.Fatalf("another prefix heard %q", got)
	case <-time.After(200 * time.Millisecond):
	}
	select {
	case got := <-chA:
		t.Fatalf("server A heard %q, which it does not listen to", got)
	case <-time.After(200 * time.Millisecond):
	}
}

var listenRE = regexp.MustCompile(`data-brick-listen-token="([^"]+)"`)

// The whole path across two servers: server A renders the page, the
// browser's stream opens on server B, and a publish on A refreshes it.
// The servers share a Sealer, as a deploy behind a balancer must.
func TestIntegrationEventsAcrossServers(t *testing.T) {
	c, prefix := integrationClient(t)
	ring, err := brick.StaticKeys(make([]byte, brick.StateKeySize))
	if err != nil {
		t.Fatal(err)
	}
	sealer := brick.NewAESGCMSealer(ring, nil)
	fsys := fstest.MapFS{
		"components/Orders.html": {Data: []byte("---\nstate: {n: 0}\nbrick: {listen: orders}\n---\n<div>{{ .State.n }}</div>")},
		"content/a.html":         {Data: []byte(`<Orders />`)},
	}
	engine := func() *brick.Engine {
		push, err := brick.NewPush(brick.PushOptions{Broadcaster: newB(t, c, prefix), Sealer: sealer})
		if err != nil {
			t.Fatal(err)
		}
		e, err := brick.New(brick.Options{FS: fsys, States: brick.NewMemoryStates(0, 0), Push: push})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	a, b := engine(), engine()
	page, err := a.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := listenRE.FindStringSubmatch(string(page))
	if m == nil {
		t.Fatalf("no listen token: %s", page)
	}
	srv := httptest.NewServer(b.EventsHandler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "?l=" + url.QueryEscape(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	// The stream has subscribed once its opening comment arrived.
	for l := range lines {
		if l == ": open" {
			break
		}
	}
	if err := a.Publish(context.Background(), "orders"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(waitEvent)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("stream closed")
			}
			if d, ok := strings.CutPrefix(l, "data: "); ok {
				if d != "orders" {
					t.Fatalf("event %q", d)
				}
				return
			}
		case <-deadline:
			t.Fatal("no event across servers")
		}
	}
}

// A sign-out on server A ends the session's open stream on server B at
// once: RevokeSession rides the same Redis subscription as the topics.
func TestIntegrationRevokeAcrossServers(t *testing.T) {
	c, prefix := integrationClient(t)
	ring, err := brick.StaticKeys(make([]byte, brick.StateKeySize))
	if err != nil {
		t.Fatal(err)
	}
	sealer := brick.NewAESGCMSealer(ring, nil)
	fsys := fstest.MapFS{
		"components/Orders.html": {Data: []byte("---\nstate: {n: 0}\nbrick: {listen: orders}\n---\n<div>{{ .State.n }}</div>")},
		"content/a.html":         {Data: []byte(`<Orders />`)},
	}
	sessions := brick.SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get("X-User"), nil })
	server := func() (*brick.Engine, *brick.Push) {
		push, err := brick.NewPush(brick.PushOptions{Broadcaster: newB(t, c, prefix), Sealer: sealer})
		if err != nil {
			t.Fatal(err)
		}
		e, err := brick.New(brick.Options{FS: fsys, States: brick.NewMemoryStates(0, 0), Push: push, Sessions: sessions})
		if err != nil {
			t.Fatal(err)
		}
		return e, push
	}
	a, pushA := server()
	b, _ := server()
	page, err := a.Render(context.Background(), "content/a.html", nil, brick.WithSession("alice"))
	if err != nil {
		t.Fatal(err)
	}
	m := listenRE.FindStringSubmatch(string(page))
	if m == nil {
		t.Fatalf("no listen token: %s", page)
	}
	srv := httptest.NewServer(b.EventsHandler())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"?l="+url.QueryEscape(m[1]), nil)
	req.Header.Set("X-User", "alice")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	for l := range lines {
		if l == ": open" {
			break
		}
	}
	if err := pushA.RevokeSession(context.Background(), "alice"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(waitEvent)
	revoked := false
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				if !revoked {
					t.Fatal("the stream ended without a revocation")
				}
				return
			}
			if l == ": revoked" {
				revoked = true
			}
		case <-deadline:
			t.Fatal("the stream on server B outlived the revocation on server A")
		}
	}
}
