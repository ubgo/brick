# Hosting under a path, and several sites on one server

## What it is and when you want it

A site is always written as if it lived at the root: `href="/install/"`. `BasePath` hosts it somewhere else, say `/docs/` for a GitHub Pages project site, by adding the base to every root-absolute URL in the HTML it serves or exports. `site.Mount` puts several such sites behind one `http.Handler`, each with its own folder, engine, cache and live reload. You want it when the site does not own the whole domain.

## Before you start

- Read [pages and routes](pages-and-routes.md) and [static export](static-export.md).
- The example is [`examples/site-subpath`](../../../examples/site-subpath/): a `docs/` project and a `shop/` project.

## Step 1 — Write the site for the root

Nothing in the project knows its base. `docs/layouts/default.html`:

<!-- snippet: examples/site-subpath/docs/layouts/default.html -->
```html
<!doctype html>
<html>
<head><title>{{ .Title }}</title><link rel="stylesheet" href="/style.css"></head>
<body>
<nav><a href="/">Docs home</a> <a href="/install/">Install</a> <a href="/shop/">Shop</a></nav>
<main>{{ children }}</main>
</body>
</html>
```

`docs/content/index.html`:

<!-- snippet: examples/site-subpath/docs/content/index.html -->
```html
---
title: Docs
---
<h1>Docs</h1>
<p>Start with <a href="/install/">installing</a>. Links are written as if the site were at the root: <code>/install/</code>.</p>
```

## Step 2 — Give each site its base, and keep the sibling's links

`KeepPaths` (the `-keep /shop/` flag of `brick build` and `brick serve`) lists URL prefixes the rewrite leaves alone: the docs link to `/shop/` belongs to the other site. From `examples/site-subpath/main.go`:

<!-- snippet: examples/site-subpath/main.go lines=23-31 -->
```go
// The two projects (relative to this example's directory) and the paths
// they are hosted under. Each base also names the other site's prefix in
// KeepPaths, because a link to a sibling site must not get this site's base.
const (
	docsDir  = "docs"
	shopDir  = "shop"
	docsBase = "/docs"
	shopBase = "/shop"
)
```

<!-- ds:block id=newsite-g7xs2dc3 title="examples/site-subpath/main.go" -->
**examples/site-subpath/main.go** · [`examples/site-subpath/main.go:36-42`](../../../examples/site-subpath/main.go#L36-L42)

```go
func newSite(ctx context.Context, dir, base string, keep ...string) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{FS: root.FS(), BasePath: base, KeepPaths: keep})
}
```
<!-- /ds:block hash=c82ac6 -->

<!-- ds:block id=sites-89veq6pm title="examples/site-subpath/main.go" -->
**examples/site-subpath/main.go** · [`examples/site-subpath/main.go:46-54`](../../../examples/site-subpath/main.go#L46-L54)

```go
func sites(ctx context.Context) (docs, shop *site.Site, err error) {
	if docs, err = newSite(ctx, docsDir, docsBase, shopBase+"/"); err != nil {
		return nil, nil, err
	}
	if shop, err = newSite(ctx, shopDir, shopBase, docsBase+"/"); err != nil {
		return nil, nil, err
	}
	return docs, shop, nil
}
```
<!-- /ds:block hash=2711c8 -->

## Step 3 — Mount them on one server

`site.Mount(docs, shop)` returns one `http.Handler` for your own server; `site.ListenAndServeAll(ctx, addr, docs, shop)` serves it and watches each Dev site. From `main.go`:

<!-- ds:block id=handler-zfwgxx9a title="examples/site-subpath/main.go" -->
**examples/site-subpath/main.go** · [`examples/site-subpath/main.go:58-64`](../../../examples/site-subpath/main.go#L58-L64)

```go
func handler(ctx context.Context) (http.Handler, error) {
	docs, shop, err := sites(ctx)
	if err != nil {
		return nil, err
	}
	return site.Mount(docs, shop)
}
```
<!-- /ds:block hash=05085f -->

## Try it

```sh
cd examples/site-subpath
go run .
curl -s localhost:8095/docs/
```

```html
<!doctype html>
<html>
<head><meta name="brick-base" content="/docs"><title>Docs</title><link rel="stylesheet" href="/docs/style.css"></head>
<body>
<nav><a href="/docs/">Docs home</a> <a href="/docs/install/">Install</a> <a href="/shop/">Shop</a></nav>
<main><h1>Docs</h1>
<p>Start with <a href="/docs/install/">installing</a>. Links are written as if the site were at the root: <code>/install/</code>.</p>
</main>
</body>
</html>
```

Every root-absolute `href`, `src`, `action`, `srcset`, `poster` and `formaction` got `/docs`; the `/shop/` link did not (it is in `KeepPaths`); the `<code>` text did not (text is never rewritten). The `brick-base` meta tells the browser runtime where the site ends, so in-place navigation never crosses into the shop. Also:

```text
/docs            301 Location: /docs/
/docs/logo.svg   200 image/svg+xml   (static/ is served under the base too)
/shop/           200 <nav><a href="/shop/">Shop home</a> <a href="/docs/">Docs</a></nav>
/  and /install/ 404                 (outside every base)
```

A static build for `/docs/`:

```sh
brick build -base /docs -o public docs
```

```text
wrote 2 pages, 2 static files and 0 assets to public
```

`public/index.html` holds the same rewritten HTML, at paths without the base, ready to upload as what the host serves under `/docs/`. `go test .` checks the page above, the redirects, the 404s, the export and what happens without `KeepPaths`.

## Pitfalls

- **Without `KeepPaths` a sibling link breaks.** The docs page then links `<a href="/docs/shop/">Shop</a>` (`TestKeepPathsMatters`). The `brick` command has the same switch: `brick build -base /docs` writes `/docs/shop/` too, while `brick build -base /docs -keep /shop/` (comma-separated for several) leaves it as `/shop/`; `brick serve` takes `-keep` as well.
- **Text and code are left alone.** A URL inside `<code>`, a script string or JSON is not rewritten; build such URLs in templates, or keep them relative.
- **Output pages are not rewritten.** Feeds, `robots.txt` and the sitemap get no base, so write their links from `.Site.baseURL`; the built-in sitemap adds the base itself, once ([feeds and sitemap](feeds-and-sitemap.md)).
- **The base must be a plain path.** `docs` (no leading slash) or `//docs` fails with `site: BasePath "…" must start with a single /`; anything else that is not a plain path fails with `must be a plain path such as /docs`. Empty or `/` is the root.
- **One site per base.** Two sites with the same base fail `Mount` with `site: two sites are mounted at "/docs"`; the longest base wins a request, so `/docs/api` can sit beside `/docs`. At most one site may be at the root.
- **Redirects get the base too.** A loader's `brick.redirect("/login/")` answers `Location: /docs/login/` ([protected pages](protected-pages.md)).

## Related

- [Static export](static-export.md) · [Feeds and sitemap](feeds-and-sitemap.md) · [Dev server](dev-server.md)
- [Feature index](../../reference/features.md) rows: hosting under a path (`BasePath`, `site.Mount`), `KeepPaths`, `ListenAndServeAll`.
- Long-form: [guide › A whole site](../../guide.md#a-whole-site--urls-serving-building).
- Example: [`examples/site-subpath`](../../../examples/site-subpath/).
