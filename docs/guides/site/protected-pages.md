# Protected and per-request pages

## What it is and when you want it

The site caches each page's render and shares it between visitors, which is what makes it fast. Some content depends on who is asking: an admin panel, "my orders", a greeting. brick gives you two ways to render per visitor without giving up the cache for everything else: an **island** (one component filled on every response, while the page around it stays cached) and a **per-request page** (`brick: {per_request: true}`, the whole page rendered for each visitor). An island's loader can also turn a visitor away with a redirect or an error status.

## Before you start

- Read [pages and routes](pages-and-routes.md) and [dynamic routes](dynamic-routes.md) (loaders).
- Your Go program already knows who the visitor is (a session, a token). You pass that to brick with `site.Options.Request`; brick never reads cookies itself.
- The example is [`examples/site-protected`](../../../examples/site-protected/). It trusts a `user` cookie as the session, which only a demo should do.

## Step 1 — Tell the site who is asking

`Request` runs for every response and returns render options; `brick.Provide("user", …)` makes the value `.Ctx.user` in islands and per-request pages, and `input["ctx"]["user"]` in their loaders. Here the visitor is whatever the `user` cookie says (`userCookie`), which only a demo may trust; a real program reads its own session. From `examples/site-protected/main.go`:

<!-- ds:block id=visitor-xbkx7jr2 title="examples/site-protected/main.go" -->
**examples/site-protected/main.go** · [`examples/site-protected/main.go:36-41`](../../../examples/site-protected/main.go#L36-L41)

```go
func visitor(r *http.Request) []brick.RenderOption {
	if c, err := r.Cookie(userCookie); err == nil {
		return []brick.RenderOption{brick.Provide("user", c.Value)}
	}
	return nil
}
```
<!-- /ds:block hash=3ee76c -->

<!-- ds:block id=newsite-c9ajaymd title="examples/site-protected/main.go" -->
**examples/site-protected/main.go** · [`examples/site-protected/main.go:46-60`](../../../examples/site-protected/main.go#L46-L60)

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
	return site.New(ctx, site.Options{
		FS:      root.FS(),
		Engine:  brick.Options{Scripting: sc},
		Request: visitor,
	})
}
```
<!-- /ds:block hash=60a987 -->

## Step 2 — Guard with an island's loader

The guard must run for each visitor, so it goes in an island's loader, never in the page's own loader (that one runs once, for nobody, and its answer would be cached for everyone). `brick.redirect(url)` answers 303 by default; `brick.error(status, message)` answers any 4xx or 5xx. `scripts/auth.star`:

<!-- snippet: examples/site-protected/theme/scripts/auth.star -->
```python
def admin(input):
    """Signed out goes to /login/; signed in but not admin is refused."""
    user = input["ctx"].get("user")
    if not user:
        brick.redirect("/login/")
    if user != "admin":
        brick.error(403, "Admins only.")
    return {"name": user}
```

`components/AdminPanel.html` is the island; `inject:` declares the context value it reads:

<!-- snippet: examples/site-protected/theme/components/AdminPanel.html -->
```html
---
brick: {island: true}
inject: {user: {default: ""}}
use: {auth: ~/scripts/auth.star}
load: auth.admin
---
<section>Welcome, {{ .Props.name }}. Secret numbers: 42.</section>
```

`content/admin.html` is an ordinary, cached page:

<!-- snippet: examples/site-protected/theme/content/admin.html -->
```html
---
title: Admin
---
<h1>Admin</h1>
<AdminPanel />
```

`content/error.html` renders every `brick.error` (see [404 and error pages](not-found-and-error-pages.md)).

## Step 3 — Render a whole page per visitor

When most of the page is personal, mark the page instead. `content/dashboard.html`:

<!-- snippet: examples/site-protected/theme/content/dashboard.html -->
```html
---
title: Dashboard
brick: {per_request: true}
---
<h1>Dashboard</h1>
<p>{{ with .Ctx.user }}Hello, {{ . }}.{{ else }}Hello, guest.{{ end }}</p>
```

## Try it

```sh
cd examples/site-protected
go run .
curl -i localhost:8092/admin/
curl -i --cookie user=bob localhost:8092/admin/
curl -i --cookie user=ada localhost:8092/dashboard/
```

```text
HTTP/1.1 303 See Other
Cache-Control: private, no-store
Content-Type: text/html; charset=utf-8
Location: /login/
Content-Length: 34

<a href="/login/">See Other</a>.
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

```text
HTTP/1.1 200 OK
Cache-Control: private, no-cache
Content-Type: text/html; charset=utf-8
Content-Length: 136

<!doctype html>
<html>
<head><title>Dashboard</title></head>
<body>
<main><h1>Dashboard</h1>
<p>Hello, ada.</p>
</main>
</body>
</html>
```

With `--cookie user=admin`, `/admin/` answers 200 with `<section>Welcome, admin. Secret numbers: 42.</section>`, and a signed-out request right after it still gets the 303: the shell is cached, the island never is. `go test .` checks this sequence (`TestAdminIsland`), the dashboard for three visitors (`TestPerRequestPage`) and the export below.

`brick build` has no visitor. It writes `admin/index.html` as a redirect page, never the panel, and skips the dashboard:

```text
time=… level=WARN msg="site: export skips a per-request page; serve it with brick serve or site.Handler" page=content/dashboard.html url=/dashboard/
wrote 2 pages, 0 static files and 0 assets to public
skipped /dashboard/: brick: {per_request: true} pages are served, not built
```

`public/admin/index.html`:

```html
<!doctype html><html><head><meta charset="utf-8"><title>Redirecting…</title><meta name="robots" content="noindex"><link rel="canonical" href="/login/"><meta http-equiv="refresh" content="0; url=/login/"></head><body><a href="/login/">/login/</a></body></html>
```

## Pitfalls

- **A guard in the page's own loader protects nothing.** The page loader runs once and its result is cached and shared; put visitor checks in an island's loader or on a per-request page.
- **Answers are always `private, no-store`.** An island's redirect or error is never cached, so one visitor's answer cannot reach another. A per-request page is `private, no-cache` (`site.PerRequestCacheControl`) unless its loader sets its own `Cache-Control`.
- **Redirect targets are locked.** A path works; an absolute URL only on the request's own host or one in `brick.Options.RedirectHosts`. Anything else (`//evil.example`, `javascript:`) is a logged 500, never followed. 301 is refused (browsers cache it forever); use 308 for permanent moves.
- **An error status stops `brick build`** when a page or page loader answers it (`site: export /posts/gone/: its loader answered 410, which a static file cannot send: …`); an island's redirect becomes a redirect page instead, as above.
- **`brick.header` cannot set cookies.** `Set-Cookie` is refused because a page's render is shared; sessions belong to your Go program, before brick renders.
- **A boundary does not catch an answer.** `<brick:boundary>` around the island never turns a redirect or 403 into its fallback, so a turned-away visitor never sees the page with status 200.
- **Prefer an island when only a corner is personal.** A per-request page renders everything for every visitor, including its loaders.

## Related

- [404 and error pages](not-found-and-error-pages.md) · [Dynamic routes](dynamic-routes.md) · [Static export](static-export.md)
- [Feature index](../../reference/features.md) rows: islands, per-request pages (`per_request`), loader answers (`brick.redirect`, `brick.error`, `brick.header`), `site.Options.Request`.
- Long-form: [guide › Protect a page, redirect, and answer with a status](../../guide.md#protect-a-page-redirect-and-answer-with-a-status) and [cookbook › Protecting a page](../../cookbook.md#protecting-a-page).
- Example: [`examples/site-protected`](../../../examples/site-protected/).
