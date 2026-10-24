# Layouts, context, site data and translations

## What it is and when you want it

Values reach a template from six places, and only one of them is global. This page covers the ones that do not come from the call site: a **layout** wraps a page in its `<html>` shell; **`.Ctx`** carries a value a layout or ancestor provides (a section's accent colour, the locale, the current user) to every component beneath it; **`.Site`** is the read-only site config and its data files, loaded once by your Go program; **`.Data`** is a data file one file imports with `use:`; and **`t`** looks up the text for the current language.

| Write | Comes from | Visible in |
|---|---|---|
| `.Props` | the tag's attributes (or the data your Go program passes to `Render`, for a page) | this component or page |
| `.Params` | the page's frontmatter | this page, and its layouts |
| `.Data` | data files imported with `use:` | this file |
| `.Ctx` | a value an ancestor provided | everything beneath it |
| `.Slot` | the value passed to `{{ node "x" $v }}` | that block |
| `.Site` | the site config, loaded once | everywhere, read-only |

## Before you start

- Read [Components and props](props.md).
- Your Go program loads everything explicitly: `brick.LoadSite` for `site.yaml` and `data/`, `brick.LoadMessages` for `i18n/`. Nothing is discovered on its own, and there is no `.Env`.

## Step 1 — Load the site config, data and messages in Go

`${BASE_URL}` in `site.yaml` is filled from the environment lookup you pass, so that file is the complete list of what a template may read from the environment.

<!-- snippet: examples/components-layouts-data/theme/site.yaml -->
```yaml
title: My Blog
baseURL: ${BASE_URL}
```

<!-- ds:block id=lookup-ycm8wjqj title="examples/components-layouts-data/main.go" -->
**examples/components-layouts-data/main.go** · [`examples/components-layouts-data/main.go:49-52`](../../../examples/components-layouts-data/main.go#L49-L52)

```go
func lookup(name string) (string, bool) {
	v, ok := siteEnv[name]
	return v, ok
}
```
<!-- /ds:block hash=c9c672 -->

<!-- ds:block id=newengine-7h38t45k title="examples/components-layouts-data/main.go" -->
**examples/components-layouts-data/main.go** · [`examples/components-layouts-data/main.go:61-76`](../../../examples/components-layouts-data/main.go#L61-L76)

```go
func newEngine(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	fsys := root.FS()
	site, err := brick.LoadSite(fsys, "site.yaml", "data/", brick.WithEnv(lookup))
	if err != nil {
		return nil, err
	}
	msgs, err := brick.LoadMessages(fsys, "i18n/")
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{FS: fsys, Site: site, Messages: msgs, DefaultLocale: defaultLocale})
}
```
<!-- /ds:block hash=dee6f5 -->

A real host passes `os.LookupEnv`; the example uses a fixed map so its output never depends on the machine.

## Step 2 — Wrap pages in a layout

A layout is a component whose `children` is the page. A page gets `layouts/default.html` unless it names another with `layout:`, and `layout: none` means no layout. In a layout, `.Title` and `.Params` are the page's.

<!-- snippet: examples/components-layouts-data/theme/layouts/default.html -->
```html
<!doctype html>
<html>
<head><title>{{ .Title }} · {{ .Site.title }}</title></head>
<body>
<a href="{{ .Site.baseURL }}/">{{ .Site.title }}</a>
{{ children }}
</body>
</html>
```

Layouts can have layouts: `post.html` renders inside `default.html`.

<!-- snippet: examples/components-layouts-data/theme/layouts/post.html -->
```html
---
layout: default
provide:
  accent: blue
---
<article class="post">{{ children }}</article>
```

<!-- snippet: examples/components-layouts-data/theme/content/blog/hello.html -->
```html
---
title: Hello
layout: post
---
<h1>{{ .Title }}</h1>
<Card :plan="dict `name` `Team` `price` 30" />
```

For a fragment that a script on the page fetches and inserts, render the same page with `brick.WithLayout("none")` from Go.

## Step 3 — Provide a value to everything beneath

The post layout's `provide: {accent: blue}` reaches any component inside it. A component declares what it reads with `inject:`, which also gives it a default for pages that provide nothing.

<!-- snippet: examples/components-layouts-data/theme/components/Card.html -->
```html
---
inject:
  accent: {default: gray}
props:
  plan: {type: map, required: true}
---
<div class="card card--{{ .Ctx.accent }}">{{ .Props.plan.name }}: ${{ .Props.plan.price }}</div>
```

The nearest provider wins, so a section's layout can override the site's. Your Go program can seed a value for a whole render with `brick.Provide("accent", "green")`; a `provide:` nearer the component still wins over it. To provide something computed, call `{{ provide "accent" .Page.Params.accent }}` before `{{ children }}`.

## Step 4 — Read site data, or import one data file

Files in `data/` become `.Site.Data.<filename>`, visible everywhere:

<!-- snippet: examples/components-layouts-data/theme/content/index.html -->
```html
---
title: Pricing
---
<h1>{{ .Title }}</h1>
{{ range .Site.Data.pricing.plans }}<Card :plan="." />{{ end }}
```

A data file only one page needs is imported with `use:` and read as `.Data.<name>`:

<!-- snippet: examples/components-layouts-data/theme/content/plans.html -->
```html
---
layout: none
use:
  prices: ~/data/pricing.yaml
---
<ul>{{ range .Data.prices.plans }}<li>{{ .name }}</li>{{ end }}</ul>
```

## Step 5 — Translate text

`t` looks up a key in the messages for the render's language (`brick.WithLocale("fr")`), with plurals following each language's rules. `dateFormat` and `numberFormat` follow the same language.

<!-- snippet: examples/components-layouts-data/theme/i18n/en.yaml -->
```yaml
cart:
  add: Add to cart
  items:
    one: "{count} item"
    other: "{count} items"
```

<!-- snippet: examples/components-layouts-data/theme/i18n/fr.yaml -->
```yaml
cart:
  add: Ajouter au panier
  items:
    one: "{count} article"
    other: "{count} articles"
```

<!-- snippet: examples/components-layouts-data/theme/content/cart.html -->
```html
---
title: Cart
date: 2026-09-27
layout: none
---
<button>{{ t "cart.add" }}</button>
<p>{{ t "cart.items" (dict "count" 1) }} / {{ t "cart.items" (dict "count" 3) }}</p>
<p>{{ dateFormat "Monday 2 January" .Date }}</p>
```

When one language needs its own markup, add a variant next to the file: `about.fr.html` beside `about.html`. A French render picks it, every other language gets the shared file. Variants work for pages, layouts and components.

## Step 6 — Read a data format brick does not bundle

`use:` and `load()` read `.json` and `.yaml`. Any other format is one parser away: `Options.DataFormats` maps an extension to a function from bytes to plain Go values, which brick normalizes the way it does YAML, and `use:` then accepts that extension. A host registers a TOML or CSV library's parser; the example registers a ten-line parser for its own `.kv` format so it needs no dependency.

<!-- ds:block id=parsetoml-jtxkffq5 title="examples/components-options/main.go" -->
**examples/components-options/main.go** · [`examples/components-options/main.go:44-58`](../../../examples/components-options/main.go#L44-L58)

```go
func parseKV(src []byte) (any, error) {
	out := map[string]any{}
	for i, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, " = ")
		if !ok {
			return nil, fmt.Errorf("line %d: want `name = value`", i+1)
		}
		out[k] = strings.Trim(v, `"`)
	}
	return out, nil
}
```
<!-- /ds:block hash=59757e -->

The data file and the page that imports it:

<!-- snippet: examples/components-options/theme/data/colors.kv -->
```text
# The brand colours, in a format brick does not read by itself.
brand = "#0f766e"
accent = "#b45309"
```

<!-- snippet: examples/components-options/theme/content/index.html lines=1-4 -->
```html
---
use:
  colors: ~/data/colors.kv
---
```

The page prints `brand #0f766e, accent #b45309` (line 10 of the program's output). The format is registered once, in the `Options` the example builds in [Components and props](props.md#step-5--find-components-in-more-than-one-folder).

## Try it

```sh
cd examples/components-layouts-data
go run .
go test .
```

<!-- snippet: examples/components-layouts-data/testdata/output.txt -->
```text
== content/index.html
<!doctype html>
<html>
<head><title>Pricing · My Blog</title></head>
<body>
<a href="https://example.com/">My Blog</a>
<h1>Pricing</h1>
<div class="card card--gray">Free: $0</div><div class="card card--gray">Pro: $12</div>

</body>
</html>

== content/index.html as a fragment
<h1>Pricing</h1>
<div class="card card--gray">Free: $0</div><div class="card card--gray">Pro: $12</div>

== content/blog/hello.html
<!doctype html>
<html>
<head><title>Hello · My Blog</title></head>
<body>
<a href="https://example.com/">My Blog</a>
<article class="post"><h1>Hello</h1>
<div class="card card--blue">Team: $30</div>
</article>

</body>
</html>

== content/plans.html
<ul><li>Free</li><li>Pro</li></ul>

== content/cart.html (en)
<button>Add to cart</button>
<p>1 item / 3 items</p>
<p>Sunday 27 September</p>

== content/cart.html (fr)
<button>Ajouter au panier</button>
<p>1 article / 3 articles</p>
<p>dimanche 27 septembre</p>

== content/about.html (en)
<p>About us.</p>

== content/about.html (fr)
<p>À propos.</p>

```

## Pitfalls

- **A missing translation shows the key, never a blank.** `{{ t "cart.remove" }}` renders `cart.remove` and the render's warnings carry `translation "cart.remove" is missing for en`. A language without the key falls back to its base language (`pt-BR` → `pt`), then to `DefaultLocale`.
- **A named layout must exist.** `layout: blog` fails with `layout "blog": ~/layouts/blog.html does not exist`. A chain that loops fails with `layout cycle: content/x.html → layouts/a.html → layouts/b.html → layouts/a.html`.
- **There is no `.Env`.** `{{ .Env }}` fails with `at <.Env>: can't evaluate field Env in type *brick.Scope`. Environment values reach templates only through `${VAR}` in `site.yaml`.
- **A missing `.Params` key renders empty, like Go.** To catch typos, set `strictParams: true` on the page; then `{{ .Params.titel }}` fails with `at <.Params.titel>: map has no entry for key "titel"`. Read a genuinely optional key with `{{ index .Params "subtitle" }}`.
- **`inject:` is optional but recommended.** Without it a component can still read `.Ctx`, but the dependency is invisible and there is no default. `inject: {user: {required: true}}` turns a missing value into an error instead of a blank: `<Need>: inject: "user" is required, but no ancestor provides it (searched page → Need)`.
- **`.Ctx` is read where the markup is shown, everything else where it was written.** That is what lets a layout's `provide` reach text the page wrote. Inside a component, `.Page.Params.title` reaches the page's data from any depth.
- **Month and day names are built in for fr, de, es, it, pt and nl.** Other languages keep Go's English names in `dateFormat`.
- **Keep `.Ctx` for theme, locale or current user.** If a value can be a prop without pain, make it a prop.
- **An extension nobody registered is refused.** `use: d: ~/data/c.xyz` fails with `use: d: ".xyz" is not a component (.html), data (.json .yaml .toml, plus registered .kv) or script (.star) file`, naming the extensions registered in `DataFormats`; the example's test pins the text.
- **A parser's error names the file.** The example's `data/bad.kv` fails with `data/bad.kv: line 1: want `name = value``, so a bad data file is found by its path.
- **A custom format works from `use:` and `load()` alike.** Both go through `Options.DataFormats`, and a registered entry for `.json` or `.yaml` replaces the built-in parser.

## Related

- [Components and props](props.md) and [Children, blocks and slots](slots.md).
- [Scripts and loaders](loaders.md): data a component fetches for itself.
- [Frontmatter reference](../../reference/frontmatter.md): `layout`, `provide`, `inject`, `use`, `strictParams`.
- [Feature index](../../reference/features.md): the "Layouts, context and data" rows.
- The single-page read: [Layouts](../../guide.md#layouts), [Where values come from](../../guide.md#where-values-come-from), [Shared values for a section — `.Ctx`](../../guide.md#shared-values-for-a-section--ctx), [Translations](../../guide.md#translations).
- Example: [examples/components-layouts-data](../../../examples/components-layouts-data/).
- Example: [examples/components-options](../../../examples/components-options/) for the settings added in the later steps.
