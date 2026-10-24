# The render cache

## What it is and when you want it

The site renders each page once and keeps the result; every later request for that URL is served from memory. It knows which files each render read, so editing a file drops only the pages that used it, and a cache tag lets data that lives outside the project (a database, a CMS) drop the pages built from it. You get it for free; you need this page when you serve changing data or run your own cache in front of brick.

## Before you start

- Read [pages and routes](pages-and-routes.md); this page uses [`examples/site-blog`](../../../examples/site-blog/), whose blog index carries a tag.
- Per-visitor content is never cached: islands are filled on every response and `per_request` pages skip the cache ([protected pages](protected-pages.md)).

## Step 1 — Tag the pages that read outside data

A `cache: {tags: […]}` key in a page's (or layout's) frontmatter records tags on every render of it; `{{ cacheTag "name" }}` does the same from inside a template or component. `content/blog/index.html`:

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

## Step 2 — Drop pages when something changes

- `s.InvalidateTags("posts")` drops every cached page that recorded the tag, returns their URLs and tells live-reloading browsers. Call it from your CMS webhook or after a write to the table a loader reads.
- `s.Apply(ctx, changes)` is what the file watcher calls: it drops only the pages whose render read a changed file. `Watch` and `ListenAndServe` (in Dev) call it for you.

From `examples/site-blog/main_test.go` (`TestRenderCache`):

<!-- snippet: examples/site-blog/main_test.go lines=222-242 -->
```go
		t.Fatal(err)
	}
	for _, u := range []string{"/blog/", "/about/"} {
		if _, err := s.Render(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := s.Render(ctx, "/blog/")
	b, _ := s.Render(ctx, "/blog/")
	if a != b {
		t.Fatal("a second render of the same URL did not come from the cache")
	}
	dropped, err := s.Apply(ctx, []watch.Change{{Path: "components/PostLink.html", Op: watch.Modified}})
	if err != nil || !slices.Equal(dropped, []string{"/blog/"}) {
		t.Fatalf("Apply dropped %v, %v", dropped, err)
	}
	if _, err := s.Render(ctx, "/blog/"); err != nil {
		t.Fatal(err)
	}
	if got := s.InvalidateTags("posts"); !slices.Equal(got, []string{"/blog/"}) {
		t.Fatalf("InvalidateTags dropped %v", got)
```

Editing `PostLink.html` drops `/blog/` (the only page that renders a `PostLink`) and keeps `/about/`; the tag drops `/blog/` again.

## Try it

```sh
cd examples/site-blog
go test -run TestRenderCache -v .
```

```text
=== RUN   TestRenderCache
--- PASS: TestRenderCache (0.01s)
PASS
```

In the dev server the toolbar shows it directly: the first request for `/blog/` says `cache miss`, the second `cache hit` ([dev server](dev-server.md)).

## Pitfalls

- **Some edits clear everything.** A new or deleted file, a change to `site.yaml`, `data/` or `i18n/`, or an edit that changes the page index (a title, a date, a URL) can change any page, so `Apply` clears the whole cache and returns `["*"]`.
- **Loader data is not watched.** brick knows the files a render read, not the rows a loader fetched. A page whose loader reads a database stays cached until you call `InvalidateTags` (or restart); tag such pages.
- **A failed render is never cached**, and neither is a loader's not-found, so a fixed bug or a newly created record shows on the next request.
- **The cache is a `Result`, not bytes.** `Site.Render` returns the cached `*brick.Result`; call `Result.Bytes` (with a nonce, if you use one) per response, and never modify the result.
- **Your own cache must skip per-visitor pages.** A host that caches responses itself skips routes with `Route.PerRequest` and every response that carries a filled island.
- **It is memory.** The cache lives in one process; several servers each keep their own, so call `InvalidateTags` on each (for example from a pub/sub message).

## Related

- [Dev server](dev-server.md) · [Protected and per-request pages](protected-pages.md) · [Pages and routes](pages-and-routes.md)
- [Feature index](../../reference/features.md) rows: render cache and cache tags (`cache: {tags:}`, `cacheTag`, `InvalidateTags`).
- Long-form: [guide › A whole site](../../guide.md#a-whole-site--urls-serving-building).
- Example: [`examples/site-blog`](../../../examples/site-blog/).
