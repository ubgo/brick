# Limits and determinism

## What it is and when you want it

Two guarantees make brick safe to run under load. **Limits**: every render is bounded in depth, component count, output size and file size, so one bad record or a component that renders itself cannot exhaust memory; actions, effects, sealed blobs and streamed islands have their own bounds. **Determinism**: the same input gives the same bytes, and one `*Engine` serves any number of concurrent renders. Limits matter when a page renders data you do not control; determinism is what makes caching, golden-file tests and content-hashed asset URLs work.

## Before you start

- An engine built with `brick.New` ([The Go host API](host-api.md)).
- The example this page uses is [examples/production-host-api](../../../examples/production-host-api/); its tests pin every message shown below.

## Step 1 — Know the defaults

Every limit is an `Options` field (or a field of the store that owns it); zero means the default.

| Limit | Default | What it bounds | Over it |
|---|---|---|---|
| `MaxDepth` | 64 (`DefaultMaxDepth`) | components nested inside each other | `render depth passed MaxDepth (64) — a component that renders itself without stopping?` |
| `MaxComponents` | 100,000 (`DefaultMaxComponents`) | components in one render | `more than MaxComponents (N) components in one render` |
| `MaxOutputBytes` | 32 MiB (`DefaultMaxOutputBytes`) | the bytes one render writes | `output passed MaxOutputBytes (N)` |
| `MaxFileBytes` | 8 MiB (`DefaultMaxFileBytes`) | one theme or data file read | `<file> is N bytes, over MaxFileBytes (M)` |
| `MaxActionBytes` | 1 MiB (`DefaultMaxActionBytes`) | one action's JSON or form post | `400 bad action request` |
| `MaxEffectBytes` | 32 KiB (`DefaultMaxEffectBytes`) | the effects one action records | the action fails |
| `StreamWorkers` | 4 (`DefaultStreamWorkers`) | streamed islands of one response rendering at once | they wait their turn |
| `SealedOptions.MaxBytes` | 64 KiB (`DefaultSealedMaxBytes`) | one sealed state blob | the render fails, naming the component |
| `MaxImagePixels` | 40 million | an image `{{ image }}` decodes | refused before decoding |

A limit failure is a `*brick.Error` with `Code == brick.CodeLimit`, the file, and the component chain from the page down, so a log line says where it happened.

## Step 2 — Lower the render limits for data you do not control

The defaults are generous for hand-written pages. A page that ranges over user data should cap what one request can cost. The example takes its limits as a parameter:

<!-- ds:block id=limits-ud5w8x9n title="examples/production-host-api/main.go" -->
**examples/production-host-api/main.go** · [`examples/production-host-api/main.go:40-51`](../../../examples/production-host-api/main.go#L40-L51)

```go
type limits struct {
	MaxComponents  int
	MaxOutputBytes int
	// MaxActionBytes caps an action request's JSON body; zero keeps the
	// default (brick.DefaultMaxActionBytes).
	MaxActionBytes int
	// ScriptSteps and ScriptTimeout bound one script call (a loader or an
	// action function); zero keeps the Starlark runtime's own defaults
	// (scriptstarlark.DefaultSteps, DefaultTimeout).
	ScriptSteps   uint64
	ScriptTimeout time.Duration
}
```
<!-- /ds:block hash=308a7c -->

and renders a list with one component per name:

<!-- snippet: examples/production-host-api/theme/content/list.html -->
```html
<ul>{{ range .Props.names }}<li><Greeting :name="." /></li>{{ end }}</ul>
```

<!-- snippet: examples/production-host-api/main_test.go lines=129-149 -->
```go
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
```

`MaxDepth` catches recursion the compiler cannot. A component that calls itself unconditionally is refused at compile time; one that recurses under a condition that never becomes false is stopped at render:

<!-- snippet: examples/production-host-api/theme/components/Nest.html -->
```html
---
props:
  depth: {type: int, default: 0}
---
<div class="nest">{{ if lt .Props.depth 1000 }}<Nest :depth="add .Props.depth 1" />{{ end }}</div>
```

## Step 3 — Rely on same input, same bytes

A render's output depends only on the files, the props and the render options: map keys are walked in sorted order, dates use `Options.TimeZone` (UTC by default), and asset URLs are content hashes. One engine is safe for concurrent renders; the example renders the same page from 32 goroutines and compares the bytes:

<!-- snippet: examples/production-host-api/main_test.go lines=107-127 -->
```go
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
```

What *does* change per render is by design and lives only in state attributes: a stateful component's `data-brick-state` handle and `data-brick-token` are random per render (`Options.Rand` and `Options.Clock` replace the sources in tests, Step 5), and a sealed handle changes on every save.

## Step 4 — Cap action bodies and shape their errors

`Options.MaxActionBytes` bounds one action request; `Options.ActionErrors` replaces the status and body of a failed action when your gateway expects its own envelope. The example sets both in the one function that assembles its options:

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

The envelope function receives the error brick would have used. `brick.ActionStatus` gives the status brick would have chosen, so the host keeps the codes and changes only the body:

<!-- ds:block id=actionerror-cy5tkee6 title="examples/production-host-api/main.go" -->
**examples/production-host-api/main.go** · [`examples/production-host-api/main.go:105-109`](../../../examples/production-host-api/main.go#L105-L109)

```go
func actionError(w http.ResponseWriter, _ *http.Request, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(brick.ActionStatus(err))
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
```
<!-- /ds:block hash=00bde5 -->

The test posts an action for a handle that does not exist, then the same request padded past a 64-byte cap:

```console
$ go test -v -run Envelope ./production-host-api/
=== RUN   TestActionEnvelopeAndCap
    main_test.go:227: unknown handle: 410 {"error":"brick: this component's state was not found; the page recovers it"}
    main_test.go:236: oversize: 400 bad action request
--- PASS: TestActionEnvelopeAndCap (0.00s)
```

## Step 5 — Pin the random source and the clock in tests

A stateful component's `data-brick-token` is random per render, so a golden file of such a page never matches twice. `Options.Rand` and `Options.Clock` replace the sources; with the same seed two engines print the same token:

<!-- ds:block id=newpinnedengine-krcch6ug title="examples/production-host-api/main.go" -->
**examples/production-host-api/main.go** · [`examples/production-host-api/main.go:73-75`](../../../examples/production-host-api/main.go#L73-L75)

```go
func newPinnedEngine(dir string, src sources) (*brick.Engine, error) {
	return buildEngine(dir, limits{}, src)
}
```
<!-- /ds:block hash=531609 -->

```console
$ go test -v -run Pinned ./production-host-api/
=== RUN   TestPinnedStateTokens
--- PASS: TestPinnedStateTokens (0.00s)
```

## Step 6 — Bound script steps and time

A loader or action function is Starlark code, and `scriptstarlark.Options` bounds every call of it: `Steps` caps the interpreter's execution steps and `Timeout` the wall-clock time (the request's own deadline wins when sooner). Zero keeps the defaults, 1,000,000 steps and 2 seconds, never "unlimited". The example passes both through `limits`, in the `buildEngine` of Step 4: `scriptstarlark.New(scriptstarlark.Options{Steps: l.ScriptSteps, Timeout: l.ScriptTimeout})`. The component loops as many times as its `rounds` prop says:

<!-- snippet: examples/production-host-api/theme/components/Spin.html -->
```html
---
load: spin
props:
  rounds: {type: int, default: 100}
---
<brick:script>
def spin(input):
    total = 0
    for i in range(input["props"]["rounds"]):
        total += i
    return {"total": total}
</brick:script>
<p class="spin">{{ .Props.total }}</p>
```

```console
$ go test -v -run ScriptLimits ./production-host-api/
=== RUN   TestScriptLimits
    main_test.go:281: steps: components/Spin.html: load: spin: Traceback (most recent call last):
          components/Spin.html#script:10:15: in spin
        Error: Starlark computation cancelled: too many steps
    main_test.go:286: timeout: components/Spin.html: load: spin: spin: over the time limit (1ms per call)
--- PASS: TestScriptLimits (0.00s)
```

## Try it

From `examples/`:

```console
$ go test -v -run 'Limits|Deterministic' ./production-host-api/
=== RUN   TestDeterministicAndConcurrent
    main_test.go:126: list: <ul><li><p class="greeting">Hello, Ada!</p></li><li><p class="greeting">Hello, Lin!</p></li><li><p class="greeting">Hello, Sam!</p></li></ul>
--- PASS: TestDeterministicAndConcurrent (0.00s)
=== RUN   TestLimits
    main_test.go:137: MaxComponents: content/list.html:1: <Greeting>: more than MaxComponents (5) components in one render
    main_test.go:142: MaxOutputBytes: components/Greeting.html: output passed MaxOutputBytes (64)
    main_test.go:148: MaxDepth: file=components/Nest.html code=limit chain=64
--- PASS: TestLimits (0.01s)
PASS
ok  	github.com/ubgo/brick/examples/production-host-api	0.025s
```

And the compile-time refusal of unconditional recursion, from a theme whose `Nest.html` was `<div class="nest"><Nest /></div>`:

```console
components/Nest.html:1:19: <Nest> renders itself outside every {{ if }}/{{ range }}, so it can never stop (C14); put the recursion under a condition
```

## Pitfalls

- **Your functions must be pure too.** `Options.Funcs` run inside renders that may be concurrent and cached: a function that reads the clock, a global counter or the network breaks "same input, same bytes" and races. Pass changing values in as props or render options.
- **`StyleTransform` is hashed into the asset URL.** A transform that is not deterministic (a timestamp banner, a random class) changes the URL on every compile and defeats browser caching.
- **A limit stops the whole render.** It is not contained by `<brick:boundary>` the way a component's runtime error is; size limits to what a correct page can reach, not to the average.
- **`MaxActionBytes` truncates, then fails to decode.** An oversized action answers `400 bad action request`, the same as malformed JSON; raise it for forms that legitimately send large deferred fields.
- **A script limit is per call, not per page.** A page that calls a loader in a loop can spend `Steps` times the number of calls; lower `MaxComponents` too, or use a batch loader ([loaders](../components/loaders.md)).
- **Pinning `Rand` fixes the token, not the handle.** The handle (`data-brick-state="default.<id>"`) is minted by the state store, and `NewMemoryStates` draws it from its own random source, so a golden file of a stateful page still needs the handle masked; `TestPinnedStateTokens` compares tokens only.
- **Golden tests need fixed state tokens.** Pages with stateful components carry random handles; pin `Options.Rand` (and `Options.Clock` for sealed blobs) in tests, or compare a component without state, as `bricktest.Golden` examples do ([bricktest](../tooling/bricktest.md)).

## Related

- [The Go host API](host-api.md) · [Sealed state](sealed-state.md) · [bricktest](../tooling/bricktest.md)
- [Feature index: Go host API](../../reference/features.md#go-host-api) and [Errors and diagnostics](../../reference/features.md#errors-and-diagnostics)
- Example: [examples/production-host-api](../../../examples/production-host-api/)
