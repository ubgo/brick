# site-feeds-sitemap

Output pages and the built-in sitemap. `content/feed.xml.html` is served at `/feed.xml` as `application/xml`, `content/robots.txt.html` at `/robots.txt` as `text/plain`, `content/tags/[tag]/feed.xml.html` writes one feed per tag, and `site.Options.Sitemap` (or `sitemap: {}` in `brick.yaml`) adds `/sitemap.xml` with absolute URLs and `lastmod`.

```sh
go run .                    # http://localhost:8093/feed.xml
go run . -export public     # feed.xml, robots.txt, tags/*/feed.xml and sitemap.xml
go test .                   # every body and Content-Type, the 404s and the exported files
```

Guide: [feeds, robots.txt and the sitemap](../../docs/guides/site/feeds-and-sitemap.md).

## What the tests prove

- TestOutputPages: the feed, the tag feeds and robots.txt answer with their own content types and no layout.
- TestExportFeeds: output pages are written at their file names, one feed per tag, and `sitemap.xml` leaves `private.html` out.

A passing run from `examples/`:

```text
$ go test ./site-feeds-sitemap/
ok  	github.com/ubgo/brick/examples/site-feeds-sitemap	0.032s
```
