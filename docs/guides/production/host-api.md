# The Go host API: New, Render, RenderComponent, handlers, RunAction

## What it is and when you want it

brick's site layer (`site`, `brick serve`) is optional. Underneath it is one `*brick.Engine` that any Go program can use: render a page or a single component into bytes, cache a page once and fill its per-request parts, mount the engine's HTTP handlers on your own router, and call an action from Go with no HTTP at all. You want this when brick is the view layer of an existing app (your router, your auth, your database) rather than the whole site.

## Before you start

- Go 1.25 or newer and `go get github.com/ubgo/brick`.
- A theme folder: `components/`, `content/`, optionally `layouts/` ([Pages and routes](../site/pages-and-routes.md) has the layout; every path in a theme starts at the project root).
- The example this page builds is [examples/production-host-api](../../../examples/production-host-api/).

## Step 1 — Build one engine

`brick.New` compiles nothing yet; it checks the options and returns an engine that is safe for concurrent renders. Every `Options` field is optional: this host sets the project root, a script runtime, a state store and its limits. Pass an `os.Root`-backed FS so a symlink cannot escape the theme.

<!-- ds:block id=newengine-z7ddf77v title="examples/production-host-api/main.go" -->
**examples/production-host-api/main.go** · [`examples/production-host-api/main.go:57-59`](../../../examples/production-host-api/main.go#L57-L59)

```go
func newEngine(dir string, l limits) (*brick.Engine, error) {
	return buildEngine(dir, l, sources{})
}
```
<!-- /ds:block hash=8a5f9f -->

`newEngine` hands the work to one function that assembles the options, so a test can build the same engine with a pinned clock and random source ([Limits and determinism](limits-determinism.md)):

<!-- ds:block id=buildengine-dmfsuna9 title="examples/production-host-api/main.go" -->
**examples/production-host-api/main.go** · [`examples/production-host-api/main.go:79-99`](../../../examples/production-host-api/main.go#L79-L99)

```go
func buildEngine(dir string, l limits, src sources) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{Steps: l.ScriptSteps, Timeout: l.ScriptTimeout})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{
		FS:             root.FS(),
		Scripting:      sc,
		States:         brick.NewMemoryStates(0, 0),
		MaxComponents:  l.MaxComponents,
		MaxOutputBytes: l.MaxOutputBytes,
		MaxActionBytes: l.MaxActionBytes,
		ActionErrors:   actionError,
		Clock:          src.Clock,
		Rand:           src.Rand,
	})
}
```
<!-- /ds:block hash=3df507 -->

## Step 2 — Render a page, or one component

`Engine.Render(ctx, path, props, opts...)` renders a page file; `props` arrive as `.Props`. `Engine.RenderComponent(ctx, name, props, children, opts...)` renders one component alone with no layout, for a gallery, a preview route or a test; `children` is brick markup for its body, and props keep their Go types. Render options carry per-call values: `brick.Provide` (for `inject:`), `WithSession`, `WithLocale`, `WithQuery`, `WithNonce`.

<!-- ds:block id=server-preview-dczhmp94 title="examples/production-host-api/main.go" -->
**examples/production-host-api/main.go** · [`examples/production-host-api/main.go:200-212`](../../../examples/production-host-api/main.go#L200-L212)

```go
func (s *server) preview(w http.ResponseWriter, r *http.Request) {
	props := map[string]any{}
	for k := range r.URL.Query() {
		props[k] = r.URL.Query().Get(k)
	}
	html, err := s.eng.RenderComponent(r.Context(), r.PathValue("name"), props, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}
```
<!-- /ds:block hash=209e10 -->

## Step 3 — Cache the page, fill the island per request

A component marked `brick: {island: true}` is the part of a page that differs per request:

<!-- snippet: examples/production-host-api/theme/components/Whoami.html -->
```html
---
brick:
  island: true
inject:
  user: {default: "guest"}
---
<p class="whoami">Signed in as {{ .Ctx.user }}</p>
```

`Engine.RenderChunks` renders the page once into a `*brick.Result` with a hole where each island goes; `Engine.Fill` renders only the islands, with this request's options, into the cached bytes. Calling `Render` on such a page also works; `Fill` is how you avoid rendering the shared parts on every request.

<!-- snippet: examples/production-host-api/main.go lines=27-29 -->
```go

// home is the page with an island, cached as a Result and filled per
// request.
```

The handler that does the filling renders the page once (guarded by `sync.Once`), then calls `Fill` with this request's `Provide` option:

<!-- ds:block id=server-home-z8tngqqw title="examples/production-host-api/main.go" -->
**examples/production-host-api/main.go** · [`examples/production-host-api/main.go:147-166`](../../../examples/production-host-api/main.go#L147-L166)

```go
func (s *server) home(w http.ResponseWriter, r *http.Request) {
	s.once.Do(func() { s.page, s.err = s.eng.RenderChunks(context.Background(), home, nil) })
	if s.err != nil {
		log.Printf("render %s: %v", home, s.err)
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	var opts []brick.RenderOption
	if u := r.URL.Query().Get("user"); u != "" {
		opts = append(opts, brick.Provide("user", u))
	}
	html, err := s.eng.Fill(r.Context(), s.page, opts...)
	if err != nil {
		log.Printf("fill %s: %v", home, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}
```
<!-- /ds:block hash=b13d6d -->

## Step 4 — Mount the engine's handlers

The browser runtime talks to a few endpoints under `AssetBase` (default `/_brick/`). The site layer mounts them for you; a Go host mounts the ones its features use, and may wrap any of them in its own middleware:

| Handler | Mount at | Needed for |
|---|---|---|
| `Engine.ActionHandler()` | `AssetBase + brick.ActionEndpoint` (`/_brick/action`) | stateful components' actions |
| `Engine.UploadHandler()` | `AssetBase + brick.UploadEndpoint` (`/_brick/upload`) | `brick:upload` ([Uploads](../forms/uploads.md)) |
| `Engine.IslandHandler()` | `AssetBase + brick.IslandEndpoint` (`/_brick/island`) | lazy islands, `brick:lazy` ([Islands](../interactivity/islands-lazy-and-streaming.md)) |
| `Engine.EventsHandler()` | `AssetBase + brick.EventsEndpoint` (`/_brick/events`) | server push, `listen:` |
| `assetshttp.Handler` ([contrib/assets-http](../../../contrib/assets-http/)) | `AssetBase` | the runtime and component CSS/JS that `{{ assets }}` links |

<!-- ds:block id=server-handler-f4ujmxxj title="examples/production-host-api/main.go" -->
**examples/production-host-api/main.go** · [`examples/production-host-api/main.go:134-142`](../../../examples/production-host-api/main.go#L134-L142)

```go
func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, s.eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return s.eng }))
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /preview/{name}", s.preview)
	mux.HandleFunc("GET /stats", s.stats)
	return mux
}
```
<!-- /ds:block hash=d553f9 -->

## Step 5 — Call an action from Go

`Engine.RunAction` runs the same pipeline as the action endpoint (store lookup, token and session check, middleware, the action, the re-render) and returns the whole `*brick.ActionResult`: the new HTML, the instance's version and the effects. `Engine.Instance` reads what the store now holds without changing it. Use them in your own client, a job, or a test:

<!-- snippet: examples/production-host-api/main_test.go lines=70-105 -->
```go
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
```

`Engine.Instance` is also what the dev server's instance inspector (`/_brick/instances`) is built on. A host can offer the same read to an admin page or a support tool; it returns copies and changes nothing:

<!-- ds:block id=inspect-756aqtra title="examples/production-host-api/main.go" -->
**examples/production-host-api/main.go** · [`examples/production-host-api/main.go:115-121`](../../../examples/production-host-api/main.go#L115-L121)

```go
func inspect(ctx context.Context, eng *brick.Engine, handle string) (component string, version int64, err error) {
	view, err := eng.Instance(ctx, handle)
	if err != nil {
		return "", 0, err
	}
	return view.Component, view.Version, nil
}
```
<!-- /ds:block hash=e70d46 -->

`Engine.RunUpload` is the same for a file. The handle and token come from the page's `data-brick-state` and `data-brick-token` attributes.

## Step 6 — Count what a page rendered (`WithStats`)

`Result.Stats` counts the components of the cached page, but an island renders later, inside `Fill`. Pass `brick.WithStats(&stats)` to `Fill` and it adds each island's components and loader calls to your struct. The dev toolbar does exactly this. Start from a copy of `Result.Stats`, because a cached `Result` is shared between requests and `Fill` never changes it.

<!-- ds:block id=pagestats-cxvaastn title="examples/production-host-api/main.go" -->
**examples/production-host-api/main.go** · [`examples/production-host-api/main.go:172-178`](../../../examples/production-host-api/main.go#L172-L178)

```go
func pageStats(ctx context.Context, eng *brick.Engine, page *brick.Result) (brick.RenderStats, error) {
	stats := page.Stats
	if _, err := eng.Fill(ctx, page, brick.WithStats(&stats)); err != nil {
		return brick.RenderStats{}, err
	}
	return stats, nil
}
```
<!-- /ds:block hash=61c4d2 -->

`content/team.html` holds an island, `Team`, that renders two `Greeting` components of its own:

<!-- snippet: examples/production-host-api/theme/components/Team.html -->
```html
---
brick:
  island: true
inject:
  user: {default: "guest"}
---
<ul class="team"><li><Greeting name="Ada" /></li><li><Greeting :name=".Ctx.user" /></li></ul>
```

The page renders `Team` once; `Fill` adds the two greetings, so the count goes from 1 to 3. The host exposes it at `/stats`:

```console
$ curl -s localhost:8086/stats
{"Components":3,"Types":[{"Name":"Greeting","Count":2},{"Name":"Team","Count":1}],"Islands":1,"LazyIslands":0,"LoaderCalls":0,"BatchCalls":0,"BatchItems":0}
```

## Try it

From `examples/`, print one page and exit:

```console
$ go run ./production-host-api -render static
<h1>Static</h1>
<p class="greeting">Hello, world!</p>
```

Or serve, then ask for a preview and the page with its island filled for one user:

```console
$ go run ./production-host-api
2026/10/14 10:08:40 serving on :8086
$ curl -s 'http://localhost:8086/preview/Greeting?name=Ada'
<p class="greeting">Hello, Ada!</p>
$ curl -s 'http://localhost:8086/?user=Ada'
<h1>Host API</h1>
<p class="greeting">Hello, Go!</p>
<p class="whoami">Signed in as Ada</p>
<div data-brick="bdc0d0d8a" data-brick-state="default.<handle>" data-brick-token="<token>" data-brick-name="Counter" class="counter"><button brick:click="bump">Clicked 0 times</button></div>
<brick-assets><script type="module" src="/_brick/c/runtime.0b4d3d08.js" integrity="sha384-1ojXDjndJ49gLs9LzFJ7HhqO9T6yLwLO3U8Y+zouOx6APv8CGhC1ifU41Z4HS9sK"></script></brick-assets>
```

The tests:

```console
$ go test -v ./production-host-api/
--- PASS: TestRender (0.01s)
--- PASS: TestHomeFillsIsland (0.00s)
    main_test.go:103: unknown action: brick: unknown action "nope" on <Counter> (status 400)
--- PASS: TestRunAction (0.00s)
    main_test.go:126: list: <ul><li><p class="greeting">Hello, Ada!</p></li><li><p class="greeting">Hello, Lin!</p></li><li><p class="greeting">Hello, Sam!</p></li></ul>
--- PASS: TestDeterministicAndConcurrent (0.00s)
    main_test.go:137: MaxComponents: content/list.html:1: <Greeting>: more than MaxComponents (5) components in one render
    main_test.go:142: MaxOutputBytes: components/Greeting.html: output passed MaxOutputBytes (64)
    main_test.go:148: MaxDepth: file=components/Nest.html code=limit chain=64
--- PASS: TestLimits (0.01s)
    main_test.go:175: stats: {"Components":3,"Types":[{"Name":"Greeting","Count":2},{"Name":"Team","Count":1}],"Islands":1,"LazyIslands":0,"LoaderCalls":0,"BatchCalls":0,"BatchItems":0}
--- PASS: TestPageStats (0.00s)
--- PASS: TestPinnedStateTokens (0.00s)
    main_test.go:227: unknown handle: 410 {"error":"brick: this component's state was not found; the page recovers it"}
    main_test.go:236: oversize: 400 bad action request
--- PASS: TestActionEnvelopeAndCap (0.00s)
--- PASS: TestInspect (0.00s)
    main_test.go:281: steps: components/Spin.html: load: spin: Traceback (most recent call last):
    main_test.go:286: timeout: components/Spin.html: load: spin: spin: over the time limit (1ms per call)
--- PASS: TestScriptLimits (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/production-host-api	0.070s
```

## Pitfalls

- **`RenderComponent` trims the trailing newline; `Render` keeps it.** `RenderComponent("Greeting", …)` returns `<p class="greeting">Hello, Ada!</p>` with no final `\n`, while a page ends with the file's newline. Compare accordingly in tests.
- **A page with stateful components needs a store.** With `Options.States` nil, rendering a page that holds a stateful component is an error; `NewMemoryStates(0, 0)` is enough for one process.
- **The action endpoint checks the request shape first.** A POST without the `Brick-Action` header or a JSON content type answers `400 an action is a JSON POST with the Brick-Action header`; a GET answers `405`. Proxies that strip custom headers break actions this way.
- **`RunAction` has no `*http.Request`.** `ActionCall.Request` is nil inside middleware and scripts, and `Options.Sessions` is not asked; pass `brick.WithSession` in the options when the instance is bound to a session, or it answers `ErrGone`.
- **Provide a value only when there is one.** `brick.Provide("user", "")` counts as provided and hides the component's `inject:` default; the example adds the option only for a non-empty `?user=`.
- **`WithStats` needs a copy.** Passing `&page.Stats` directly would add every request's islands to the shared cached `Result`; copy first, as `pageStats` does. Lazy islands render on their own request and are not counted.
- **A `Result` with islands is not a finished page.** `Result.Bytes()` on one returns `brick: this Result has islands; render it with Engine.Fill` (`brick.ErrIslands`); finish it with `Fill`, or `Stream` for streamed islands.

## Related

- [Limits and determinism](limits-determinism.md) · [Action middleware](action-middleware.md) · [bricktest](../tooling/bricktest.md)
- [Feature index: Go host API](../../reference/features.md#go-host-api)
- Example: [examples/production-host-api](../../../examples/production-host-api/) · the smaller [examples/engineonly](../../../examples/engineonly/)
