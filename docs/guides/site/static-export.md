# Static export

## What it is and when you want it

`brick build` (or `Site.Export` from Go) writes the whole site as plain files: every page as `<url>/index.html`, the `static/` folder, component assets and `404.html`. Any static host can serve the result (GitHub Pages, Netlify, an S3 bucket, nginx). You want it when the site has no per-visitor content, or when that content lives in islands served by a separate brick server.

## Before you start

- Read [pages and routes](pages-and-routes.md); this page uses [`examples/site-blog`](../../../examples/site-blog/).
- Pages that come from a pattern need `brick: {paths:}` to be exported ([dynamic routes](dynamic-routes.md)). Per-visitor pages are left out ([protected pages](protected-pages.md)).

## Step 1 — Export from Go

`Export` returns an `ExportReport` with `Pages`, `Static`, `Assets`, `Sitemap` and `Skipped`. From `examples/site-blog/main.go`:

<!-- snippet: examples/site-blog/main.go lines=70-77 -->
```go
	for _, d := range s.Diagnostics() {
		log.Printf("%s: %s", d.File, d.Message)
	}
	if *export != "" {
		rep, err := s.Export(ctx, *export)
		if err != nil {
			log.Fatal(err)
		}
```

## Step 2 — Or export with the command

```sh
brick build -o public theme
```

`-drafts` includes drafts, `-base /docs` writes a site hosted under a path ([hosting under a path](subpath-and-mount.md)), `-keep /shop/` leaves links to a sibling site on the same host unrewritten, `-locales en,fr` writes every language, and `-json` prints the report.

## Try it

```sh
cd examples/site-blog
go run . -export public
```

```text
wrote 6 pages, 1 static files to public
```

```sh
brick build -o public theme
brick build -json -o public theme
```

```text
wrote 6 pages, 1 static files and 0 assets to public
{
  "pages": 6,
  "static": 1,
  "assets": 0,
  "sitemap": false,
  "out": "public"
}
```

The folder it writes:

```text
public/
  .brick-export
  404.html
  index.html
  about/index.html
  blog/index.html
  blog/first-light/index.html
  blog/tide-tables/index.html
  refresh.js
  terms/index.html
```

`public/blog/index.html` holds exactly the bytes the server sends for `/blog/`, and the draft is not there. `TestExport` checks the file list, the report and those bytes, and that a second export into the same folder succeeds.

## Pitfalls

- **Export only overwrites its own folder.** A folder that is not empty and holds no `.brick-export` marker is refused: `site: public is not empty and is not a previous export (no .brick-export); refusing to overwrite it`. Export never deletes files it did not create.
- **Same input, same bytes.** Exporting an unchanged site twice gives identical files, so a deploy diff shows only real changes. Do not add timestamps to templates if you rely on that.
- **No visitor at build time.** A loader redirect becomes a meta-refresh page, a loader error status stops the build (`site: export /posts/gone/: its loader answered 410, which a static file cannot send: …`), loader headers are dropped, and `brick: {per_request: true}` pages are skipped with a warning and listed in `ExportReport.Skipped`.
- **A pattern page needs `paths:`.** Without it: `site: export content/docs/[...path].html: a dynamic page needs brick: {paths: …} to be exported, naming the script function that lists its route values`.
- **Stateful components need keys.** A page with a stateful component exports only with `site.Options.StateKeys` (the CLI reads `BRICK_SECRET`), so the action server can open the sealed state; otherwise `Export` returns `ErrExportNeedsKeys` rather than write dead handles.
- **`error.html` is not exported.** A static file cannot send a status; `404.html` is, at the root.

## Related

- [Dynamic routes](dynamic-routes.md) · [Feeds and sitemap](feeds-and-sitemap.md) · [Hosting under a path](subpath-and-mount.md) · [Protected pages](protected-pages.md)
- [Feature index](../../reference/features.md) rows: static export (`Site.Export`), `brick build`.
- Long-form: [guide › A whole site](../../guide.md#a-whole-site--urls-serving-building).
- Example: [`examples/site-blog`](../../../examples/site-blog/).
