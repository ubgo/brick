# Islands, lazy islands and streaming

## What it is and when you want it

Some parts of a page should not wait for, or be cached with, the rest. An **island** renders for every request inside a page that is rendered once and cached (a header that says "Hi, Sara"). A **lazy island** is left out of the first response and fetched by the browser later, when the page has loaded, when it scrolls into view, or when the browser is idle (a slow chart below the fold). A **streamed island** is sent later in the same response, so the visitor sees the page at once and the slow panel fills in without a second request. A **streamed reply** sends text into the page while an action is still running (a chat answer that grows as it arrives). You want them when one slow or personal part would otherwise hold back or personalise the whole page.

## Before you start

- Read [stateful components and actions](stateful-components.md) for the action endpoint; `brick.stream` is an action feature.
- A boundary (`<brick:boundary>`) gives a lazy or streamed island its `loading` and `fallback` blocks; see the guide's [when things go wrong](../../guide.md#when-things-go-wrong).
- The example is [`examples/interactivity-lazy-stream`](../../../examples/interactivity-lazy-stream/).

## Step 1 — Mount the island endpoint

The browser fetches a lazy island from `/_brick/island?t=TICKET`. The site layer mounts it; with the engine alone, mount `eng.IslandHandler()` beside the action endpoint. From `examples/interactivity-lazy-stream/main.go`:

<!-- ds:block id=app-handler-peugtyhy title="examples/interactivity-lazy-stream/main.go" -->
**examples/interactivity-lazy-stream/main.go** · [`examples/interactivity-lazy-stream/main.go:95-102`](../../../examples/interactivity-lazy-stream/main.go#L95-L102)

```go
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase+brick.IslandEndpoint, a.eng.IslandHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("GET /{$}", a.index)
	return mux
}
```
<!-- /ds:block hash=45d9bf -->

## Step 2 — Fill or stream the cached page per request

The page is rendered once with `RenderChunks`. A browser loading the page (`Sec-Fetch-Mode: navigate`) on a writer that can flush gets the stream islands later in the same response through `eng.Stream`; everything else (curl, a crawler, a client navigation, a test) gets them rendered in place by `Fill`, with the same bytes as a plain island. `brick.Provide` hands the island a request value:

<!-- ds:block id=app-index-r7v5qe8m title="examples/interactivity-lazy-stream/main.go" -->
**examples/interactivity-lazy-stream/main.go** · [`examples/interactivity-lazy-stream/main.go:117-138`](../../../examples/interactivity-lazy-stream/main.go#L117-L138)

```go
func (a *app) index(w http.ResponseWriter, r *http.Request) {
	a.once.Do(func() { a.page, a.err = a.eng.RenderChunks(r.Context(), page, nil) })
	if a.err != nil {
		http.Error(w, a.err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	flusher, canFlush := w.(http.Flusher)
	if canFlush && a.page.HasStream() && r.Header.Get("Sec-Fetch-Mode") == "navigate" {
		w.Header().Set("X-Accel-Buffering", "no") // tell nginx not to buffer
		if err := a.eng.Stream(r.Context(), w, flusher.Flush, a.page, userOf(r)...); err != nil {
			log.Printf("stream: %v", err)
		}
		return
	}
	html, err := a.eng.Fill(r.Context(), a.page, userOf(r)...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(html)
}
```
<!-- /ds:block hash=e3a0e5 -->

## Step 3 — Mark the island, the lazy island and the streamed one

An island is a frontmatter flag. It takes props only (no children, no `<brick:prop>` blocks), because it renders after the page's template is gone, and it reads request values through `.Ctx`. `components/Account.html`:

<!-- snippet: examples/interactivity-lazy-stream/theme/components/Account.html -->
```html
---
brick:
  island: true
---
{{ with .Ctx.user }}<a id="account" href="/account">Hi, {{ . }}</a>{{ else }}<a id="account" href="/login">Sign in</a>{{ end }}
```

Lazy and streamed are chosen at the call site with `brick:lazy` (`load`, the default; `visible`; `idle`; `stream`), or in a component's frontmatter with `brick: {lazy: visible}`, where the call site wins. Any component can be lazy, and lazy implies island. `SalesChart.html` and `Revenue.html` are plain components (`<figure id="sales">Sales: 42 orders</figure>` and `<p id="revenue">Revenue: $1,200</p>`). The page, `content/index.html`:

<!-- snippet: examples/interactivity-lazy-stream/theme/content/index.html -->
```html
<!doctype html>
<html><body>
<header><Account /></header>
<brick:boundary>
  <SalesChart brick:lazy="visible" />
  <brick:prop name="loading"><div class="skeleton">Loading sales…</div></brick:prop>
  <brick:prop name="fallback"><p>Sales unavailable.</p></brick:prop>
</brick:boundary>
<brick:boundary>
  <Revenue brick:lazy="stream" />
  <brick:prop name="loading"><div class="skeleton">Loading revenue…</div></brick:prop>
  <brick:prop name="fallback"><p>Revenue unavailable.</p></brick:prop>
</brick:boundary>
<Ask />
{{ assets }}
</body></html>
```

## Step 4 — Stream text from an action

`brick.stream(ref, text, replace = False)` appends text to the element with `brick:ref="ref"` in the component's root (or replaces its text) before the action returns; the action's normal render then lands as usual. Chunks are always text set with `textContent`, so they never need escaping. `components/Ask.html`:

<!-- snippet: examples/interactivity-lazy-stream/theme/components/Ask.html -->
```html
---
state:
  q: ""
  answer: ""
---
<brick:script>
PARTS = ["Hel", "lo, ", "Ada!"]

def ask(state, props):
    text = ""
    for part in PARTS:
        brick.stream("answer", part)
        text += part
    return dict(state, answer = text)
</brick:script>
<div class="ask">
  <p id="answer" brick:ref="answer">{{ .State.answer }}</p>
  <input brick:model.defer="q">
  <button id="ask" brick:click="ask">Ask</button>
</div>
```

In a real app the loop reads from a global your Go host gives scripts (a model client, a job's output). In Go middleware, `brick.StreamFrom(ctx).Write(ref, text, replace)` does the same.

## Step 5 — Seal the tickets so they survive a restart

A lazy island's placeholder carries a ticket, and the browser redeems it at `/_brick/island`. By default tickets live in the memory of the process that issued them, so a restart or a second server answers `410` for every placeholder already on a visitor's screen, and the engine warns about it in Dev. `Options.IslandTickets` replaces the ticket store. `brick.NewIslandTickets(store)` over a sealed store puts the island itself inside the ticket, encrypted and authenticated under your keys: any server holding the keys redeems it, a changed byte never does, and `TTL` is how long it stays good. The store must be its own, never the one that holds component state, because a ticket's handle must not reach the action endpoint. `main.go`:

<!-- snippet: examples/interactivity-lazy-stream/main.go lines=36-90 -->
```go
// ticketTTL is how long a lazy island's ticket redeems: a visitor who
// leaves the tab open for the afternoon still gets the chart, a ticket
// found in an old log is dead by the next day.
const ticketTTL = 4 * time.Hour

// demoKeyByte fills the demo's 32-byte ticket key. It is public in this
// source, so it protects nothing: a real deployment makes a key with
// `brick keygen` and reads it from the environment (brick.KeysFromEnv).
const demoKeyByte = 0x2a

// demoKeys is the key ring newApp seals tickets under.
func demoKeys() (brick.KeyRing, error) {
	return brick.StaticKeys(bytes.Repeat([]byte{demoKeyByte}, brick.StateKeySize))
}

// newApp builds the engine with tickets sealed under the demo key ring.
func newApp(dir string) (*app, error) {
	keys, err := demoKeys()
	if err != nil {
		return nil, err
	}
	return newAppWithKeys(dir, keys)
}

// newAppWithKeys builds the engine with Options.IslandTickets set: a
// lazy island's ticket is the island itself, sealed (encrypted and
// authenticated) under keys, so it redeems on any server holding the same
// keys, survives a restart, and a changed byte is refused. Without this
// option tickets live in process memory and a restart or a second server
// answers 410 (the engine warns about it in Dev). The ticket store must
// be its own: never the store that holds component state.
func newAppWithKeys(dir string, keys brick.KeyRing) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	sealed, err := brick.NewSealedStates(brick.SealedOptions{Sealer: brick.NewAESGCMSealer(keys, nil), TTL: ticketTTL})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{
		FS:            root.FS(),
		Scripting:     sc,
		States:        brick.NewMemoryStates(0, 0),
		IslandTickets: brick.NewIslandTickets(sealed),
	})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng}, nil
}
```

The demo key is public in the source and protects nothing; make a real one with `brick keygen` and read it with `brick.KeysFromEnv(os.LookupEnv, "BRICK_SECRET")`. The test issues a ticket from one app, redeems it on a brand-new engine with the same keys (a restart or a second server) and on one with other keys:

<!-- snippet: examples/interactivity-lazy-stream/main_test.go lines=100-134 -->
```go
func TestTicketsSurviveARestart(t *testing.T) {
	first, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	m := ticketRE.FindStringSubmatch(get(t, first.handler(), httptest.NewRequest(http.MethodGet, "/?user=Sara", nil)))
	if m == nil {
		t.Fatal("no ticket on the page")
	}
	redeem := func(a *app) int {
		rec := httptest.NewRecorder()
		a.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_brick/island?t="+m[1], nil))
		return rec.Code
	}

	restarted, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	if code := redeem(restarted); code != http.StatusOK {
		t.Fatalf("same keys, new engine: %d, want 200", code)
	}

	other, err := brick.StaticKeys(bytes.Repeat([]byte{0x17}, brick.StateKeySize))
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := newAppWithKeys("theme", other)
	if err != nil {
		t.Fatal(err)
	}
	if code := redeem(stranger); code != http.StatusGone {
		t.Fatalf("other keys: %d, want 410", code)
	}
}
```

Sealed tickets are long (hundreds of characters) because the island travels inside them; the unsealed default is a short random id.

## Step 6 — Read lazy islands in a test

A lazy island is a placeholder until the browser asks for it, which a test would have to imitate with a second request. `brick.WithLazyInline()` makes `Fill` render every lazy island in place, with its state stored as usual so its actions still run. Pass it to `bricktest.Live`:

<!-- snippet: examples/interactivity-lazy-stream/main_test.go lines=138-147 -->
```go
func TestLazyInlineInTests(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page, brick.WithLazyInline())
	if !strings.Contains(live.HTML(), `<figure id="sales">Sales: 42 orders</figure>`) || strings.Contains(live.HTML(), "<brick-lazy") {
		t.Fatalf("lazy island not rendered in place:\n%s", live.HTML())
	}
}
```

It is for tests only: a page served with it loses what lazy loading is for.

## Try it

```sh
cd examples/interactivity-lazy-stream && go run .
```

A plain request, `curl -s 'localhost:8093/?user=Sara'` (blank lines trimmed, the sealed ticket cut short): the island is filled, the lazy island is a placeholder holding the boundary's `loading` block and a ticket, and the stream island is rendered in place because curl cannot take a stream:

```html
<header><a id="account" href="/account">Hi, Sara</a></header>
  <brick-lazy data-brick-lazy="s1.AVROYs5o1wS-aeqAlMGL11T9PFY4VpGz…" data-when="visible"><div class="skeleton">Loading sales…</div></brick-lazy>
  <p id="revenue">Revenue: $1,200</p>
<div data-brick="be1abfdfd" data-brick-state="default.<handle>" data-brick-token="<token>" data-brick-name="Ask" class="ask">
```

When the placeholder scrolls into view the runtime redeems the ticket, and the answer replaces the skeleton (the example's test checks these exact bytes):

```text
GET /_brick/island?t=s1.AVROYs5o1wS-aeqAlMGL11T9PFY4VpGz…
{"v":1,"html":"<figure id=\"sales\">Sales: 42 orders</figure>","assets":[]}
```

A browser load, `curl -s -H 'Sec-Fetch-Mode: navigate' localhost:8093/`, gets the placeholder first and the island after the page, in the same response:

```html
  <brick-lazy data-brick-stream="s1"><div class="skeleton">Loading revenue…</div></brick-lazy>
…
</body></html>
<noscript><style>brick-lazy[data-brick-stream="s1"]{display:none}brick-fill[data-brick-fill="s1"]{display:contents}</style></noscript><brick-fill data-brick-fill="s1" hidden><p id="revenue">Revenue: $1,200</p></brick-fill><script>window.__brickFill=…;__brickFill("s1")</script>
```

`cd examples && go test ./interactivity-lazy-stream/` checks all of it, and that `Ask` streams `Hel`, `lo, `, `Ada!` into `answer` before its render lands.

## Pitfalls

- **A forged, foreign or expired ticket answers 410**, and the runtime reloads the page once. Tickets are bound to the visitor's session and valid for an hour by default (this example sets four). Without state keys they live in this process's memory, so a restart or a second server also answers 410; the engine warns `lazy islands keep their tickets in this process` and the fix is to seal them: `Options.IslandTickets` as in Step 5, or `site.Options.StateKeys` in the site layer.
- **`eng.Stream` writes the fills after the page's last byte.** That is valid and browsers place each fill, but the site layer uses `FillStream` to write them before `</body>`; do the same if your HTML must end with `</html>`.
- **Without JavaScript a streamed island shows at the end of the body**, not in its place. A page whose layout must hold without scripts uses a plain island. A Content-Security-Policy without the page's nonce blocks the tiny inline script; the runtime then places the islands once the whole response has arrived.
- **Search engines see the placeholder** of a lazy island; use it below the fold. `brick build` renders lazy islands in place, since an exported site has no endpoint.
- **An island takes props only.** Pass ids and values, not markup: it renders after the page's template is gone, so it cannot draw blocks that depend on the page's data.
- **A page with a lazy island is private** (its tickets belong to one visitor), like a page with a stateful component.
- **A stream island that fails never fails the page** (its status is already sent): production writes the boundary's `fallback`, Dev the error block. A loader's `brick.header` from it is dropped, with a Dev warning.
- **Streaming holds the component's queue.** Give a streaming component a longer `actionmw.Timeout`, and turn off proxy buffering; the answer already sends `X-Accel-Buffering: no`.

## Related

- [Stateful components and actions](stateful-components.md), [navigation](navigation.md) (a client navigation never streams).
- The guide's [islands](../../guide.md#islands--per-request-parts-of-a-cached-page) and [lazy islands and streamed replies](../../guide.md#lazy-islands-and-streamed-replies) sections.
- [Attribute reference](../../reference/attributes.md) and [feature index](../../reference/features.md#islands-lazy-and-streaming).
- Example: [`examples/interactivity-lazy-stream`](../../../examples/interactivity-lazy-stream/).
