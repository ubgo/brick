# Pages, URLs and `.Site.Pages`

## What it is and when you want it

The `site` package turns a folder of pages into a website. Every file under `content/` is a page at the URL its path names, every page goes into the layout, and every template can read `.Site.Pages`, the list of all pages, to build a blog index or a menu. You want it as soon as you have more than one page and do not want to write a router.

## Before you start

- You know how a brick page, a layout and a component look ([guide: pages and layouts](../../guide.md)).
- Go 1.25 and this repository. The example is [`examples/site-blog`](../../../examples/site-blog/); run it with `cd examples/site-blog && go run .`.
- Optional: the `brick` command (`go build -o brick ./cmd/brick` from `cmd/brick/`), which runs the same site with no Go code.

## Step 1 — Lay out the files

The path decides the URL, so there is no route table to keep in sync.

```text
theme/
  site.yaml                      → .Site.title
  layouts/default.html           → wraps every page
  components/PostLink.html
  content/index.html             → /
  content/about.html             → /about/
  content/legal.html             → /terms/     (brick: {url: /terms/})
  content/404.html               → served for unknown URLs
  content/blog/index.html        → /blog/
  content/blog/first-light.html  → /blog/first-light/
  content/blog/tide-tables.md    → /blog/tide-tables/
```

`site.yaml` is loaded into `.Site`:

<!-- snippet: examples/site-blog/theme/site.yaml -->
```yaml
title: Field Notes
```

## Step 2 — Write the layout

A page with no `layout:` key gets `layouts/default.html`; `{{ children }}` is where the page goes.

<!-- snippet: examples/site-blog/theme/layouts/default.html -->
```html
<!doctype html>
<html>
<head><title>{{ .Title }} · {{ .Site.title }}</title></head>
<body>
<nav><a href="/">Home</a> <a href="/blog/">Blog</a> <a href="/about/">About</a></nav>
<main>{{ children }}</main>
</body>
</html>
```

## Step 3 — Move a page with `brick: {url:}`

When the file name is not the URL you want, say so in the frontmatter instead of renaming the file. `content/legal.html`:

<!-- snippet: examples/site-blog/theme/content/legal.html -->
```html
---
title: Terms
brick: {url: /terms/}
---
<h1>Terms</h1>
```

## Step 4 — List the posts from `.Site.Pages`

`.Site.Pages` holds every page, newest first, each with `.URL`, `.Title`, `.Date`, `.Section` (the first folder under `content/`) and `.Params` (its frontmatter). The section's own index page is in the list too, so skip it. `content/blog/index.html`:

<!-- snippet: examples/site-blog/theme/content/blog/index.html -->
```html
---
title: Blog
menu: main
weight: 2
cache: {tags: [posts]}
---
<h1>Blog</h1>
<ul>
{{- range .Site.Pages }}{{ if and (eq .Section "blog") (ne .URL "/blog/") }}
  <li><PostLink title="{{ .Title }}" url="{{ .URL }}" /> · {{ dateFormat "2 Jan 2006" .Date }}</li>
{{- end }}{{ end }}
</ul>
```

`components/PostLink.html`:

<!-- snippet: examples/site-blog/theme/components/PostLink.html -->
```html
---
props:
  title: {type: string, required: true}
  url: {type: string, required: true}
---
<a class="post" href="{{ .Props.url }}">{{ .Props.title }}</a>
```

(The `cache:` line is used in [render cache](render-cache.md); it changes nothing here.)

## Step 5 — Serve it from Go

`site.New` needs only `FS`; markdown pages need a backend, here goldmark with heading ids. From `examples/site-blog/main.go`:

<!-- ds:block id=newsite-8vbpnjfz title="examples/site-blog/main.go" -->
**examples/site-blog/main.go** · [`examples/site-blog/main.go:33-39`](../../../examples/site-blog/main.go#L33-L39)

```go
func newSite(ctx context.Context, dir string, dev, drafts bool) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return newSiteFS(ctx, root.FS(), dev, drafts)
}
```
<!-- /ds:block hash=18a250 -->

<!-- ds:block id=newsitefs-5nq2ppfu title="examples/site-blog/main.go" -->
**examples/site-blog/main.go** · [`examples/site-blog/main.go:43-50`](../../../examples/site-blog/main.go#L43-L50)

```go
func newSiteFS(ctx context.Context, fsys fs.FS, dev, drafts bool) (*site.Site, error) {
	return site.New(ctx, site.Options{
		FS:     fsys,
		Engine: brick.Options{Markdown: markdowngoldmark.New(markdowngoldmark.WithHeadingIDs())},
		Dev:    dev,
		Drafts: drafts,
	})
}
```
<!-- /ds:block hash=1d7a4c -->

`s.ListenAndServe(ctx, ":8090")` serves it, and `s.Handler()` is the same thing as an `http.Handler` for your own server.

## Step 6 — Build a menu from route metadata

`Site.Routes()` returns every URL with the page's `Title`, `Description`, `Date`, `Draft`, `Section` and `Meta`, the frontmatter without the `brick:` key. The engine gives no frontmatter key a routing meaning, so a menu is just a key you choose: the blog's home, blog index and about page say `menu: main` and a `weight:` (see the frontmatter in Step 4). `examples/site-blog/ops.go` turns that into an ordered list:

<!-- ds:block id=menulinks-rerg2rdc title="examples/site-blog/ops.go" -->
**examples/site-blog/ops.go** · [`examples/site-blog/ops.go:36-55`](../../../examples/site-blog/ops.go#L36-L55)

```go
func menuLinks(s *site.Site, name string) []menuLink {
	type entry struct {
		weight int
		link   menuLink
	}
	var found []entry
	for _, r := range s.Routes() {
		if m, _ := r.Meta[metaMenu].(string); m != name {
			continue
		}
		w := weightOf(r.Meta[metaWeight])
		found = append(found, entry{w, menuLink{Title: r.Title, URL: r.URL}})
	}
	slices.SortStableFunc(found, func(a, b entry) int { return a.weight - b.weight })
	links := make([]menuLink, len(found))
	for i, e := range found {
		links[i] = e.link
	}
	return links
}
```
<!-- /ds:block hash=7eda9d -->

YAML integers arrive in `Meta` as `int64`, which is why the example reads a weight through a small switch instead of `.(int)`:

<!-- ds:block id=weightof-r5bzbjyn title="examples/site-blog/ops.go" -->
**examples/site-blog/ops.go** · [`examples/site-blog/ops.go:64-74`](../../../examples/site-blog/ops.go#L64-L74)

```go
func weightOf(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return unweighted
}
```
<!-- /ds:block hash=3b2397 -->

`TestMenuLinks` checks the order (`Home`, `Blog`, `About`, though the URLs sort `/`, `/about/`, `/blog/`), that a menu nobody uses is empty, and that editing a returned `Meta` map changes nothing in the site.

## Try it

```sh
cd examples/site-blog
go run .
curl -s localhost:8090/blog/
```

```html
<!doctype html>
<html>
<head><title>Blog · Field Notes</title></head>
<body>
<nav><a href="/">Home</a> <a href="/blog/">Blog</a> <a href="/about/">About</a></nav>
<main><h1>Blog</h1>
<ul>
  <li><a class="post" href="/blog/tide-tables/">Reading tide tables</a> · 1 Oct 2026</li>
  <li><a class="post" href="/blog/first-light/">First light</a> · 1 Sep 2026</li>
</ul>
</main>
</body>
</html>
```

Every URL and the file behind it:

```sh
brick routes theme
```

```text
/                   content/index.html
/about/             content/about.html
/blog/              content/blog/index.html
/blog/first-light/  content/blog/first-light.html
/blog/tide-tables/  content/blog/tide-tables.md
/terms/             content/legal.html
```

From Go, `s.Routes()` returns the same list (Step 6); `brick routes -json` prints it with each route's `Meta`. `go test .` checks both the route list and the exact page above.

## Pitfalls

- **Two pages on one URL is an error, never a silent winner.** Setting `brick: {url: /about/}` on `legal.html` stops the site: `site: content/about.html and content/legal.html both have the URL /about/; set brick: {url: …} on one of them`.
- **The section index lists itself.** `content/blog/index.html` has `.Section "blog"`, so a bare `{{ if eq .Section "blog" }}` puts "Blog" in its own list; compare `.URL` as in Step 4.
- **`.Site` already has a `Pages` key.** A `site.yaml` or `data/` file that defines `Pages` makes `site.New` fail with `site: .Site already has a "Pages" key, which is where the page index goes`.
- **Drafts are missing from `.Site.Pages`** unless drafts are on; see [collections and drafts](collections-and-drafts.md).
- **YAML numbers in `Meta` are `int64`.** `r.Meta["weight"].(int)` is silently false for `weight: 2`; accept `int64` as `weightOf` does.
- **`Routes()` returns copies.** Changing a `Route.Meta` map changes nothing in the site.

## Related

- [Markdown and headings](markdown-and-headings.md) · [Collections and drafts](collections-and-drafts.md) · [404 and error pages](not-found-and-error-pages.md) · [Dynamic routes](dynamic-routes.md) · [Static export](static-export.md)
- [Feature index](../../reference/features.md) rows: the page index (`.Site.Pages`), route metadata (`Site.Routes`, `Route.Meta`), `brick routes`.
- Long-form: [guide › A whole site](../../guide.md#a-whole-site--urls-serving-building).
- Example: [`examples/site-blog`](../../../examples/site-blog/).
