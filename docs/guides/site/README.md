# Site guides

The `site` package and the `brick` command turn a folder of pages into a website: URLs from file paths, a dev server, a static export. Each guide below is one task, built from a runnable example under `examples/site-*` whose test checks the exact output the guide shows.

| Guide | What it covers | Example |
|---|---|---|
| [Pages, URLs and `.Site.Pages`](pages-and-routes.md) | files to URLs, `brick: {url:}`, the page index, `Site.Routes`, `brick routes` | [site-blog](../../../examples/site-blog/) |
| [Markdown pages and a table of contents](markdown-and-headings.md) | `.md` pages, goldmark, heading ids, `.Page.Headings` | [site-blog](../../../examples/site-blog/) |
| [Collections and drafts](collections-and-drafts.md) | `_collection.yaml` schemas, `brick check`, `draft: true` | [site-blog](../../../examples/site-blog/) |
| [404 and error pages](not-found-and-error-pages.md) | `content/404.html`, `content/error.html`, `.Ctx.error` | [site-blog](../../../examples/site-blog/), [site-protected](../../../examples/site-protected/) |
| [Dynamic routes and `paths:`](dynamic-routes.md) | `[slug]`, `[...rest]`, `brick: {paths:}`, loaders, `.Page.Route` | [site-dynamic-routes](../../../examples/site-dynamic-routes/) |
| [Protected and per-request pages](protected-pages.md) | `site.Options.Request`, island guards, `brick.redirect` / `brick.error`, `per_request` | [site-protected](../../../examples/site-protected/) |
| [Feeds, robots.txt and the sitemap](feeds-and-sitemap.md) | output pages, one feed per tag, `site.Options.Sitemap` | [site-feeds-sitemap](../../../examples/site-feeds-sitemap/) |
| [Languages](languages.md) | `Locales`, `about.fr.html` variants, `t`, `.Page.Translations`, hreflang | [site-i18n](../../../examples/site-i18n/) |
| [Images](images.md) | `image`, resized copies, `srcset`, WebP encoders | [site-images](../../../examples/site-images/) |
| [Tailwind](tailwind.md) | `tailwind:` in brick.yaml, `tailwind.Build`, `OnChange` | [site-tailwind](../../../examples/site-tailwind/) |
| [Hosting under a path](subpath-and-mount.md) | `BasePath`, `KeepPaths`, `site.Mount`, `brick build -base` | [site-subpath](../../../examples/site-subpath/) |
| [The dev server](dev-server.md) | `Dev`, live reload, the playground, the toolbar, inline errors | [site-blog](../../../examples/site-blog/) |
| [Static export](static-export.md) | `Site.Export`, `brick build`, what a build cannot contain | [site-blog](../../../examples/site-blog/) |
| [The render cache](render-cache.md) | how pages are cached, `Apply`, `cache: {tags:}`, `InvalidateTags` | [site-blog](../../../examples/site-blog/) |

Every feature with its guide and example is listed in the [feature index](../../reference/features.md). The long single-page read is the [guide](../../guide.md#a-whole-site--urls-serving-building).
