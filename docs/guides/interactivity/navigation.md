# Navigation without reloads

## What it is and when you want it

With `navigation: true`, internal links and forms fetch the next page and patch only what changed, instead of reloading the whole document: the header does not flicker, a playing audio element keeps playing, a sidebar keeps its scroll. When the next page has the same layout, the server sends only the page part (fragment navigation), so a navigation costs a fraction of a full page. A few attributes refine it: `brick:current` marks the active link, `brick:prefetch` loads a page before the click, `brick:persist` and `brick:scroll` keep an element and a scroll position across pages, and `brick:nav="off"` or `"replace"` changes how one link behaves. You want it on any multi-page site where full reloads feel slow.

## Before you start

- No server change is needed: the browser fetches the same page a normal visit gets, and anything unusual falls back to a normal page load.
- Fragment navigation is a site-layer feature (`site.Options`, `brick serve`). A Go host using the engine alone gets in-place navigation but always sends the full page.
- The example is [`examples/interactivity-navigation`](../../../examples/interactivity-navigation/).

## Step 1 — Turn navigation on

`brick.yaml`:

<!-- snippet: examples/interactivity-navigation/theme/brick.yaml -->
```yaml
navigation: true
```

`brick serve` and `brick build` read `brick.yaml` themselves. A Go host loads it with `brick.LoadConfig` and copies it into the engine options with `Config.Apply` (or sets `brick.Options.Navigation: true` directly). From `examples/interactivity-navigation/main.go`:

<!-- ds:block id=newsite-2kuuspzw title="examples/interactivity-navigation/main.go" -->
**examples/interactivity-navigation/main.go** · [`examples/interactivity-navigation/main.go:32-46`](../../../examples/interactivity-navigation/main.go#L32-L46)

```go
func newSite(ctx context.Context, dir string) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	cfg, err := brick.LoadConfig(root.FS(), configFile)
	if err != nil {
		return nil, err
	}
	var eng brick.Options
	if err := cfg.Apply(&eng); err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{FS: root.FS(), Engine: eng})
}
```
<!-- /ds:block hash=a9d235 -->

## Step 2 — Mark up the layout

Every page in the example uses this one layout, which is what lets the server answer a navigation with only the page. `layouts/default.html`:

<!-- snippet: examples/interactivity-navigation/theme/layouts/default.html -->
```html
<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>{{ .Title }}</title>
<style>html[data-navigating] #bar { display: block } [data-current] { font-weight: bold }</style>
</head>
<body>
<div id="bar" hidden>Loading…</div>
<header>
  <a href="/" brick:current.exact brick:prefetch>Home</a>
  <a href="/docs/" brick:current brick:prefetch>Docs</a>
  <a href="/docs/intro/" brick:prefetch="visible">Intro</a>
  <a href="/report.csv" brick:nav="off">Download</a>
</header>
<aside id="toc" brick:scroll="docs-nav">…a long list of links…</aside>
<main>{{ children }}</main>
<audio id="player" brick:persist controls></audio>
{{ assets }}
</body>
</html>
```

What each attribute does in the browser:

| Attribute | Effect |
|---|---|
| `brick:current` | While its page is shown, the runtime sets `aria-current="page"` and `data-current` when the link's path is the page's or a segment prefix of it (`/docs/` on `/docs/intro/`, never on `/docsearch`). `.exact` needs the paths to be equal; a link to `/` matches only `/`. |
| `brick:prefetch` | Loads the page after 65 ms of hover, and at once on keyboard focus or a press, so the click is instant. `="visible"` loads it when it scrolls into view. |
| `brick:nav="off"` | A normal full load: downloads, sign-in, anything that must leave the page. On a link, a form or any ancestor. `="replace"` swaps the history entry instead of adding one. |
| `brick:scroll="docs-nav"` | The next page's element with the same name starts where this one was; Back and Forward put it back where it was on that page. Keyed by the name, so two layouts can share one position. |
| `brick:persist` | Keeps the element (it needs an `id`) across pages even when the new page moves it: an audio player, a chat box. |
| `html[data-navigating]` | Set on `<html>` when a navigation takes longer than 150 ms, for a progress bar of your own. |

Pages are ordinary content files, such as `content/docs/intro.html`:

<!-- snippet: examples/interactivity-navigation/theme/content/docs/intro.html -->
```html
---
title: Intro
---
<h1>Intro</h1>
<p>The first page of the docs.</p>
```

## Step 3 — Listen and cancel from code

Two runtime events cover analytics and "unsaved changes" guards, in a `<brick:client>` block or any module on the page:

```js
brick.on("navigated", ({ url }) => analytics.page(url));
brick.on("navigate", (d) => { if (unsaved && !confirm("Leave?")) d.preventDefault(); });
brick.navigate("/docs/", { replace: true });
```

A cancelled Back puts the shown URL back in the address bar. After a navigation the new title is announced to screen readers and focus moves to `<main>`.

## Try it

```sh
cd examples/interactivity-navigation && go run .
```

The first visit, `curl -si localhost:8094/docs/intro/`, is the whole page with the runtime, `nav.js` and the layout's fingerprint in a header and a meta tag:

```text
HTTP/1.1 200 OK
Brick-Layout: 147fb98811c484b98f3478abcb9114c9
Content-Type: text/html; charset=utf-8
Vary: Brick-Nav
```

A navigation sends that fingerprint back as `Brick-Nav`. Because `/docs/` has the same layout, the answer is only the page, 414 bytes instead of 1765 (`curl -si -H 'Brick-Nav: 147fb98811c484b98f3478abcb9114c9' localhost:8094/docs/`):

```text
HTTP/1.1 200 OK
Brick-Fragment: 1
Brick-Layout: 147fb98811c484b98f3478abcb9114c9
Content-Type: text/html; charset=utf-8
Vary: Brick-Nav
Content-Length: 414

<!doctype html><html><head><title>Docs</title></head><body><brick-assets><script type="module" src="/_brick/c/runtime.0b4d3d08.js" integrity="sha384-…"></script><script type="module" src="/_brick/c/nav.78666221.js" integrity="sha384-…"></script></brick-assets><h1>Docs</h1>
</body></html>
```

`nav.js` splices that between its own marks, so the header, the persisted player and the sidebar's DOM are never touched. A fingerprint that does not match gets the full page. `cd examples && go test ./interactivity-navigation/` checks all three answers.

## Pitfalls

- **A server-marked active link breaks fragment navigation.** The layout is compared by its rendered bytes, so a sidebar that adds `class="active"` to the current page's link on the server makes every page's layout different, and every navigation gets the full page. Mark it with `brick:current` instead; the fingerprint ignores the `aria-current` and `data-current` attributes on `brick:current` links, so you may also render `aria-current` server-side for readers without JavaScript.
- **`brick.yaml` is not read by `site.New` or `brick.New`.** Only the `brick` command reads it. Without `LoadConfig` and `Apply` (or `Options.Navigation`) in a Go host, the page ships no `nav.js` and links do full loads.
- **`brick:scroll` needs a literal name** (letters, digits, `-` and `_`), no modifiers, once per file: `brick:scroll="x" ... brick:scroll="x"` fails with `the name is used twice in this file; brick:scroll acts on the first only`.
- **Each link is prefetched once, by whatever will load it.** A page with a `brick:prefetch` link also carries speculation rules; links `nav.js` intercepts are fetched into its own cache, because the browser's prefetch cache only serves full loads, so the rules cover only `brick:nav="off"` links. Rules match links inside the site's base path, and exported pages carry them too.
- **Any form submission forgets cached pages**, so a page shown after a POST is never stale. `GET` search forms and `POST` forms navigate in place; a `POST`'s redirect is followed.
- **What always gets the full page:** a different layout, a streamed page, a form post, an error page, or any doubt. That is a fallback, not a failure.

## Related

- [State in the URL](url-and-server-state.md#step-3--put-fields-in-the-url-and-respond-when-it-changes): a same-path link that changes only a component's `query:` fields is a patch, not a navigation.
- [Islands, lazy islands and streaming](islands-lazy-and-streaming.md): a client navigation never streams.
- The guide's [navigation section](../../guide.md#navigation-without-reloads) and the site guides in [`docs/guides/site`](../site/).
- [Attribute reference](../../reference/attributes.md) and [feature index](../../reference/features.md#navigation).
- Example: [`examples/interactivity-navigation`](../../../examples/interactivity-navigation/).
