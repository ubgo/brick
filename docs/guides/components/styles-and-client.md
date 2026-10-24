# Styles and client code

## What it is and when you want it

A card's `.title { color: crimson }` should not turn every title on the page crimson, and a component's JavaScript should not load three times for three copies. A component carries its own CSS in a `<brick:style>` block (or a sibling `Card.css`) and its own browser JavaScript in a `<brick:client>` block. brick ships each component type's CSS and JS **once**, as a content-hashed file, linked by `{{ assets }}` with Subresource Integrity and the request's CSP nonce. JavaScript only enhances markup the server already rendered: a component works with no JavaScript at all.

## Before you start

- Read [Components and props](props.md).
- Your Go program serves the engine's asset files (`Engine.Assets()`) under `/_brick/` (`Options.AssetBase` changes the prefix). With `net/http`, `contrib/assets-http` does it; see [examples/engineonly](../../../examples/engineonly/main.go).
- Put `{{ assets }}` once in your layout, usually at the end of `<body>`. That is where the files are linked.

## Step 1 — Scope a component's CSS

`<brick:style scoped>` confines the rules to this component using the browser's own `@scope`: brick marks the root element with `data-brick-s` and wraps the CSS in `@scope (…) to ([data-brick-s])`, so the rules stop at the root of any nested component that has scoped styles of its own. The component must have exactly one top-level element.

<!-- snippet: examples/components-styles-client/theme/components/Card.html -->
```html
---
props:
  title: string
---
<brick:style scoped>
.title { color: crimson }
</brick:style>
<article class="card"><h3 class="title">{{ .Props.title }}</h3>{{ children }}</article>
```

## Step 2 — Let a parent reach a child's root

A parent sometimes needs to place a child (a margin, a width, a grid area) without restyling its insides. `scoped="inclusive"` lets the parent's rules reach the root element of a nested scoped component, and nothing below it. `:scope` styles the component's own root.

<!-- snippet: examples/components-styles-client/theme/components/Grid.html -->
```html
<brick:style scoped="inclusive">
:scope { display: grid }
.card { margin-block: 1rem }
</brick:style>
<div class="grid">{{ children }}</div>
```

## Step 3 — Keep the CSS in its own file

A stylesheet next to the component ships as its CSS, exactly like a `<brick:style>` block: global unless the component says `brick: {css: scoped}` (or `inclusive`). The file stays plain CSS that any editor or linter reads.

<!-- snippet: examples/components-styles-client/theme/components/Badge.html -->
```html
---
brick:
  css: scoped
---
<span class="badge">{{ children }}</span>
```

<!-- snippet: examples/components-styles-client/theme/components/Badge.css -->
```css
:scope { padding: 0 .25rem; border-radius: 4px }
```

## Step 4 — Transform every stylesheet in Go

`Options.StyleTransform` receives each block (`brick.StyleBlock`: the file, the content hash, whether it is scoped, the source, and the CSS brick would ship) and returns the CSS that ships instead: a minifier, an `@scope` fallback, another scoping strategy.

<!-- ds:block id=minify-kfznkq8x title="examples/components-styles-client/main.go" -->
**examples/components-styles-client/main.go** · [`examples/components-styles-client/main.go:39-41`](../../../examples/components-styles-client/main.go#L39-L41)

```go
func minify(b brick.StyleBlock) ([]byte, error) {
	return []byte(strings.Join(strings.Fields(string(b.CSS)), " ")), nil
}
```
<!-- /ds:block hash=c0767a -->

<!-- ds:block id=newengine-8kneqnh6 title="examples/components-styles-client/main.go" -->
**examples/components-styles-client/main.go** · [`examples/components-styles-client/main.go:45-51`](../../../examples/components-styles-client/main.go#L45-L51)

```go
func newEngine(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{FS: root.FS(), StyleTransform: minify})
}
```
<!-- /ds:block hash=3c37cc -->

## Step 5 — Mount browser code on a component

`<brick:client>` is an ES module. `brick.mount("Accordion", (el, props) => { … })` runs once per element of that type, on the page or inserted later, and runs the function it returns when the element is removed. Only props declared `client: true` reach the browser, as `data-brick-props`; every other prop stays on the server.

<!-- snippet: examples/components-styles-client/theme/components/Accordion.html -->
```html
---
props:
  open: {type: bool, default: true, client: true}
  secret: {type: string, default: "server only"}
---
<brick:client>
brick.mount("Accordion", (el, props) => {
  const toggle = () => el.toggleAttribute("open")
  if (!props.open) el.removeAttribute("open")
  el.addEventListener("toggle", toggle)
  return () => el.removeEventListener("toggle", toggle)
})
</brick:client>
<details class="accordion" open><summary>More</summary>{{ children }}</details>
```

The accordion renders `open` on the server, so without JavaScript its content is visible; the mount closes it when the page asked for `:open="false"`.

## Step 6 — Import one component's code from another

`import { openModal } from "brick:Modal"` becomes a relative import of the Modal module's hashed file, so it works wherever the assets are served.

<!-- snippet: examples/components-styles-client/theme/components/Modal.html -->
```html
<brick:client>
export function openModal(id) {
  document.getElementById(id)?.showModal()
}
</brick:client>
<dialog id="{{ .Props.id }}">{{ children }}</dialog>
```

<!-- snippet: examples/components-styles-client/theme/components/Opener.html -->
```html
---
props:
  target: string
---
<brick:client>
import { openModal } from "brick:Modal"
brick.mount("Opener", (el) => {
  const click = () => openModal(el.dataset.target)
  el.addEventListener("click", click)
  return () => el.removeEventListener("click", click)
})
</brick:client>
<button class="opener" data-target="{{ .Props.target }}">{{ children }}</button>
```

`brick:mount="idle"` on the tag waits until the browser is idle (`"visible"` waits until it scrolls into view). The page that uses all of them:

<!-- snippet: examples/components-styles-client/theme/content/index.html -->
```html
<!doctype html>
<html><body>
<Grid><Card title="Scoped">Only this title is crimson.</Card></Grid>
<Badge>new</Badge>
<Accordion :open="false">Shown without JavaScript.</Accordion>
<Opener target="help" brick:mount="idle">Help</Opener>
<Modal id="help">Help text.</Modal>
{{ assets }}
</body></html>
```

## Step 7 — Keep the old engine's assets after a rebuild

A page cached before a theme rebuild links stylesheets by content hash. A fresh engine serves only the assets of pages it has rendered since, so the cached page's CSS would 404 until the page is rendered again. `InheritAssets` makes the new engine serve the replaced engine's asset files too; its own files are never shadowed, and `nil` or the same engine is a no-op. The site layer does this when it rebuilds its engine in Dev; a host that swaps engines itself calls it once. Names are content-hashed, so an inherited file can never shadow a new one.

<!-- ds:block id=inherit-4dryrmmk title="examples/components-options/main.go" -->
**examples/components-options/main.go** · [`examples/components-options/main.go:169-188`](../../../examples/components-options/main.go#L169-L188)

```go
func inherit(ctx context.Context, w io.Writer, dir string) error {
	oldEng, err := newEngine(dir, brick.Prod)
	if err != nil {
		return err
	}
	if _, err := oldEng.Render(ctx, "content/island.html", nil); err != nil {
		return err
	}
	// A rebuild after a theme edit: a fresh snapshot that has rendered
	// nothing yet, while a cache still holds pages the old one made.
	newEng, err := newEngine(dir, brick.Prod)
	if err != nil {
		return err
	}
	before := assetNames(newEng)
	newEng.InheritAssets(oldEng)
	after := assetNames(newEng)
	_, err = fmt.Fprintf(w, "== InheritAssets\nbefore: %v\nafter:  %v\n\n", before, after)
	return err
}
```
<!-- /ds:block hash=41afdc -->

The new engine serves nothing before the call and the old page's stylesheet after it (lines 82–84 of the program's output):

<!-- snippet: examples/components-options/testdata/output.txt lines=82-84 -->
```text
== InheritAssets
before: []
after:  [c/Badge.716b9a90.css]
```

## Try it

```sh
cd examples/components-styles-client
go run .
go test .
```

The program prints the page and then every file `{{ assets }}` links. The runtime's name and integrity change with every runtime release; the test compares everything else byte for byte.

<!-- snippet: examples/components-styles-client/testdata/output.txt -->
```text
== content/index.html
<!doctype html>
<html><body>
<div data-brick-s="f2d1f4eb" class="grid"><article data-brick-s="5093e86a" class="card"><h3 class="title">Scoped</h3>Only this title is crimson.</article></div>
<span data-brick-s="a7055d23" class="badge">new</span>
<details data-brick="b9a47377c" data-brick-type="Accordion" data-brick-props="{&#34;open&#34;:false}" class="accordion" open><summary>More</summary>Shown without JavaScript.</details>
<button data-brick="bf35f94d6" data-brick-type="Opener" data-brick-props="{}" data-brick-mount="idle" class="opener" data-target="help">Help</button>
<dialog data-brick="b33f53269" data-brick-type="Modal" data-brick-props="{}" id="help">Help text.</dialog>
<brick-assets><script type="module" src="/_brick/c/runtime.0b4d3d08.js" integrity="sha384-1ojXDjndJ49gLs9LzFJ7HhqO9T6yLwLO3U8Y+zouOx6APv8CGhC1ifU41Z4HS9sK" nonce="r4nd0m"></script><script type="module" src="/_brick/c/Accordion.74b03d84.js" integrity="sha384-33wTT/AOjxrVSfXo3vkHCtpI+8KPOIMtAfZkkAI050ZQKv5hx/zmaJ6Jd8bo/Tx5" nonce="r4nd0m"></script><link rel="stylesheet" href="/_brick/c/Badge.556dd832.css" integrity="sha384-OAvP8Lt5XYxXEHuenmbELbQsadvenOdtaPb0psdDqZ1joTC6ufS5ldzaJF3rMXiA" nonce="r4nd0m"><link rel="stylesheet" href="/_brick/c/Card.5fb74083.css" integrity="sha384-KfR3uvGlnAMXFgLQpnQWN29p4vStT9KPfSFHEN78NM2SfMoCPLmHyNdF7n1AlyFZ" nonce="r4nd0m"><link rel="stylesheet" href="/_brick/c/Grid.b65e54ca.css" integrity="sha384-PgSzR9dz71C9EzvAhjKVVkF53Rms1BlFG8362zInXdQ0VsT1RchY86TqPcuahg/f" nonce="r4nd0m"><script type="module" src="/_brick/c/Modal.7604cf59.js" integrity="sha384-ROvYV8zhf2JHSOCvsmxa+ULz8EoUPUlqIIIiUciY1eACFMI8PcG9BghQnaxVYRxS" nonce="r4nd0m"></script><script type="module" src="/_brick/c/Opener.59907819.js" integrity="sha384-/q1TaB6EL65Cu5ZJWfG+ntdFVJlmPkntdEoBozZ1X3hszW7P1uvm7Gce4F0Jyz+/" nonce="r4nd0m"></script></brick-assets>
</body></html>

== c/Accordion.74b03d84.js

brick.mount("Accordion", (el, props) => {
  const toggle = () => el.toggleAttribute("open")
  if (!props.open) el.removeAttribute("open")
  el.addEventListener("toggle", toggle)
  return () => el.removeEventListener("toggle", toggle)
})


== c/Badge.556dd832.css
@scope ([data-brick-s="a7055d23"]) to ([data-brick-s]) { :scope { padding: 0 .25rem; border-radius: 4px } }

== c/Card.5fb74083.css
@scope ([data-brick-s="5093e86a"]) to ([data-brick-s]) { .title { color: crimson } }

== c/Grid.b65e54ca.css
@scope ([data-brick-s="f2d1f4eb"]) to ([data-brick-s] > *) { :scope { display: grid } .card { margin-block: 1rem } }

== c/Modal.7604cf59.js

export function openModal(id) {
  document.getElementById(id)?.showModal()
}


== c/Opener.59907819.js

import { openModal } from "./Modal.7604cf59.js"
brick.mount("Opener", (el) => {
  const click = () => openModal(el.dataset.target)
  el.addEventListener("click", click)
  return () => el.removeEventListener("click", click)
})


== c/runtime.0b4d3d08.js (the runtime, not shown)

```

## Pitfalls

- **One top-level element.** A component with `<brick:style scoped>`, `brick: {css: scoped}` or `<brick:client>` and two roots fails with `a component with <brick:style scoped>, brick: {css: scoped} or <brick:client> must have exactly one top-level element, so the engine has one place for its marker`.
- **`scoped` takes three spellings.** Bare `scoped` is `"exclusive"`; anything other than `exclusive` or `inclusive` fails with `<brick:style scoped="deep">: scoped is bare, "exclusive" or "inclusive"`.
- **No `{{ assets }}`, no CSS or JS.** A page whose components carry styles or client code but renders no `{{ assets }}` gets the warning `a component on this page has <brick:client> or <brick:style>, but no {{ assets }} was rendered, so its JS and CSS never load`.
- **`brick: {css:}` without the file ships nothing.** `brick check` warns: `brick: css: scoped is set, but there is no ~/components/NoCss.css next to this component; nothing ships`. Pages and layouts never pick up a `.css` beside them.
- **With both a sibling file and an inline block,** the file comes first and the inline blocks after, in one hashed stylesheet, so an inline rule wins a tie.
- **`@scope` support.** `@scope` with its `to ()` clause is in Chrome and Edge 118+, Safari 17.4+ and Firefox 146+; older browsers skip those rules. A `StyleTransform` is the place to add a fallback.
- **A StyleTransform must be deterministic.** Its output is hashed into the asset URL when the file compiles; an error from it fails the compile, naming the file and line.
- **Rebuilding the engine?** Pages rendered by the old engine link the old hashed files. A host that swaps engines on a file change calls `newEngine.InheritAssets(oldEngine)` so those links keep working; the site layer does this in Dev.
- **Nonces.** Pass a fresh one per response with `brick.WithNonce(n)` (or `Result.Bytes(brick.WithNonce(n))` on a cached render) and send the same value in your `Content-Security-Policy` header.
- **`InheritAssets` grows with every rebuild.** Call it with the engine you are replacing, not an older one, and drop an engine that no cached page can still reference; otherwise the served set only ever gets larger.

## Related

- [Components and props](props.md): `client: true` is a prop setting.
- [When things go wrong](errors.md): a failed boundary does not load the failed component's JavaScript.
- [Frontmatter reference](../../reference/frontmatter.md): `brick.css` and `props.<name>.client`.
- [Feature index](../../reference/features.md): the "Styles and client code" rows.
- The single-page read: [Styles and browser JavaScript](../../guide.md#styles-and-browser-javascript).
- Example: [examples/components-styles-client](../../../examples/components-styles-client/).
- Example: [examples/components-options](../../../examples/components-options/) for the settings added in the later steps.
