# site-blog

A small blog built with the `site` package: pages at URLs taken from their file paths, a blog index made from `.Site.Pages`, a markdown post with a table of contents, a collection schema for the posts, a draft, a 404 page, the page cache and a static export.

```sh
go run .                    # http://localhost:8090
go run . -dev -drafts       # live reload, the dev toolbar, the playground and drafts
go run . -export public     # write the static site to ./public
go run . -watch             # your own http.Server plus site.Watch, no Dev
go test .                   # renders every page through the real handler and checks the exact HTML
```

The same folder works with the `brick` command: `brick serve theme`, `brick routes theme`, `brick check theme`, `brick build -o public theme`.

Guides that use it: [pages and routes](../../docs/guides/site/pages-and-routes.md), [markdown and headings](../../docs/guides/site/markdown-and-headings.md), [collections and drafts](../../docs/guides/site/collections-and-drafts.md), [404 and error pages](../../docs/guides/site/not-found-and-error-pages.md), [dev server](../../docs/guides/site/dev-server.md), [static export](../../docs/guides/site/static-export.md), [render cache](../../docs/guides/site/render-cache.md).
