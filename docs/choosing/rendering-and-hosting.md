# Choosing a rendering and hosting mode

The same project can be served four ways: exported to static files, served live by the `brick` command during development, served from your own Go binary with the `site` package, or embedded as a bare engine in an existing `net/http` app. They differ in what is rendered when, what is cached, and what a visitor-specific page can do. Pick by the content you have.

## The decision

| Mode | What runs | Rendered | Per-visitor content | Stateful components | Pick it when |
|---|---|---|---|---|---|
| Static export (`brick build`, `Site.Export`) | nothing at request time | once, at build | none; a loader redirect becomes a meta-refresh page, `per_request` pages are skipped with a warning | only with `BRICK_SECRET` / `site.Options.StateKeys`, and an action server somewhere that holds the same keys | the site is the same for everyone and any static host will do |
| Dev server (`brick serve`, `site.Options{Dev: true}`) | live reload, a component playground, a toolbar, inline errors | on first request, cached, reloaded on file change | yes | yes (a process key when `BRICK_SECRET` is unset) | you are writing the site; never production |
| Site in your binary (`site.New` with `Dev` false, then `s.Handler()`) | the full site layer | on first request, then cached | islands per request, `per_request` pages, protected pages | yes, with a state store you choose | production with dynamic content, actions or a database |
| Engine alone (`brick.New`, `Render`, `RenderChunks` + `Fill`) | only the pieces you mount | when you call it | whatever your handler passes in | needs `Options.States` set | you have an existing Go app and want components in some of its handlers |
| Mounted under a path (`BasePath`, `site.Mount`) | any of the site modes above | as above | as above | as above | the site does not own the whole domain, or several sites share one server |

## Which per-visitor mechanism

| Need | Use | Cached | Guide | Example |
|---|---|---|---|---|
| One part differs per visitor | an island, filled on each response | the page around it is | [Islands](../guides/interactivity/islands-lazy-and-streaming.md) | [site-protected](../../examples/site-protected/) |
| The whole page differs per visitor | `brick: {per_request: true}` | no, skipped by the cache and by export | [Protected and per-request pages](../guides/site/protected-pages.md) | [site-protected](../../examples/site-protected/) |
| Turn a visitor away | an island loader's `brick.redirect` or `brick.error` | n/a | [Protected and per-request pages](../guides/site/protected-pages.md) | [site-protected](../../examples/site-protected/) |
| The URL carries a value | dynamic routes (`[slug]`, `[...rest]`, `paths:`) | per URL | [Dynamic routes](../guides/site/dynamic-routes.md) | [site-dynamic-routes](../../examples/site-dynamic-routes/) |

## If X then Y

- **A blog or docs site with no accounts:** static export. Deploy the folder to GitHub Pages, Netlify, S3 or nginx. Use `BasePath` (`brick build -base /docs`) for a project site that lives under a path.
- **A static site with one dynamic corner (a sign-in widget, a cart):** export the site, serve the dynamic part from a brick server, and share one `BRICK_SECRET` so the exported pages' sealed handles open there. Without keys `Export` returns `ErrExportNeedsKeys` rather than write dead handles.
- **A site with data in a database:** the site in your binary, with `Dev` off. Pages are cached; data the cache cannot see needs `InvalidateTags`. Loader data is not watched.
- **You already have a router and want a few components:** the engine alone. Render with `RenderChunks` once, `Fill` per request, mount `Engine.ActionHandler()` and `contrib/assets-http`. See [Host API](../guides/production/host-api.md).
- **Several sites, one server:** `site.Mount`, each site with its own folder, engine, cache and live reload.
- **Behind a CDN:** share pages, never per-visitor responses. Skip routes with `Route.PerRequest` and every response that carries a filled island.

## The render cache

The site renders a page once and serves later requests from memory. It knows which files each render read, so editing one file drops only the pages that used it. The cache lives in one process: several servers each keep their own, so call `InvalidateTags` on each (for example from a pub/sub message). A failed render and a loader's not-found are never cached. Details: [The render cache](../guides/site/render-cache.md).

## Pitfalls

- **Never run Dev in production.** It serves the playground (any component with any props), the instance inspector and file paths in error pages. Every Dev route answers 404 when `Dev` is false.
- **`s.Handler()` alone does not watch files.** For live reload from your own server run `go s.Watch(ctx, watch.Options{})` beside it.
- **Export only overwrites its own folder.** A non-empty folder without a `.brick-export` marker is refused, and `error.html` is not exported (a static file cannot send a status), `404.html` is.
- **A page with a stateful component is private.** Its handle belongs to one visitor; do not cache the HTML shared. The site layer sets `Cache-Control` for such a response (`StateCacheControl`).
- **Engine alone, no `States`:** a page with a stateful component is an error. Set `Options.States` ([choosing a state store](state-store.md)).
- **Lazy islands need tickets that outlive a restart** if you run several servers: seal them (`site.Options.StateKeys`), or a ticket from another server answers 410.

## Guides and examples

| Mode | Guide | Example |
|---|---|---|
| Static export | [Static export](../guides/site/static-export.md) | [site-blog](../../examples/site-blog/) |
| Dev server | [The dev server](../guides/site/dev-server.md) | [site-blog](../../examples/site-blog/) |
| Site in your binary | [Pages and routes](../guides/site/pages-and-routes.md) | [site-dynamic-routes](../../examples/site-dynamic-routes/) |
| Engine alone | [Host API](../guides/production/host-api.md) | [production-host-api](../../examples/production-host-api/), [engineonly](../../examples/engineonly/) |
| Under a path, several sites | [Hosting under a path](../guides/site/subpath-and-mount.md) | [site-subpath](../../examples/site-subpath/) |
| Render cache | [The render cache](../guides/site/render-cache.md) | [site-blog](../../examples/site-blog/) |

Back to [choosing guides](README.md).
