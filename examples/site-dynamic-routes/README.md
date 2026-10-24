# site-dynamic-routes

One file, many URLs. `content/posts/[slug].html` answers `/posts/hello/` and `/posts/tides/`: its `brick: {paths:}` function lists the slugs and its loader fills the page from the one in the URL. `content/posts/archive.html` shows a static page beating the pattern, and `content/docs/[...path].html` a catch-all over several segments.

```sh
go run .                    # http://localhost:8091/posts/hello/
go run . -export public     # one file per listed value
go test .                   # every URL's status and HTML, and the exported files
```

Guide: [dynamic routes and paths:](../../docs/guides/site/dynamic-routes.md).

## What the tests prove

- TestDynamicRoutes: every URL's status and HTML, the static page beating the pattern, the catch-all taking several segments, `/docs/` being a 404, and `Patterns()`.
- TestExportDynamic: one exported file per `paths:` entry.

A passing run from `examples/`:

```text
$ go test ./site-dynamic-routes/
ok  	github.com/ubgo/brick/examples/site-dynamic-routes	0.031s
```
