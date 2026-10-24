package pushredis

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ubgo/brick"
)

const presenceRoom = "---\nstate: {n: 0}\nbrick: {listen: {presence: room:7}}\n---\n" +
	`<ul>{{ $p := .Presence "room:7" }}total={{ $p.Total }}{{ range $p.Entries }}<li>{{ .ID }}={{ index .Data "name" }}</li>{{ end }}</ul>`

// stream is one open event stream's lines.
type stream struct {
	resp  *http.Response
	lines chan string
}

func openStream(t *testing.T, srvURL, user, tok string) *stream {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srvURL+"?l="+url.QueryEscape(tok), nil)
	req.Header.Set("X-User", user)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: status %d", user, resp.StatusCode)
	}
	s := &stream{resp: resp, lines: make(chan string, 64)}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		close(s.lines)
	}()
	t.Cleanup(func() { resp.Body.Close() })
	return s
}

// waitData waits for a refresh event whose data is want.
func (s *stream) waitData(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(waitEvent)
	for {
		select {
		case l, ok := <-s.lines:
			if !ok {
				t.Fatal("stream closed")
			}
			if d, ok := strings.CutPrefix(l, "data: "); ok && d == want {
				return
			}
		case <-deadline:
			t.Fatalf("no %q event", want)
		}
	}
}

// Presence across two servers: a viewer whose stream is on server B is
// listed by a render on server A, a join on A refreshes the viewer on B,
// and a leave on A does too.
func TestIntegrationPresenceAcrossServers(t *testing.T) {
	c, prefix := integrationClient(t)
	ring, err := brick.StaticKeys(make([]byte, brick.StateKeySize))
	if err != nil {
		t.Fatal(err)
	}
	sealer := brick.NewAESGCMSealer(ring, nil)
	fsys := fstest.MapFS{
		"components/Room.html": {Data: []byte(presenceRoom)},
		"content/a.html":       {Data: []byte(`<Room />`)},
	}
	sessions := brick.SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get("X-User"), nil })
	identify := func(r *http.Request, topic string) (brick.PresenceEntry, bool) {
		u := r.Header.Get("X-User")
		return brick.PresenceEntry{ID: u, Data: map[string]string{"name": strings.ToUpper(u)}}, true
	}
	store := NewPresenceStore(c, Options{Prefix: prefix})
	t.Cleanup(func() { _ = c.Del(context.Background(), store.Key("room:7")).Err() })
	server := func() (*brick.Engine, *httptest.Server) {
		push, err := brick.NewPush(brick.PushOptions{Broadcaster: newB(t, c, prefix), Sealer: sealer,
			Presence: brick.PresenceOptions{Store: store, Identify: identify}})
		if err != nil {
			t.Fatal(err)
		}
		e, err := brick.New(brick.Options{FS: fsys, States: brick.NewMemoryStates(0, 0), Push: push, Sessions: sessions})
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(e.EventsHandler())
		t.Cleanup(srv.Close)
		return e, srv
	}
	a, srvA := server()
	b, srvB := server()
	render := func(e *brick.Engine, user string) (string, string) {
		page, err := e.Render(context.Background(), "content/a.html", nil, brick.WithSession(user))
		if err != nil {
			t.Fatal(err)
		}
		m := listenRE.FindStringSubmatch(string(page))
		if m == nil {
			t.Fatalf("no listen token: %s", page)
		}
		return string(page), m[1]
	}
	const control = "brick:presence:room:7"

	_, tokAlice := render(a, "alice")
	alice := openStream(t, srvB.URL, "alice", tokAlice) // rendered on A, streaming on B
	alice.waitData(t, control)
	if page, _ := render(a, "carol"); !strings.Contains(page, "total=1<li>alice=ALICE</li>") {
		t.Fatalf("server A does not list alice on B: %s", page)
	}
	_, tokBob := render(b, "bob")
	bob := openStream(t, srvA.URL, "bob", tokBob)
	alice.waitData(t, control) // bob joined on A; alice on B hears it
	if page, _ := render(b, "carol"); !strings.Contains(page, "total=2<li>alice=ALICE</li><li>bob=BOB</li>") {
		t.Fatalf("both servers' viewers: %s", page)
	}
	bob.resp.Body.Close()
	alice.waitData(t, control) // bob left on A
	if page, _ := render(b, "carol"); !strings.Contains(page, "total=1<li>alice=ALICE</li></ul>") {
		t.Fatalf("after bob left: %s", page)
	}
}

// The Redis store's cap is atomic and a lapsed entry frees its place.
func TestIntegrationPresenceStore(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	s := NewPresenceStore(c, Options{Prefix: prefix})
	t.Cleanup(func() { _ = c.Del(ctx, s.Key("t")).Err() })
	now := time.Now()
	s.clock = func() time.Time { return now }
	if err := s.Join(ctx, "t", "c1", brick.PresenceEntry{ID: "a"}, time.Minute, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Join(ctx, "t", "c2", brick.PresenceEntry{ID: "b"}, time.Minute, 1); !errors.Is(err, brick.ErrPresenceFull) {
		t.Fatalf("over max: %v", err)
	}
	if err := s.Join(ctx, "t", "c1", brick.PresenceEntry{ID: "a", Data: map[string]string{"n": "1"}}, time.Minute, 1); err != nil {
		t.Fatalf("renewal at max: %v", err)
	}
	if l, err := s.List(ctx, "t"); err != nil || len(l) != 1 || l[0].Data["n"] != "1" {
		t.Fatalf("list %+v %v", l, err)
	}
	now = now.Add(2 * time.Minute)
	if l, _ := s.List(ctx, "t"); len(l) != 0 {
		t.Fatalf("expired entry listed: %+v", l)
	}
	if err := s.Join(ctx, "t", "c2", brick.PresenceEntry{ID: "b"}, time.Minute, 1); err != nil {
		t.Fatalf("lapsed place not freed: %v", err)
	}
	if err := s.Leave(ctx, "t", "c2"); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.HLen(ctx, s.Key("t")).Result(); n != 0 {
		t.Fatalf("%d fields left", n)
	}
}
