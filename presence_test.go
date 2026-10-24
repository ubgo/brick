package brick

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// roomComponent shows who is on room:<room>. index reads Data so an entry
// without Data renders "" rather than failing.
const roomComponent = "---\nstate: {n: 0}\nprops: {room: {type: string}}\nbrick: {listen: {presence: \"room:{{ .Props.room }}\"}}\n---\n" +
	`<ul>{{ $p := .Presence (printf "room:%s" .Props.room) }}total={{ $p.Total }}{{ range $p.Entries }}<li>{{ .ID }}={{ index .Data "name" }}x{{ .Conns }}</li>{{ end }}</ul>`

// headerSessions is the session of a request: its X-User header.
var headerSessions = SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get("X-User"), nil })

// identifyByHeader is a host Identify: the viewer is the X-User, shown by a
// display name the host chose.
func identifyByHeader(r *http.Request, topic string) (PresenceEntry, bool) {
	u := r.Header.Get("X-User")
	return PresenceEntry{ID: "u-" + u, Data: map[string]string{"name": strings.ToUpper(u)}}, true
}

func presenceEngine(t *testing.T, p *Push, files ...string) *Engine {
	t.Helper()
	if len(files) == 0 {
		files = []string{"components/Room.html", roomComponent, "content/a.html", `<Room room="7" />`}
	}
	return newEngine(t, Options{States: NewMemoryStates(0, 0), Push: p, Sessions: headerSessions, FS: theme(files...)})
}

func renderAs(t *testing.T, e *Engine, user string) (string, string, string) {
	t.Helper()
	page, err := e.Render(context.Background(), "content/a.html", nil, WithSession(user))
	if err != nil {
		t.Fatal(err)
	}
	m := listenRE.FindStringSubmatch(string(page))
	if m == nil {
		t.Fatalf("no listen attrs: %s", page)
	}
	return string(page), m[1], m[2]
}

// nextControl waits for a refresh event naming want, skipping others.
func (c *sseClient) nextControl(t *testing.T, want string) {
	t.Helper()
	for {
		if got := c.next(t); got == want {
			return
		}
	}
}

// Two viewers: each join and leave refreshes the other, and a render lists
// exactly the open streams, with the host's chosen identity.
func TestPresenceJoinLeave(t *testing.T) {
	t.Parallel()
	p := newPush(t, PushOptions{Presence: PresenceOptions{Identify: identifyByHeader}})
	e := presenceEngine(t, p)
	srv := eventsServer(e)
	t.Cleanup(srv.Close)
	page, topics, tokA := renderAs(t, e, "alice")
	if topics != "brick:presence:room:7" {
		t.Fatalf("data-brick-listen %q", topics)
	}
	if !strings.Contains(page, "total=0") {
		t.Fatalf("nobody is on the room yet: %s", page)
	}
	a, code := openEventsAs(t, srv, "alice", tokA)
	if code != http.StatusOK {
		t.Fatalf("alice: %d", code)
	}
	a.nextControl(t, "brick:presence:room:7") // her own join
	if page, _, _ := renderAs(t, e, "alice"); !strings.Contains(page, "total=1<li>u-alice=ALICEx1</li>") {
		t.Fatalf("after alice joined: %s", page)
	}
	_, _, tokB := renderAs(t, e, "bob")
	b, code := openEventsAs(t, srv, "bob", tokB)
	if code != http.StatusOK {
		t.Fatalf("bob: %d", code)
	}
	a.nextControl(t, "brick:presence:room:7") // bob joined
	if page, _, _ := renderAs(t, e, "alice"); !strings.Contains(page, "total=2<li>u-alice=ALICEx1</li><li>u-bob=BOBx1</li>") {
		t.Fatalf("after bob joined: %s", page)
	}
	// A second tab of alice is one viewer with two connections.
	a2, _ := openEventsAs(t, srv, "alice", tokA)
	a.nextControl(t, "brick:presence:room:7")
	if page, _, _ := renderAs(t, e, "bob"); !strings.Contains(page, "total=2<li>u-alice=ALICEx2</li>") {
		t.Fatalf("two tabs: %s", page)
	}
	a2.resp.Body.Close()
	b.resp.Body.Close()
	waitFor(t, "bob and the second tab leave", func() bool {
		l, _ := p.opts.Presence.Store.List(context.Background(), "room:7")
		return len(l) == 1
	})
	if page, _, _ := renderAs(t, e, "alice"); !strings.Contains(page, "total=1<li>u-alice=ALICEx1</li></ul>") {
		t.Fatalf("after bob left: %s", page)
	}
	a.resp.Body.Close()
	waitFor(t, "everyone leaves", func() bool {
		l, _ := p.opts.Presence.Store.List(context.Background(), "room:7")
		return len(l) == 0 && p.Connections() == 0
	})
}

// Without Identify nothing personal is listed: a random id per
// connection, no Data.
func TestPresenceAnonymousByDefault(t *testing.T) {
	t.Parallel()
	p := newPush(t, PushOptions{})
	e := presenceEngine(t, p)
	srv := eventsServer(e)
	t.Cleanup(srv.Close)
	_, _, tok := renderAs(t, e, "alice")
	a, _ := openEventsAs(t, srv, "alice", tok)
	a.nextControl(t, "brick:presence:room:7")
	l, err := p.opts.Presence.Store.List(context.Background(), "room:7")
	if err != nil || len(l) != 1 {
		t.Fatalf("list %v %v", l, err)
	}
	if l[0].Data != nil || len(l[0].ID) < presenceConnIDBytes || strings.Contains(l[0].ID, "alice") || l[0].ID == hashSession("alice") {
		t.Fatalf("anonymous entry leaks or is weak: %+v", l[0])
	}
	page, _, _ := renderAs(t, e, "bob")
	if strings.Contains(page, "alice") {
		t.Fatalf("the session leaked into the page: %s", page)
	}
}

// A page sees presence only for the topics its own tokens carry: a
// listen-only token neither joins nor hears a presence topic of the same
// name, a presence token for room:7 hears nothing of room:8, a sealed
// presence topic in the engine's namespace is refused, and a component
// cannot read a topic it does not declare.
func TestPresenceScopedToTokens(t *testing.T) {
	t.Parallel()
	b := NewMemoryBroadcaster()
	p := newPush(t, PushOptions{Broadcaster: b, Presence: PresenceOptions{Identify: identifyByHeader}})
	e := presenceEngine(t, p,
		"components/Room.html", roomComponent,
		"components/Listen.html", "---\nstate: {n: 0}\nbrick: {listen: \"room:7\"}\n---\n<div>x</div>",
		"content/a.html", `<Room room="7" />`,
		"content/l.html", `<Listen />`,
		"content/r8.html", `<Room room="8" />`)
	srv := eventsServer(e)
	t.Cleanup(srv.Close)
	ctx := context.Background()

	// A listen-only token for the same name: no join, no presence events.
	lp, err := e.Render(ctx, "content/l.html", nil, WithSession("eve"))
	if err != nil {
		t.Fatal(err)
	}
	m := listenRE.FindStringSubmatch(string(lp))
	if m == nil || m[1] != "room:7" {
		t.Fatalf("listen page %s", lp)
	}
	eve, code := openEventsAs(t, srv, "eve", m[2])
	if code != http.StatusOK {
		t.Fatalf("eve: %d", code)
	}
	waitFor(t, "eve subscribed", func() bool { return b.subscribers() == 2 }) // topic + revoke
	if l, _ := p.opts.Presence.Store.List(ctx, "room:7"); len(l) != 0 {
		t.Fatalf("a listen-only token joined presence: %+v", l)
	}
	// A room:8 viewer: hears neither room:7's joins nor makes eve's
	// stream hear its own.
	p8, err := e.Render(ctx, "content/r8.html", nil, WithSession("mallory"))
	if err != nil {
		t.Fatal(err)
	}
	m8 := listenRE.FindStringSubmatch(string(p8))
	mal, _ := openEventsAs(t, srv, "mallory", m8[2])
	mal.nextControl(t, "brick:presence:room:8")
	_, _, tokA := renderAs(t, e, "alice")
	a, _ := openEventsAs(t, srv, "alice", tokA)
	a.nextControl(t, "brick:presence:room:7")
	if l, _ := p.opts.Presence.Store.List(ctx, "room:7"); len(l) != 1 || l[0].ID != "u-alice" {
		t.Fatalf("room:7 lists %+v", l)
	}
	// Close both streams and read what each heard.
	time.Sleep(100 * time.Millisecond)
	eve.resp.Body.Close()
	mal.resp.Body.Close()
	for _, c := range []*sseClient{eve, mal} {
		for _, l := range c.drainAfterClose() {
			if strings.Contains(l, "brick:presence:room:7") {
				t.Fatalf("a stream heard room:7 presence without a token for it: %q", l)
			}
		}
	}
	// A token sealing an engine-namespace presence topic never opens.
	bad, err := p.listenTokenWith(ctx, nil, []string{"brick:revoke:x"}, hashSession("eve"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.openListenToken(ctx, bad, hashSession("eve")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("engine-namespace presence topic opened: %v", err)
	}
	// .Presence of an undeclared topic is a render error.
	e2 := presenceEngine(t, p, "components/Spy.html",
		"---\nstate: {n: 0}\nbrick: {listen: {presence: room:7}}\n---\n<div>{{ (.Presence \"room:8\").Total }}</div>",
		"content/a.html", `<Spy />`)
	if _, err := e2.Render(ctx, "content/a.html", nil); err == nil || !strings.Contains(err.Error(), "not a presence topic of this component") {
		t.Fatalf("undeclared topic read: %v", err)
	}
	// So is .Presence in a component that declares no presence.
	e3 := presenceEngine(t, p, "components/Spy.html", "---\nstate: {n: 0}\nbrick: {listen: room:7}\n---\n<div>{{ (.Presence \"room:7\").Total }}</div>", "content/a.html", `<Spy />`)
	if _, err := e3.Render(ctx, "content/a.html", nil); err == nil || !strings.Contains(err.Error(), "not a presence topic") {
		t.Fatalf("listen-only component read presence: %v", err)
	}
}

// drainAfterClose returns the lines buffered before the body was closed.
func (c *sseClient) drainAfterClose() []string {
	var out []string
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				return out
			}
			out = append(out, l)
		case <-time.After(time.Second):
			return out
		}
	}
}

// A topic over MaxEntries leaves the extra stream unlisted but working;
// a render returns at most MaxList entries while Total counts all.
func TestPresenceCaps(t *testing.T) {
	t.Parallel()
	p := newPush(t, PushOptions{Presence: PresenceOptions{Identify: identifyByHeader, MaxEntries: 1}})
	e := presenceEngine(t, p)
	srv := eventsServer(e)
	t.Cleanup(srv.Close)
	_, _, tokA := renderAs(t, e, "alice")
	a, _ := openEventsAs(t, srv, "alice", tokA)
	a.nextControl(t, "brick:presence:room:7")
	_, _, tokB := renderAs(t, e, "bob")
	if _, code := openEventsAs(t, srv, "bob", tokB); code != http.StatusOK {
		t.Fatalf("a full topic refused the stream: %d", code)
	}
	waitFor(t, "bob's stream", func() bool { return p.Connections() == 2 })
	if l, _ := p.opts.Presence.Store.List(context.Background(), "room:7"); len(l) != 1 {
		t.Fatalf("over MaxEntries: %+v", l)
	}
	raw := []PresenceEntry{{ID: "c"}, {ID: "a"}, {ID: "b"}, {ID: "a"}}
	got := aggregatePresence(raw, 2)
	if got.Total != 3 || len(got.Entries) != 2 || got.Entries[0].ID != "a" || got.Entries[0].Conns != 2 || got.Entries[1].ID != "b" {
		t.Fatalf("aggregate %+v", got)
	}
	big := map[string]string{}
	for i := range MaxPresenceDataKeys + 1 {
		big[string(rune('a'+i))] = "v"
	}
	for _, bad := range []PresenceEntry{{ID: strings.Repeat("x", MaxPresenceIDLen+1)}, {Data: big}, {Data: map[string]string{"k": strings.Repeat("v", MaxPresenceDataLen+1)}}} {
		if checkPresenceEntry(bad) == nil {
			t.Errorf("oversized entry accepted: %d keys", len(bad.Data))
		}
	}
}

// An entry outlives its last renewal by the TTL and no longer.
func TestMemoryPresenceStoreExpiry(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	s := NewMemoryPresenceStore(func() time.Time { return now })
	ctx := context.Background()
	if err := s.Join(ctx, "t", "c1", PresenceEntry{ID: "a"}, time.Minute, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.Join(ctx, "t", "c2", PresenceEntry{ID: "b"}, time.Minute, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.Join(ctx, "t", "c3", PresenceEntry{ID: "c"}, time.Minute, 2); !errors.Is(err, ErrPresenceFull) {
		t.Fatalf("over max: %v", err)
	}
	if err := s.Join(ctx, "t", "c2", PresenceEntry{ID: "b"}, time.Minute, 2); err != nil {
		t.Fatalf("a renewal at max: %v", err)
	}
	now = now.Add(30 * time.Second)
	_ = s.Join(ctx, "t", "c1", PresenceEntry{ID: "a"}, time.Minute, 2) // renew c1 only
	now = now.Add(45 * time.Second)
	l, _ := s.List(ctx, "t")
	if len(l) != 1 || l[0].ID != "a" {
		t.Fatalf("after c2 lapsed: %+v", l)
	}
	// The lapsed place is free again.
	if err := s.Join(ctx, "t", "c3", PresenceEntry{ID: "c"}, time.Minute, 2); err != nil {
		t.Fatal(err)
	}
	_ = s.Leave(ctx, "t", "c1")
	_ = s.Leave(ctx, "t", "c3")
	if l, _ := s.List(ctx, "t"); len(l) != 0 {
		t.Fatalf("after leaves: %+v", l)
	}
}

// otherBroadcaster stands for a cross-server Broadcaster.
type otherBroadcaster struct{ *MemoryBroadcaster }

// With a cross-server Broadcaster and no shared store, a presence
// component fails to render rather than show one server's viewers.
func TestPresenceNeedsSharedStore(t *testing.T) {
	t.Parallel()
	p := newPush(t, PushOptions{Broadcaster: otherBroadcaster{NewMemoryBroadcaster()}})
	e := presenceEngine(t, p)
	if _, err := e.Render(context.Background(), "content/a.html", nil); err == nil || !strings.Contains(err.Error(), "shared PushOptions.Presence.Store") {
		t.Fatalf("render: %v", err)
	}
	// A shared store given explicitly works.
	p2 := newPush(t, PushOptions{Broadcaster: otherBroadcaster{NewMemoryBroadcaster()}, Presence: PresenceOptions{Store: NewMemoryPresenceStore(nil)}})
	if _, err := presenceEngine(t, p2).Render(context.Background(), "content/a.html", nil); err != nil {
		t.Fatal(err)
	}
}

func TestPresenceDeclaration(t *testing.T) {
	t.Parallel()
	p := newPush(t, PushOptions{})
	ok := []string{
		"brick: {listen: {presence: room}}",
		"brick: {listen: {presence: [a, b], topics: chat}}",
	}
	for _, decl := range ok {
		e := presenceEngine(t, p, "components/X.html", "---\nstate: {n: 0}\n"+decl+"\n---\n<div>x</div>", "content/a.html", `<X />`)
		if _, err := e.Render(context.Background(), "content/a.html", nil); err != nil {
			t.Errorf("%s: %v", decl, err)
		}
	}
	bad := map[string]string{
		"brick: {listen: {presence: []}}":                                                 "names no topic",
		"brick: {listen: {presence: 3}}":                                                  "must be a topic",
		"brick: {listen: {presence: \"brick:x\"}}":                                        "not a topic",
		"brick: {listen: {presence: " + strings.Repeat("r", MaxPresenceTopicLen+1) + "}}": "not a topic",
	}
	for decl, want := range bad {
		e := presenceEngine(t, p, "components/X.html", "---\nstate: {n: 0}\n"+decl+"\n---\n<div>x</div>", "content/a.html", `<X />`)
		if _, err := e.Render(context.Background(), "content/a.html", nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", decl, err, want)
		}
	}
}

// A page whose only live part is a listening component loads the runtime,
// which opens the event stream; without it push never connects.
func TestListenPageLoadsRuntime(t *testing.T) {
	t.Parallel()
	e := presenceEngine(t, newPush(t, PushOptions{}), "components/Room.html", roomComponent,
		"content/a.html", `<html><head>{{ assets }}</head><body><Room room="7" /></body></html>`)
	out, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `<script type="module" src="/_brick/c/runtime.`) {
		t.Fatalf("no runtime: %s", out)
	}
}
