# site-subpath

Two sites on one server, each under its own path: `docs/` at `/docs/` and `shop/` at `/shop/`. Both projects are written as if they lived at the root; `site.Options.BasePath` adds the base to every root-absolute URL they serve or export, `KeepPaths` leaves the link to the other site alone, and `site.Mount` puts both behind one `http.Handler`.

```sh
go run .                       # http://localhost:8095/docs/ and /shop/
go run . -export public        # the docs site, built for hosting at /docs/
brick build -base /docs docs   # the same with the brick command (no KeepPaths there)
go test .                      # the rewritten pages, redirects, 404s and the export
```

Guide: [hosting under a path, and several sites on one server](../../docs/guides/site/subpath-and-mount.md).
