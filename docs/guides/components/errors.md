# When things go wrong

## What it is and when you want it

A typo should fail loudly, before anything renders; one slow or broken widget should not take the whole page down. brick does both. Mistakes it can see in the files (an unknown tag, a missing required prop, a bad prop value, an unbalanced tag) are reported with file and line when the file compiles, and `Engine.CompileAll` (what `brick check` runs) reports every one in a folder at once. Failures that happen while rendering (a query that fails, a script error) can be contained by a `<brick:boundary>`, which shows its fallback instead, while the error still reaches your Go program through an `OnError` hook. This page also covers `<brick:render>`, which picks a component by name from data, because an unknown name is exactly the kind of render-time failure a boundary is for.

## Before you start

- Read [Components and props](props.md) and [Scripts and loaders](loaders.md); the failing widget below is a component with a loader.
- Know which mode you run: `brick.Prod` (the default) never prints file paths or source lines into a page; `brick.Dev` shows errors in the page and marks every component's root with its source.

## Step 1 — Choose a component from data

A CMS sends a list of blocks. Instead of a growing `{{ if eq .type "Hero" }}` chain, `<brick:render>` takes the component's **name** and its props. ``omit . `type` `` drops the `type` key so components that check their props do not see it.

<!-- snippet: examples/components-errors/theme/content/index.html -->
```html
---
blocks:
  - {type: Hero, title: Welcome}
  - {type: Quote, text: "Less, but better."}
---
{{ range .Params.blocks }}<brick:render :name=".type" :props="omit . `type`" />
{{ end }}
<brick:boundary>
  <RecentPosts />
  <brick:prop name="fallback"><p class="fallback">Recent posts are unavailable. ({{ .Slot.Message }})</p></brick:prop>
</brick:boundary>
```

## Step 2 — Contain a failing component

`RecentPosts` has a loader that fails, as a database outage would:

<!-- snippet: examples/components-errors/theme/components/RecentPosts.html -->
```html
---
load: recent
---
<brick:script>
def recent(input):
    fail("the posts database is down")
</brick:script>
<ul>{{ range .Props.items }}<li>{{ . }}</li>{{ end }}</ul>
```

The `<brick:boundary>` around it (step 1) renders its `fallback` block instead, with the error as `.Slot.Message`. In Prod that message is generic plus an error id, so a database password inside an error can never reach the page; in Dev it is the real error.

## Step 3 — Hear every error in Go

A hook is any value with one of the hook methods, passed in `Options.Hooks`. An `OnError` hook hears every error, including the ones a boundary contained.

<!-- ds:block id=errorlog-x2sk245m title="examples/components-errors/main.go" -->
**examples/components-errors/main.go** · [`examples/components-errors/main.go:37-40`](../../../examples/components-errors/main.go#L37-L40)

```go
type errorLog struct {
	mu    sync.Mutex
	lines []string
}
```
<!-- /ds:block hash=fe73d8 -->

<!-- ds:block id=errorlog-onerror-qy4a2wad title="examples/components-errors/main.go" -->
**examples/components-errors/main.go** · [`examples/components-errors/main.go:45-49`](../../../examples/components-errors/main.go#L45-L49)

```go
func (l *errorLog) OnError(_ context.Context, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, err.Error())
}
```
<!-- /ds:block hash=f55e42 -->

<!-- ds:block id=newengine-cv2eejr5 title="examples/components-errors/main.go" -->
**examples/components-errors/main.go** · [`examples/components-errors/main.go:53-63`](../../../examples/components-errors/main.go#L53-L63)

```go
func newEngine(dir string, mode brick.Mode, hook *errorLog) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{FS: root.FS(), Scripting: sc, Mode: mode, Hooks: []any{hook}})
}
```
<!-- /ds:block hash=9dc28f -->

A value that implements no hook method is an error from `New`, so a misspelled method can never silently never run.

## Step 4 — Check a whole folder before it ships

`CompileAll` compiles every page under a folder and returns diagnostics with file, line and column. Errors stop a page; warnings (such as an element left open) never change what renders.

<!-- ds:block id=check-8rd5dky7 title="examples/components-errors/main.go" -->
**examples/components-errors/main.go** · [`examples/components-errors/main.go:68-78`](../../../examples/components-errors/main.go#L68-L78)

```go
func check(ctx context.Context, eng *brick.Engine) (string, error) {
	rep, err := eng.CompileAll(ctx, brokenDir)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(rep.Diagnostics))
	for _, d := range rep.Diagnostics {
		lines = append(lines, d.String())
	}
	return strings.Join(lines, "\n"), nil
}
```
<!-- /ds:block hash=54ca6a -->

<!-- snippet: examples/components-errors/theme/broken/typo.html -->
```html
<main>
  <Crad title="Hi" />
</main>
```

<!-- snippet: examples/components-errors/theme/broken/unbalanced.html -->
```html
<main>
  <div class="box">
    <p>An open div.</p>
</main>
```

From a terminal, `brick check` runs the same check over a project's pages. Put the two broken files above in a project's `content/` folder and run it:

```console
$ brick check .
content/typo.html:2:3: error: unknown component <Crad> — no file in use: or the roots (components) defines it
content/unbalanced.html:4:1: warning: </main> closes <div> (line 2), which is still open
$ echo $?
1
```

Each line is `file:line:col: severity: message`. The exit code is 1 when any error is reported and 0 when there are only warnings, so the command is a CI gate; `-json` prints the same diagnostics as JSON for tools. A clean project prints `ok: N pages`.

## Step 5 — Watch compiles and renders with other hooks

`OnError` is one of five hook interfaces. `CompileHook.OnCompile` sees each file once per snapshot (its path, the component tags it uses and its frontmatter), `BeforeRenderHook` can stop a page before it renders, and `AfterRenderHook.AfterRender` sees the finished page. One value may implement several; this one counts what the engine compiled and rendered, as a metrics feed would:

<!-- ds:block id=counter-abndry84 title="examples/components-options/main.go" -->
**examples/components-options/main.go** · [`examples/components-options/main.go:64-68`](../../../examples/components-options/main.go#L64-L68)

```go
type counter struct {
	mu       sync.Mutex
	compiled []string
	rendered []string
}
```
<!-- /ds:block hash=b813e3 -->

<!-- ds:block id=counter-oncompile-n9naz422 title="examples/components-options/main.go" -->
**examples/components-options/main.go** · [`examples/components-options/main.go:72-76`](../../../examples/components-options/main.go#L72-L76)

```go
func (c *counter) OnCompile(_ context.Context, f *brick.CompiledFile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.compiled = append(c.compiled, f.Path)
}
```
<!-- /ds:block hash=c72911 -->

<!-- ds:block id=counter-afterrender-utdqgruk title="examples/components-options/main.go" -->
**examples/components-options/main.go** · [`examples/components-options/main.go:80-85`](../../../examples/components-options/main.go#L80-L85)

```go
func (c *counter) AfterRender(_ context.Context, p *brick.Page, _ *brick.Result) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rendered = append(c.rendered, p.Path)
	return nil
}
```
<!-- /ds:block hash=67ad55 -->

It is passed in `Options.Hooks`, as in step 3 (`newEngine` in [Components and props](props.md#step-5--find-components-in-more-than-one-folder)). After the example's pages the program prints:

<!-- snippet: examples/components-options/testdata/output.txt lines=79-80 -->
```text
== hooks
compiled 9 files, rendered 4 pages
```

## Step 6 — Read the unbalanced-HTML warning and the source location

An element left open does not fail a render, so brick reports it as a warning with the code `html-balance`. Print the code to filter on it: a host can fail its build on that code, or ignore it.

<!-- snippet: examples/components-options/theme/broken/unbalanced.html -->
```html
<main>
  <section>
    <p>The section is never closed.</p>
</main>
```

<!-- ds:block id=check-rfdkhfjq title="examples/components-options/main.go" -->
**examples/components-options/main.go** · [`examples/components-options/main.go:150-162`](../../../examples/components-options/main.go#L150-L162)

```go
func check(ctx context.Context, w io.Writer, eng *brick.Engine) error {
	rep, err := eng.CompileAll(ctx, "broken")
	if err != nil {
		return err
	}
	for _, d := range rep.Diagnostics {
		if _, err := fmt.Fprintf(w, "== %s [%s]\n", d, d.Code); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(w)
	return err
}
```
<!-- /ds:block hash=b3dd31 -->

<!-- snippet: examples/components-options/testdata/output.txt lines=74-74 -->
```text
== broken/unbalanced.html:4:1: warning: </main> closes <section> (line 2), which is still open [html-balance]
```

In Dev mode every component with one root element writes where it came from as `data-brick-loc="file:line"` on that element, so the browser's inspector leads straight to the source file (`components-options` renders `content/loc.html` in Dev mode):

<!-- snippet: examples/components-options/testdata/output.txt lines=76-77 -->
```text
== content/loc.html
<aside data-brick-loc="shared/Note.html:1" class="note">located</aside>
```

## Step 7 — Count what a render did

`Result.Stats` counts the components a page render drew and the islands it captured. An island renders later, in `Engine.Fill`, so its components are counted only if you pass `WithStats` a copy of the stats (a cached `Result` is shared, and `Fill` never changes it). The Dev toolbar shows these numbers; a host can log them.

<!-- ds:block id=stats-5hymhkv8 title="examples/components-options/main.go" -->
**examples/components-options/main.go** · [`examples/components-options/main.go:208-219`](../../../examples/components-options/main.go#L208-L219)

```go
func stats(ctx context.Context, w io.Writer, eng *brick.Engine) error {
	res, err := eng.RenderChunks(ctx, "content/island.html", nil)
	if err != nil {
		return err
	}
	st := res.Stats
	if _, err := eng.Fill(ctx, res, brick.WithStats(&st)); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "== WithStats\npage render: %d components, %d islands\nafter Fill:  %d components, top %v\n\n", res.Stats.Components, res.Stats.Islands, st.Components, st.Top(1))
	return err
}
```
<!-- /ds:block hash=a4eb07 -->

<!-- snippet: examples/components-options/testdata/output.txt lines=86-88 -->
```text
== WithStats
page render: 2 components, 2 islands
after Fill:  4 components, top [{Badge 2}]
```

## Try it

```sh
cd examples/components-errors
go run .
go test .
```

The program renders the page in Prod and then Dev mode, prints what the hook heard after each, then the check report. In Dev, the error also shows as a visible block, and each single-root component carries `data-brick-loc` naming its file and line.

<!-- snippet: examples/components-errors/testdata/output.txt -->
```text
== content/index.html in Prod mode
<section class="hero"><h1>Welcome</h1></section>
<blockquote>Less, but better.</blockquote>

<p class="fallback">Recent posts are unavailable. (This part of the page could not be shown. (error 08587a4e))</p>

== OnError heard
components/RecentPosts.html: load: recent: Traceback (most recent call last):
  components/RecentPosts.html#script:6:9: in recent
Error in fail: fail: the posts database is down

== content/index.html in Dev mode
<section data-brick-loc="components/Hero.html:5" class="hero"><h1>Welcome</h1></section>
<blockquote data-brick-loc="components/Quote.html:5">Less, but better.</blockquote>

<pre class="brick-error" style="background:#fff1f0;color:#a8071a;border:1px solid #ffa39e;padding:12px;white-space:pre-wrap;font:13px/1.5 monospace">components/RecentPosts.html: load: recent: Traceback (most recent call last):
  components/RecentPosts.html#script:6:9: in recent
Error in fail: fail: the posts database is down</pre><p class="fallback">Recent posts are unavailable. (components/RecentPosts.html: load: recent: Traceback (most recent call last):
  components/RecentPosts.html#script:6:9: in recent
Error in fail: fail: the posts database is down)</p>

== OnError heard
components/RecentPosts.html: load: recent: Traceback (most recent call last):
  components/RecentPosts.html#script:6:9: in recent
Error in fail: fail: the posts database is down

== CompileAll("broken")
broken/typo.html:2:3: error: unknown component <Crad> — no file in use: or the roots (components) defines it
broken/unbalanced.html:4:1: warning: </main> closes <div> (line 2), which is still open
```

## Pitfalls

- **A boundary contains render-time failures only.** A typo is still a compile error: `<brick:boundary><Crad />…</brick:boundary>` fails with `unknown component <Crad> — no file in use: or the roots (components) defines it`, as it should.
- **`<brick:render>` takes a name, never a path.** `:name="`../x`"` fails with `name "../x": <brick:render> takes a component name, never a path`, so CMS data can only reach components your theme already has. An unknown name fails at render with `no component named "Gallery" (written in content/probe.html)`; inside a boundary that is contained like any render failure.
- **Prod hides the message, not the error.** The page shows `This part of the page could not be shown. (error 08587a4e)`; the hook and your logs get the full error. Match the two by the id.
- **A loader's answer is not an error.** `brick.redirect`, `brick.error` and `brick.not_found()` pass straight through a boundary and never reach `OnError`; see [Scripts and loaders](loaders.md#step-5--answer-with-a-redirect-a-status-or-a-header).
- **A cancelled request stops.** A boundary never swallows cancellation, so a gone visitor does not get a page full of fallbacks.
- **The unbalanced-HTML check is lenient on purpose.** `<br>` and `<img>` need no close; `<li>`, `<td>`, `<p>` and `<option>` may stay open; a wrapper opened in one `{{ if }}` and closed in a matching one is fine. A `{{ range }}` body and a component's body must each close what they open.
- **Recursion needs a way to stop.** A component that renders itself outside every `{{ if }}` and `{{ range }}` fails at compile with `<Loop> renders itself outside every {{ if }}/{{ range }}, so it can never stop (C14); put the recursion under a condition`. Render limits default to depth 64, 100,000 components and 32 MiB of output per render (`Options.MaxDepth`, `MaxComponents`, `MaxOutputBytes`).
- **`data-brick-loc` appears only in Dev, and only on a single root.** A component with several top-level elements, or a root chosen by `{{ if }}`, is not marked.
- **Hooks have rules.** `AfterRender` may change a page's static chunks but must leave marker chunks as they are; a hook that implements none of the five interfaces is refused by `New`.
- **`html-balance` is a warning, always.** It never fails `CompileAll` and never changes output; it follows `{{ if }}` branches separately and gives up silently on markup it cannot follow, so a missing warning is not proof the HTML is balanced.
- **`WithStats` needs a copy.** `st := res.Stats; eng.Fill(ctx, res, brick.WithStats(&st))`; passing a pointer into a cached `Result` would count one page's islands on top of the next request's. Lazy islands render on their own request and are not counted.

## Related

- [Scripts and loaders](loaders.md): loaders, and the answers a boundary never contains.
- [Styles and client code](styles-and-client.md): a failed boundary does not load the failed component's JavaScript.
- [Frontmatter reference](../../reference/frontmatter.md).
- [Feature index](../../reference/features.md): the "Errors and diagnostics" rows, and "Dynamic components".
- The single-page read: [Choosing a component from data](../../guide.md#choosing-a-component-from-data), [When things go wrong](../../guide.md#when-things-go-wrong), [For your Go app](../../guide.md#for-your-go-app).
- Example: [examples/components-errors](../../../examples/components-errors/).
- Example: [examples/components-options](../../../examples/components-options/) for the settings added in the later steps.
