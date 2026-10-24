package brick

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Tests for lazy islands and streaming action output. BV in a comment marks a break-verified test.

// lazyTicketRE finds a placeholder's ticket and mode.
var lazyTicketRE = regexp.MustCompile(`<brick-lazy data-brick-lazy="([^"]+)" data-when="([a-z]+)">`)

// lazyPage is a page with one lazy chart inside a boundary.
const lazyPage = `<brick:boundary>
<Chart brick:lazy="visible" range="7d" />
<brick:prop name="loading"><div class="skeleton"></div></brick:prop>
<brick:prop name="fallback"><p>Chart unavailable.</p></brick:prop>
</brick:boundary>`

// lazyChart reads its prop, the page's route, the request's provided user
// and a URL parameter; with range="fail" it fails at render.
const lazyChart = `---
props:
  range: {type: string, default: ""}
---
<div class="chart">range={{ .Props.range }}|slug={{ .Page.Route.slug }}|user={{ .Ctx.user }}{{ if eq .Props.range "fail" }}{{ .Props.range.Field }}{{ end }}</div>`

// lazyEngine builds an engine over the chart and the given page; the
// session is the X-Session header, and every request provides user=ada.
func lazyEngine(t *testing.T, opts Options, page string, files ...string) *Engine {
	t.Helper()
	opts.FS = theme(append([]string{"components/Chart.html", lazyChart, "content/a.html", page}, files...)...)
	if opts.Sessions == nil {
		opts.Sessions = SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get("X-Session"), nil })
	}
	if opts.ActionRequest == nil {
		opts.ActionRequest = func(*http.Request) []RenderOption { return []RenderOption{Provide("user", "ada")} }
	}
	return newEngine(t, opts)
}

// lazyFill renders and fills a.html, returning the page and its first
// ticket.
func lazyFill(t *testing.T, e *Engine, opts ...RenderOption) (string, string) {
	t.Helper()
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/a.html", nil, WithRoute(map[string]string{"slug": "q3"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Fill(ctx, r, opts...)
	if err != nil {
		t.Fatal(err)
	}
	m := lazyTicketRE.FindStringSubmatch(string(b))
	if m == nil {
		return string(b), ""
	}
	return string(b), html.UnescapeString(m[1])
}

// getIsland redeems a ticket at the island endpoint.
func getIsland(e *Engine, ticket, session, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/_brick/island?t="+strings.ReplaceAll(ticket, "+", "%2B"), nil)
	req.Header.Set("Accept", IslandContentType)
	if session != "" {
		req.Header.Set("X-Session", session)
	}
	if query != "" {
		req.Header.Set(islandQueryHeader, query)
	}
	rec := httptest.NewRecorder()
	e.IslandHandler().ServeHTTP(rec, req)
	return rec
}

// islandHTML decodes a 200 answer's html.
func islandHTML(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != IslandContentType {
		t.Fatalf("content type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("cache control %q", cc)
	}
	var ans islandAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &ans); err != nil || ans.V != islandEnvelopeVersion {
		t.Fatalf("answer %v: %s", err, rec.Body)
	}
	return ans.HTML
}

// The page holds the placeholder with the boundary's loading block, never
// the island's output, links the runtime, and is marked private.
func TestLazyPlaceholder(t *testing.T) {
	t.Parallel()
	e := lazyEngine(t, Options{States: NewMemoryStates(0, 0)}, "{{ assets }}"+lazyPage)
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.HasLazy() || r.HasState() {
		t.Fatalf("HasLazy %v HasState %v", r.HasLazy(), r.HasState())
	}
	page, ticket := lazyFill(t, e)
	if ticket == "" || !strings.Contains(page, `data-when="visible"><div class="skeleton"></div></brick-lazy>`) {
		t.Fatalf("placeholder: %s", page)
	}
	if strings.Contains(page, "range=") || strings.Contains(page, "Chart unavailable") {
		t.Fatalf("the island rendered with the page: %s", page)
	}
	if !strings.Contains(page, "/c/runtime.") {
		t.Fatalf("a lazy island needs the runtime: %s", page)
	}
	// A page shown to two visitors gets two tickets.
	if _, again := lazyFill(t, e); again == ticket {
		t.Fatal("tickets must be issued per response")
	}
}

// Redeeming renders the island with its props, the page's route, the
// request's Provide values and the page's query; a static export renders
// it in place; brick: {lazy:} makes it a component's default.
func TestLazyRedeem(t *testing.T) {
	t.Parallel()
	e := lazyEngine(t, Options{States: NewMemoryStates(0, 0)}, lazyPage)
	_, ticket := lazyFill(t, e, WithSession("s1"))
	got := islandHTML(t, getIsland(e, ticket, "s1", ""))
	if !strings.Contains(got, "range=7d|slug=q3|user=ada") {
		t.Fatalf("island: %s", got)
	}
	// A stateful lazy island reads the page's query (Brick-Query), and its
	// answer carries a live handle bound to the session.
	f := lazyEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: counterScripting()}, `<Filter brick:lazy />`,
		"components/Filter.html", "---\nstate:\n  q: \"\"\nbrick: {query: [q]}\n---\n<brick:script>counter</brick:script>\n<div>q={{ .State.q }}</div>")
	_, ft := lazyFill(t, f, WithSession("s1"))
	fh := islandHTML(t, getIsland(f, ft, "s1", "q=hello"))
	if !strings.Contains(fh, "q=hello") || !stateRE.MatchString(fh) {
		t.Fatalf("stateful lazy island: %s", fh)
	}
	// A ticket redeems again (a reload of the island), within its life.
	if rec := getIsland(e, ticket, "s1", ""); rec.Code != http.StatusOK {
		t.Fatalf("second redeem %d", rec.Code)
	}
	exported, _ := lazyFill(t, e, WithStaticState())
	if strings.Contains(exported, "<brick-lazy") || !strings.Contains(exported, "range=7d") {
		t.Fatalf("export renders lazy islands in place: %s", exported)
	}

	d := lazyEngine(t, Options{States: NewMemoryStates(0, 0)}, `<Slow />`, "components/Slow.html", "---\nbrick: {lazy: idle}\n---\n<p>slow</p>")
	page, tk := lazyFill(t, d)
	if tk == "" || !strings.Contains(page, `data-when="idle"`) {
		t.Fatalf("brick: {lazy:} default: %s", page)
	}
	o := lazyEngine(t, Options{States: NewMemoryStates(0, 0)}, `<Slow brick:lazy />`, "components/Slow.html", "---\nbrick: {lazy: idle}\n---\n<p>slow</p>")
	if page, _ := lazyFill(t, o); !strings.Contains(page, `data-when="load"`) {
		t.Fatalf("the call site wins: %s", page)
	}
}

// BV: a ticket opens only for the session it was issued to, unchanged and
// unexpired, under a key the engine still has; anything else is 410 and
// renders nothing. Breaking the session comparison in redeemIsland makes
// the foreign case render (the sealed store binds the session too, so the
// memory store is the one that proves the engine's own check).
func TestLazyTicketRefusals(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	stores := map[string]StateStore{
		"memory": NewMemoryStates(0, 0),
		"sealed": sealedStore(t, sealedOpts{SealedOptions: SealedOptions{Clock: clock}}),
	}
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			e := lazyEngine(t, Options{States: store}, lazyPage)
			_, ticket := lazyFill(t, e, WithSession("s1"))
			refused := func(what, tk, session string) {
				t.Helper()
				rec := getIsland(e, tk, session, "")
				if rec.Code != http.StatusGone || strings.Contains(rec.Body.String(), "range=") {
					t.Fatalf("%s: %d %s", what, rec.Code, rec.Body)
				}
			}
			refused("foreign session", ticket, "s2")
			refused("no session", ticket, "")
			last := ticket[len(ticket)-1]
			flip := byte('A')
			if last == 'A' {
				flip = 'B'
			}
			refused("forged", ticket[:len(ticket)-1]+string(flip), "s1")
			refused("unknown", "s1.deadbeef", "s1")
			refused("empty", "", "s1")
			// A ticket is no action handle, and a handle is no ticket.
			ae := v5Engine(t, Options{States: store})
			_, h, tok := fillPage(t, ae)
			if rec := postAction(e, DefaultStoreName+"."+ticket, "", "increment", nil); rec.Code != http.StatusGone {
				t.Fatalf("ticket as handle: %d", rec.Code)
			}
			_, plain, _ := strings.Cut(h, ".")
			refused("handle as ticket", plain, "")
			_ = tok
		})
	}
	// Expiry: the sealed ticket outlives its TTL.
	e := lazyEngine(t, Options{States: stores["sealed"]}, lazyPage)
	_, ticket := lazyFill(t, e, WithSession("s1"))
	if rec := getIsland(e, ticket, "s1", ""); rec.Code != http.StatusOK {
		t.Fatalf("fresh: %d", rec.Code)
	}
	now = now.Add(DefaultIslandTicketTTL + sealedSkew + time.Minute)
	if rec := getIsland(e, ticket, "s1", ""); rec.Code != http.StatusGone {
		t.Fatalf("expired: %d", rec.Code)
	}
}

// Retiring the key a ticket was sealed with retires the ticket.
func TestLazyKeyRetirement(t *testing.T) {
	t.Parallel()
	old := lazyEngine(t, Options{States: sealedStore(t, sealedOpts{Keys: [][]byte{testKey(1)}})}, lazyPage)
	_, ticket := lazyFill(t, old)
	if !strings.HasPrefix(ticket, sealedPrefix) {
		t.Fatalf("a sealing engine seals tickets: %q", ticket)
	}
	both := lazyEngine(t, Options{States: sealedStore(t, sealedOpts{Keys: [][]byte{testKey(2), testKey(1)}})}, lazyPage)
	if rec := getIsland(both, ticket, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("a second server with the key: %d", rec.Code)
	}
	retired := lazyEngine(t, Options{States: sealedStore(t, sealedOpts{Keys: [][]byte{testKey(2)}})}, lazyPage)
	if rec := getIsland(retired, ticket, "", ""); rec.Code != http.StatusGone {
		t.Fatalf("retired key: %d", rec.Code)
	}
}

// A failing island answers its boundary's fallback in Prod and the error
// block in Dev, with status 200 and Brick-Island-Error.
func TestLazyFailure(t *testing.T) {
	t.Parallel()
	page := strings.Replace(lazyPage, `range="7d"`, `range="fail"`, 1)
	for _, mode := range []Mode{Prod, Dev} {
		e := lazyEngine(t, Options{States: NewMemoryStates(0, 0), Mode: mode}, page)
		_, ticket := lazyFill(t, e)
		rec := getIsland(e, ticket, "", "")
		got := islandHTML(t, rec)
		if rec.Header().Get(IslandErrorHeader) != "1" || !strings.Contains(got, "Chart unavailable") {
			t.Fatalf("mode %v: %s", mode, got)
		}
		if hasBlock := strings.Contains(got, "brick-error"); hasBlock != (mode == Dev) {
			t.Fatalf("mode %v error block %v: %s", mode, hasBlock, got)
		}
	}
}

// A lazy island inside a lazy answer gets a placeholder of its own; an
// action's re-render shows lazy children in place.
func TestLazyNested(t *testing.T) {
	t.Parallel()
	e := lazyEngine(t, Options{States: NewMemoryStates(0, 0)}, `<Outer brick:lazy />`,
		"components/Outer.html", `<section>outer <Chart brick:lazy="idle" range="1d" /></section>`)
	_, ticket := lazyFill(t, e)
	got := islandHTML(t, getIsland(e, ticket, "", ""))
	m := lazyTicketRE.FindStringSubmatch(got)
	if m == nil || m[2] != LazyIdle || strings.Contains(got, "range=") {
		t.Fatalf("nested: %s", got)
	}
	if inner := islandHTML(t, getIsland(e, html.UnescapeString(m[1]), "", "")); !strings.Contains(inner, "range=1d") {
		t.Fatalf("inner: %s", inner)
	}

	s := lazyEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: counterScripting()}, `<Counter />`,
		"components/Counter.html", "---\nstate:\n  count: 0\n---\n<brick:script>counter</brick:script>\n<div>{{ .State.count }} <Chart brick:lazy range=\"3d\" /></div>")
	r, err := s.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Fill(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "<brick-lazy") {
		t.Fatalf("first render: placeholder expected: %s", b)
	}
	m2 := stateRE.FindStringSubmatch(string(b))
	out, err := s.Action(context.Background(), ActionRequest{Handle: m2[1], Token: m2[2], Action: "increment"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "<brick-lazy") || !strings.Contains(string(out), "range=3d") {
		t.Fatalf("an action renders lazy children in place: %s", out)
	}
}

// Compile checks and the Dev warnings.
func TestLazyChecks(t *testing.T) {
	t.Parallel()
	for page, want := range map[string]string{
		`<Chart brick:lazy="soon" />`:               "brick:lazy takes load, visible, idle",
		`<Chart brick:lazy="{{ .Params.x }}" />`:    "written literally",
		`<brick:boundary><p>x</p></brick:boundary>`: "",
	} {
		e := lazyEngine(t, Options{States: NewMemoryStates(0, 0)}, page)
		_, err := e.RenderChunks(context.Background(), "content/a.html", nil)
		if want == "" {
			if err != nil {
				t.Fatalf("%s: %v", page, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", page, err)
		}
	}
	bad := lazyEngine(t, Options{States: NewMemoryStates(0, 0)}, `<Slow />`, "components/Slow.html", "---\nbrick: {lazy: soon}\n---\n<p>slow</p>")
	if _, err := bad.RenderChunks(context.Background(), "content/a.html", nil); err == nil || !strings.Contains(err.Error(), "brick: lazy:") {
		t.Fatalf("frontmatter lazy: %v", err)
	}
	// The loading block no longer warns, and the memory store and a big
	// ticket do.
	big := strings.Repeat("x", lazyTicketWarnBytes)
	e := lazyEngine(t, Options{States: NewMemoryStates(0, 0), Mode: Dev}, `<brick:boundary><Chart brick:lazy range="`+big+`" /><brick:prop name="loading"><i></i></brick:prop></brick:boundary>`)
	lazyFill(t, e)
	var msgs []string
	for _, w := range e.Warnings() {
		msgs = append(msgs, w.Message)
	}
	all := strings.Join(msgs, "\n")
	if strings.Contains(all, "no effect until streaming") || !strings.Contains(all, "keep their tickets in this process") {
		t.Fatalf("warnings: %s", all)
	}
	sealed := lazyEngine(t, Options{States: sealedStore(t, sealedOpts{}), Mode: Dev}, `<Chart brick:lazy range="`+big+`" />`)
	lazyFill(t, sealed)
	msgs = nil
	for _, w := range sealed.Warnings() {
		msgs = append(msgs, w.Message)
	}
	if all := strings.Join(msgs, "\n"); !strings.Contains(all, "KiB ticket") || strings.Contains(all, "in this process") {
		t.Fatalf("sealed warnings: %s", all)
	}
}

// ---- 16 · streaming action output ------------------------------------------

// streamScripting is a component that streams, fails after streaming, or
// does not stream.
func streamScripting() ctxScripting {
	state := func(a []any) map[string]any { st, _ := a[0].(map[string]any); return st }
	return ctxScripting{
		"chat": {
			"ask": func(ctx context.Context, a ...any) (any, error) {
				s := StreamFrom(ctx)
				for _, part := range []string{"Hel", "lo <b>", "!"} {
					if err := s.Write("answer", part, false); err != nil {
						return nil, err
					}
				}
				st := state(a)
				st["answer"] = "Hello <b>!"
				return st, nil
			},
			"quiet": func(ctx context.Context, a ...any) (any, error) {
				st := state(a)
				st["answer"] = "quiet"
				return st, nil
			},
			"broken": func(ctx context.Context, a ...any) (any, error) {
				if err := StreamFrom(ctx).Write("answer", "partial", true); err != nil {
					return nil, err
				}
				return nil, errors.New("model down")
			},
		},
	}
}

const chatComponent = "---\nstate:\n  answer: \"\"\n---\n<brick:script>chat</brick:script>\n<div><p brick:ref=\"answer\">{{ .State.answer }}</p></div>"

func streamEngine(t *testing.T, opts Options) *Engine {
	t.Helper()
	opts.FS = theme("components/Chat.html", chatComponent, "content/a.html", `<Chat />`)
	opts.Scripting = streamScripting()
	opts.States = NewMemoryStates(0, 0)
	return newEngine(t, opts)
}

// postStream posts an action asking for the stream.
func postStream(e *Engine, handle, token, action string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]any{"handle": handle, "token": token, "action": action, "args": []any{}})
	req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	req.Header.Set("Accept", EffectsContentType+", "+StreamContentType+", text/html;q=0.5")
	rec := httptest.NewRecorder()
	e.ActionHandler().ServeHTTP(rec, req)
	return rec
}

// streamLines decodes an NDJSON answer.
func streamLines(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != StreamContentType {
		t.Fatalf("content type %q: %s", ct, rec.Body)
	}
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		out = append(out, m)
	}
	return out
}

// A streaming action answers NDJSON chunks, flushed as written, then the
// envelope; text stays text (a JSON string, never markup the server
// rendered); an action that does not stream answers the plain envelope.
func TestStreamAction(t *testing.T) {
	t.Parallel()
	e := streamEngine(t, Options{})
	_, h, tok := fillPage(t, e)
	rec := postStream(e, h, tok, "ask")
	lines := streamLines(t, rec)
	if len(lines) != 4 || !rec.Flushed || rec.Header().Get(accelBufferingHeader) != "no" {
		t.Fatalf("lines %v flushed %v", lines, rec.Flushed)
	}
	var text string
	for _, l := range lines[:3] {
		c, _ := l["stream"].(map[string]any)
		if c["ref"] != "answer" || c["replace"] != nil {
			t.Fatalf("chunk %v", l)
		}
		text += c["text"].(string)
	}
	if text != "Hello <b>!" {
		t.Fatalf("text %q", text)
	}
	final := lines[3]
	if final["v"] != float64(1) || final["version"] != float64(1) || !strings.Contains(final["html"].(string), "Hello &lt;b&gt;!") {
		t.Fatalf("envelope %v", final)
	}
	_, h2, tok2 := fillPage(t, e)
	plain := postStream(e, h2, tok2, "quiet")
	if ct := plain.Header().Get("Content-Type"); ct != EffectsContentType {
		t.Fatalf("no stream → plain envelope, got %q", ct)
	}
	// A client that does not accept the stream never gets one; the
	// action's chunks are dropped.
	if rec := v8bPost(e, h, tok, "ask", nil, nil, ""); rec.Header().Get("Content-Type") != EffectsContentType {
		t.Fatalf("no Accept: %q", rec.Header().Get("Content-Type"))
	}
}

// A failure after streaming ends the stream with an error line carrying
// the status the plain answer would have had.
func TestStreamError(t *testing.T) {
	t.Parallel()
	e := streamEngine(t, Options{})
	_, h, tok := fillPage(t, e)
	lines := streamLines(t, postStream(e, h, tok, "broken"))
	if len(lines) != 2 {
		t.Fatalf("lines %v", lines)
	}
	if c, _ := lines[0]["stream"].(map[string]any); c["replace"] != true || c["text"] != "partial" {
		t.Fatalf("chunk %v", lines[0])
	}
	errLine, _ := lines[1]["error"].(map[string]any)
	if lines[1]["v"] != float64(1) || errLine["status"] != float64(http.StatusUnprocessableEntity) || !strings.Contains(errLine["message"].(string), "the action failed") {
		t.Fatalf("error line %v", lines[1])
	}
}

// upperStream is a middleware's wrapper: it rewrites and drops chunks.
type upperStream struct{ next ActionStream }

func (u upperStream) Write(ref, text string, replace bool) error {
	if strings.Contains(text, "<") {
		return nil
	}
	return u.next.Write(ref, strings.ToUpper(text), replace)
}

// A middleware wraps the stream before next; outside a transport the
// stream is a no-op, and a collector given with WithActionStream sees
// every chunk (how bricktest collects them).
func TestStreamMiddlewareAndCollector(t *testing.T) {
	t.Parallel()
	mw := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, call *ActionCall) (*ActionResult, error) {
			return next(WithActionStream(ctx, upperStream{StreamFrom(ctx)}), call)
		}
	}
	e := streamEngine(t, Options{ActionMiddleware: []ActionMiddleware{mw}})
	_, h, tok := fillPage(t, e)
	lines := streamLines(t, postStream(e, h, tok, "ask"))
	var text []string
	for _, l := range lines {
		if c, ok := l["stream"].(map[string]any); ok {
			text = append(text, c["text"].(string))
		}
	}
	if strings.Join(text, "|") != "HEL|!" {
		t.Fatalf("filtered chunks %v", text)
	}
	if err := StreamFrom(context.Background()).Write("x", "y", false); err != nil {
		t.Fatal(err)
	}
	var got []string
	c := collectStream(func(ref, text string, replace bool) error { got = append(got, ref+":"+text); return nil })
	_, h2, tok2 := fillPage(t, e)
	if _, err := e.RunAction(WithActionStream(context.Background(), c), ActionRequest{Handle: h2, Token: tok2, Action: "ask"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "answer:HEL|answer:!" {
		t.Fatalf("collected %v", got)
	}
}

// collectStream adapts a function to ActionStream.
type collectStream func(ref, text string, replace bool) error

func (f collectStream) Write(ref, text string, replace bool) error { return f(ref, text, replace) }

// BV: a host that hands NewIslandTickets the store its state lives in
// still cannot redeem a state handle as a ticket (the ticket mark), nor
// act on a ticket (its token never leaves the server). Dropping the mark
// check renders the handle's component.
func TestLazySharedStore(t *testing.T) {
	t.Parallel()
	shared := NewMemoryStates(0, 0)
	e := lazyEngine(t, Options{States: shared, IslandTickets: NewIslandTickets(shared), Scripting: counterScripting()}, `<Chart brick:lazy range="1d" />`,
		"components/Counter.html", "---\nstate:\n  count: 0\n---\n<brick:script>counter</brick:script>\n<div>count={{ .State.count }}</div>",
		"content/b.html", `<Counter />`)
	_, ticket := lazyFill(t, e)
	r, err := e.RenderChunks(context.Background(), "content/b.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Fill(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	m := stateRE.FindStringSubmatch(string(b))
	_, handle, _ := strings.Cut(m[1], ".")
	if rec := getIsland(e, handle, "", ""); rec.Code != http.StatusGone {
		t.Fatalf("handle as ticket: %d %s", rec.Code, rec.Body)
	}
	for _, tok := range []string{"", m[2]} {
		if _, err := e.Action(context.Background(), ActionRequest{Handle: DefaultStoreName + "." + ticket, Token: tok, Action: "increment"}); err == nil {
			t.Fatalf("ticket as handle with token %q ran", tok)
		}
	}
}
