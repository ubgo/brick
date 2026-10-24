# The dev server: live reload, playground and toolbar

## What it is and when you want it

`Dev: true` (what `brick serve` runs) turns a site into a development server: it watches the project and reloads open pages when a file changes, shows render errors inside the page, serves any component on its own at `/_brick/play/<Name>`, and adds a small toolbar saying what each request did. You want it while you write; production servers leave it off and none of it exists there.

## Before you start

- Read [pages and routes](pages-and-routes.md); this page uses [`examples/site-blog`](../../../examples/site-blog/) and its `PostLink` component.
- The `brick` command needs no Go code: `brick serve theme` is Dev with drafts on.

## Step 1 — Turn Dev on

`Dev` is one field of `site.Options`; `ListenAndServe` then also watches the files. The example's `-dev` flag sets it:

<!-- snippet: examples/site-blog/main.go lines=42-47 -->
```go
// ds:def id=newsitefs-5nq2ppfu
func newSiteFS(ctx context.Context, fsys fs.FS, dev, drafts bool) (*site.Site, error) {
	return site.New(ctx, site.Options{
		FS:     fsys,
		Engine: brick.Options{Markdown: markdowngoldmark.New(markdowngoldmark.WithHeadingIDs())},
		Dev:    dev,
```

```sh
cd examples/site-blog
go run . -dev -drafts       # or: brick serve theme
```

## Step 2 — Edit and watch the page follow

Open `http://localhost:8090/blog/`, then change `components/PostLink.html`. Each Dev page carries a small script that listens on `/_brick/live`; the server evicts only the pages that read the changed file and tells the browsers showing them. With navigation on (`brick.Options{Navigation: true}`), the page morphs in place, keeping scroll position and what you typed; it reloads fully when the new page needs a script or stylesheet the old one never ran, or when the render failed. Without navigation it always reloads fully.

Your own code can ask for the same thing. `brick.refresh()` re-renders the current page in place and emits a `refreshed` event; it exists only with navigation on, so a button that wants a refresh falls back to a full reload otherwise. `examples/site-blog/theme/static/refresh.js`:

<!-- snippet: examples/site-blog/theme/static/refresh.js -->
```js
// Re-render the page you are looking at, the way the dev server's live
// reload does: with navigation on, brick.refresh() fetches the page again
// and morphs it in place (scroll, focus and typed input survive); without
// it brick.refresh does not exist, so fall back to a full reload.
export function refreshNow() {
  if (window.brick && brick.refresh) return brick.refresh()
  location.reload()
}
```

## Step 3 — Try a component on its own

Query values become text props, `children=` is the body and `lang=` the locale:

```sh
curl -s 'localhost:8090/_brick/play/PostLink?title=Hi&url=/x/'
```

```html
<!doctype html><html><head><meta charset="utf-8"><title>PostLink · playground</title></head><body><a data-brick-loc="components/PostLink.html:6" class="post" href="/x/">Hi</a><script>…live reload…</script></body></html>
```

`data-brick-loc` names the file and line that wrote each element (Dev only), so the browser's inspector leads straight to the source.

## Step 4 — Read the toolbar

Every Dev page gets a "brick" pill in the bottom-left corner. Its summary for `/blog/`, first request and second:

```text
brick 1.57ms · 2 comp · cache miss
brick 4µs · 2 comp · cache hit
```

Opened, it lists the render time, whether the page came from the [render cache](render-cache.md), how many components, islands and loader calls ran, the five most used components (each linked to its playground), and a link to `/_brick/instances`, which lists the stateful component instances the state stores hold (never their values). A host that renders pages itself reads the same numbers from `Result.Stats`.

The instance inspector is a plain page. With no stateful component rendered yet it says so (in `examples/site-blog`, which has none):

```console
$ curl -s localhost:8090/_brick/instances | sed -n 3,4p
<h1>Live instances</h1>
<p>0 instance(s). Dev only: state values and session ids are never shown.</p>
```

## Step 5 — Read the same numbers from Go

The toolbar's counts are `brick.RenderStats`. A host that renders pages itself gets the page's counts in `Result.Stats` and adds what each island rendered by passing `brick.WithStats` to `Engine.Fill` (a copy of the stats, because a cached `Result` is shared). From [`examples/production-host-api`](../../../examples/production-host-api/):

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

## Step 6 — Watch the files from your own server

`ListenAndServe` in Dev polls the project for you. With your own `http.Server`, run `Site.Watch` beside `s.Handler()`; it drops the pages that read a changed file and tells open browsers until the context ends. `examples/site-blog/ops.go` and its `-watch` flag:

<!-- ds:block id=watchsite-q2d3wh44 title="examples/site-blog/ops.go" -->
**examples/site-blog/ops.go** · [`examples/site-blog/ops.go:82-84`](../../../examples/site-blog/ops.go#L82-L84)

```go
func watchSite(ctx context.Context, s *site.Site, every time.Duration) error {
	return s.Watch(ctx, watch.Options{Interval: every})
}
```
<!-- /ds:block hash=15cea6 -->

```sh
cd examples/site-blog
go run . -watch             # no Dev: no toolbar, no playground, but edits still reach the served pages
```

`TestWatchSite` writes a changed `about.html` into a copy of the theme and waits for `/about/` to show it.

## Try it

`go test .` in `examples/site-blog` runs `TestDevServer` (toolbar, live-reload script, playground link and `data-brick-loc` present in Dev; the playground is a 404 without Dev) and `TestDevInlineError`. A page with `<p>{{ len 3 }}</p>` answers 500 in both modes; in Dev the body shows where it broke:

```html
<main><p><pre class="brick-error" style="background:#fff1f0;color:#a8071a;border:1px solid #ffa39e;padding:12px;white-space:pre-wrap;font:13px/1.5 monospace">content/broken.html:1:7: at &lt;len 3&gt;: error calling len: len of type int</pre>
```

Without Dev the body is only `Internal Server Error`, so nothing about your files reaches a visitor.

## Pitfalls

- **Never run Dev in production.** It serves the playground (any component with any props), the instance inspector and file paths in error pages. Every Dev route answers 404 when `Dev` is false.
- **The playground takes text props only.** Every query value is a string, as if written as an attribute; a prop typed `int` or `list` needs a page to test it.
- **The toolbar's lazy-island timings need a secure origin.** Browsers expose `Server-Timing` only over https or on `localhost`, so a Dev server opened on a LAN address shows only how many lazy islands are still waiting.
- **Live reload is per URL, until it cannot be.** A page that reads a changed file is reloaded; a change to `site.yaml`, `data/` or `i18n/`, a new or deleted file, or an edit that changes the page index (a title, a date, a URL) clears the whole cache and reloads every page.
- **Your own Go server needs `ListenAndServe` (or `Watch`) to reload.** `s.Handler()` alone serves the pages but watches nothing; run `go s.Watch(ctx, watch.Options{})` beside it (it polls until `ctx` ends).

## Related

- [Render cache](render-cache.md) · [Pages and routes](pages-and-routes.md) · [Static export](static-export.md)
- [Feature index](../../reference/features.md) rows: dev server and live reload (`brick.refresh`), component playground, dev toolbar and render stats (`WithStats`), instance inspector.
- Long-form: [guide › A whole site](../../guide.md#a-whole-site--urls-serving-building).
- Example: [`examples/site-blog`](../../../examples/site-blog/).
