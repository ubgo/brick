# Dynamic routes and `paths:`

## What it is and when you want it

One file can answer many URLs. Name it with a parameter in brackets, `content/posts/[slug].html`, and it answers `/posts/hello/`, `/posts/tides/` and so on; a loader reads the value and fetches the post. A `brick: {paths:}` function lists the values that exist, so unknown ones are a 404 without running the page and `brick build` knows which files to write. You want it when pages come from data (a database, a CMS, a folder of JSON) rather than one file per page.

## Before you start

- Read [pages and routes](pages-and-routes.md).
- Loaders and `paths:` are Starlark functions, so the engine needs [`contrib/script-starlark`](../../../contrib/script-starlark/) (the `brick` command has it built in). See the guide's [components that fetch their own data](../../guide.md#components-that-fetch-their-own-data) for loaders in general.
- The example is [`examples/site-dynamic-routes`](../../../examples/site-dynamic-routes/).

## Step 1 — Wire Starlark into the site

From `examples/site-dynamic-routes/main.go`:

<!-- ds:block id=newsite-t8jsqrzn title="examples/site-dynamic-routes/main.go" -->
**examples/site-dynamic-routes/main.go** · [`examples/site-dynamic-routes/main.go:31-41`](../../../examples/site-dynamic-routes/main.go#L31-L41)

```go
func newSite(ctx context.Context, dir string) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{FS: root.FS(), Engine: brick.Options{Scripting: sc}})
}
```
<!-- /ds:block hash=b77512 -->

## Step 2 — List the values and load one

`all` is the `paths:` function: a list of maps, one key per parameter. `one` is the page's loader: it reads the value from `input["page"]["route"]` and returns the page's data. `scripts/posts.star`:

<!-- snippet: examples/site-dynamic-routes/theme/scripts/posts.star -->
```python
# The posts a real site would read from a database or a CMS.
POSTS = {
    "hello": {"title": "Hello world", "body": "The first post."},
    "tides": {"title": "Reading tide tables", "body": "Two highs a day."},
}

def all():
    """Every URL of content/posts/[slug].html: what brick build writes."""
    return [{"slug": slug} for slug in sorted(POSTS)]

def one(input):
    """The post the URL names; a slug not in POSTS is the 404 page."""
    slug = input["page"]["route"]["slug"]
    if slug not in POSTS:
        brick.not_found()
    return POSTS[slug]
```

## Step 3 — Write the pattern page

What the loader returns is in `.Params`; the raw URL values are in `.Page.Route`. `content/posts/[slug].html`:

<!-- snippet: examples/site-dynamic-routes/theme/content/posts/[slug].html -->
```html
---
use: {posts: ~/scripts/posts.star}
load: posts.one
brick: {paths: posts.all}
---
<h1>{{ .Params.title }}</h1>
<p>{{ .Params.body }}</p>
<p class="slug">slug: {{ .Page.Route.slug }}</p>
```

## Step 4 — Take several segments with a catch-all

`[...name]` takes one or more segments, `[[...name]]` zero or more; either must be the last segment. A `paths:` entry gives a catch-all as an `"a/b"` string or a list. `scripts/docs.star`:

<!-- snippet: examples/site-dynamic-routes/theme/scripts/docs.star -->
```python
def all():
    """A catch-all value is an "a/b" string or a list of segments."""
    return [{"path": "install"}, {"path": "guide/routing"}, {"path": ["guide", "export"]}]
```

`content/docs/[...path].html`:

<!-- snippet: examples/site-dynamic-routes/theme/content/docs/[...path].html -->
```html
---
title: Docs
use: {docs: ~/scripts/docs.star}
brick: {paths: docs.all}
---
<h1>Docs</h1>
<p>path: {{ .Page.Route.path }}</p>
```

## Try it

```sh
cd examples/site-dynamic-routes
go run .
curl -s localhost:8091/posts/hello/
```

```html
<!doctype html>
<html>
<head><title>Hello world</title></head>
<body>
<main><h1>Hello world</h1>
<p>The first post.</p>
<p class="slug">slug: hello</p>
</main>
</body>
</html>
```

What else the server answers (all checked by `go test .`):

```text
/posts/tides/          200  slug: tides
/posts/nope/           404  not on the paths: list, the page never runs
/posts/archive/        200  content/posts/archive.html, a static page beats the pattern
/docs/guide/routing/   200  path: guide/routing
/docs/                 404  [...path] needs at least one segment
```

`brick routes theme` lists patterns with their parameters:

```text
/                 content/index.html
/posts/archive/   content/posts/archive.html
/posts/[slug]/    content/posts/[slug].html  (slug)
/docs/[...path]/  content/docs/[...path].html  (path)
```

`brick build` writes one file per listed value: `posts/hello/index.html`, `posts/tides/index.html`, `docs/install/index.html`, `docs/guide/routing/index.html`, `docs/guide/export/index.html` (`wrote 7 pages, 0 static files and 0 assets to …`). From Go, `s.Patterns()` returns the pattern routes with `Dynamic: true` and `Params`.

## Pitfalls

- **A frontmatter key beats the loader's key of the same name, and it is warned.** With `title: Post` in the pattern page's frontmatter, every post shows "Post", not the loader's title, and `brick check` reports `frontmatter "title" shadows the key of the same name from this file's load: function; the frontmatter value is used`. Leave keys the loader fills out of the frontmatter.
- **No `paths:`, no export.** A pattern page without `brick: {paths:}` is an open world: `brick serve` runs it for any value (the loader decides with `brick.not_found()`), but `brick build` stops with `site: export content/docs/[...path].html: a dynamic page needs brick: {paths: …} to be exported, naming the script function that lists its route values`.
- **Two patterns that match the same URLs are an error**, whatever their parameter names: adding `content/posts/[tag].html` gives `site: content/posts/[slug].html and content/posts/[tag].html match the same URLs (/posts/[slug]/ and /posts/[tag]/); rename one`.
- **Bad entries are named.** A `paths:` entry with a key the pattern lacks fails with `"x" is not a parameter of /posts/[slug]/`, and a value that is empty, `.`, `..` or holds a slash (for a single segment) is refused.
- **Pattern pages are not in `.Site.Pages`.** They have no single URL; list posts from the same data the loader reads.
- **A catch-all must be last.** `content/[...a]/x.html` is refused with `a catch-all must be the last segment`.

## Related

- [Protected and per-request pages](protected-pages.md) (loaders that redirect or answer an error) · [Feeds and sitemap](feeds-and-sitemap.md) (one feed per tag) · [Static export](static-export.md)
- [Feature index](../../reference/features.md) rows: dynamic routes (`[slug]`, `[...rest]`, `paths:`), `brick routes`.
- Long-form: [guide › A whole site](../../guide.md#a-whole-site--urls-serving-building).
- Example: [`examples/site-dynamic-routes`](../../../examples/site-dynamic-routes/).
