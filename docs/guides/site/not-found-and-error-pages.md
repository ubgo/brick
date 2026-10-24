# 404 and error pages

## What it is and when you want it

Two files in `content/` are not pages of the site but answers: `404.html` is served, with status 404, for any URL no page has, and `error.html` renders every error status a loader answers (`brick.error(403, …)`, `brick.error(410, …)`). Both go through the layout like any page. You want them so a wrong link or a refused visitor sees your site, not a bare line of text.

## Before you start

- Read [pages and routes](pages-and-routes.md). The 404 page is from [`examples/site-blog`](../../../examples/site-blog/); the error page is from [`examples/site-protected`](../../../examples/site-protected/), where an island's loader answers 403 (see [protected pages](protected-pages.md)).
- Either file may be markdown instead (`404.md`, `error.md`).

## Step 1 — Add the 404 page

`examples/site-blog/theme/content/404.html`:

<!-- snippet: examples/site-blog/theme/content/404.html -->
```html
---
title: Not found
---
<h1>Nothing here</h1>
<p><a href="/">Back home</a></p>
```

A dynamic page also answers with it: a value not on its `paths:` list, or a loader that calls `brick.not_found()` (see [dynamic routes](dynamic-routes.md)).

## Step 2 — Add the error page

The answer is in `.Ctx.error`: `.status` and `.message`. `examples/site-protected/theme/content/error.html`:

<!-- snippet: examples/site-protected/theme/content/error.html -->
```html
---
title: Error
---
<h1>Error {{ .Ctx.error.status }}</h1>
<p>{{ .Ctx.error.message }}</p>
```

## Try it

```sh
cd examples/site-blog && go run . &
curl -i localhost:8090/nope/
```

```text
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Content-Length: 254

<!doctype html>
<html>
<head><title>Not found · Field Notes</title></head>
<body>
<nav><a href="/">Home</a> <a href="/blog/">Blog</a> <a href="/about/">About</a></nav>
<main><h1>Nothing here</h1>
<p><a href="/">Back home</a></p>
</main>
</body>
</html>
```

```sh
cd examples/site-protected && go run . &
curl -i --cookie user=bob localhost:8092/admin/
```

```text
HTTP/1.1 403 Forbidden
Cache-Control: private, no-store
Content-Type: text/html; charset=utf-8
Content-Length: 133

<!doctype html>
<html>
<head><title>Error</title></head>
<body>
<main><h1>Error 403</h1>
<p>Admins only.</p>
</main>
</body>
</html>
```

`TestNotFound` (site-blog) and `TestAdminIsland` (site-protected) check both. `brick build` writes `404.html` at the root of the export, which most static hosts (GitHub Pages, Netlify, Cloudflare Pages) serve for unknown URLs; `error.html` is not written, because a static file cannot send a status.

From Go, `s.RenderNotFound(ctx)` and `s.RenderError(ctx, he)` render them for a host with its own router; each returns `ok == false` when the project has no such file.

## Pitfalls

- **No file, plain text.** Without `404.html` an unknown URL gets Go's `404 page not found`; without `error.html` a loader's error is sent as plain text, its message or else the status text (`Forbidden`).
- **Not pages of the site.** Neither file has a URL of its own, neither is in `.Site.Pages`, `brick routes` or the sitemap; `/404/` is itself a 404.
- **Only at the top of `content/`.** `content/blog/404.html` is an ordinary page at `/blog/404/`, not a section's not-found page.
- **The error message is the author's text.** `.Ctx.error.message` is what the loader passed to `brick.error`; never put internal details there, because it is shown to the visitor.

## Related

- [Protected and per-request pages](protected-pages.md) · [Dynamic routes](dynamic-routes.md) · [Static export](static-export.md)
- [Feature index](../../reference/features.md) rows: `404.html` and `error.html`.
- Long-form: [guide › Protect a page, redirect, and answer with a status](../../guide.md#protect-a-page-redirect-and-answer-with-a-status).
- Examples: [`examples/site-blog`](../../../examples/site-blog/), [`examples/site-protected`](../../../examples/site-protected/).
