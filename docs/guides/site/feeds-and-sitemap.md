# Feeds, robots.txt and the sitemap

## What it is and when you want it

Every page URL ends in a slash and gets the layout, which is wrong for an RSS feed or `robots.txt`. An **output page** fixes that: name the file with the format before `.html` (`feed.xml.html`) and it is served at `/feed.xml` with the right `Content-Type` and no layout, while still being a brick page with frontmatter, `.Site.Pages` and loaders. The **built-in sitemap** writes `/sitemap.xml` from your routes, with absolute URLs, `lastmod` and language alternates. You want both before a site goes public.

## Before you start

- Read [pages and routes](pages-and-routes.md). A feed per tag also uses [dynamic routes](dynamic-routes.md).
- The example is [`examples/site-feeds-sitemap`](../../../examples/site-feeds-sitemap/).

## Step 1 — Give the site an absolute base URL

Feeds and the sitemap need absolute links. `site.yaml`:

<!-- snippet: examples/site-feeds-sitemap/theme/site.yaml -->
```yaml
title: Demo Blog
baseURL: https://example.com
```

## Step 2 — Write the feed

`content/feed.xml.html`. The leading `<?xml …?>` prolog is written as is.

<!-- snippet: examples/site-feeds-sitemap/theme/content/feed.xml.html -->
```html
---
title: Feed
---
<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0">
<channel>
<title>{{ .Site.title }}</title>
<link>{{ .Site.baseURL }}/</link>
{{- range .Site.Pages }}{{ if eq .Section "blog" }}
<item><title>{{ .Title }}</title><link>{{ $.Site.baseURL }}{{ .URL }}</link><description>{{ .Description }}</description></item>
{{- end }}{{ end }}
</channel>
</rss>
```

A post, `content/blog/hello.html`:

<!-- snippet: examples/site-feeds-sitemap/theme/content/blog/hello.html -->
```html
---
title: Hello & welcome
date: 2026-10-01
description: The first post
tags: [go]
---
<p>Hi</p>
```

## Step 3 — Write robots.txt

`content/robots.txt.html` needs no frontmatter:

<!-- snippet: examples/site-feeds-sitemap/theme/content/robots.txt.html -->
```html
User-agent: *
Disallow: /private/
Sitemap: {{ .Site.baseURL }}/sitemap.xml
```

## Step 4 — One feed per tag

Put the parameter in a folder name; the file stays an output page. `scripts/tags.star`:

<!-- snippet: examples/site-feeds-sitemap/theme/scripts/tags.star -->
```python
def list():
    """One feed per tag: /tags/go/feed.xml and /tags/web/feed.xml."""
    return [{"tag": "go"}, {"tag": "web"}]
```

`content/tags/[tag]/feed.xml.html` keeps the posts whose `tags:` list holds the tag:

<!-- snippet: examples/site-feeds-sitemap/theme/content/tags/[tag]/feed.xml.html -->
```html
---
title: Tag feed
use: {tags: ~/scripts/tags.star}
brick: {paths: tags.list}
---
<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0">
<channel>
<title>{{ .Site.title }}: {{ .Page.Route.tag }}</title>
{{- $tag := .Page.Route.tag }}{{ range $p := .Site.Pages }}{{ range $p.Params.tags }}{{ if eq . $tag }}
<item><title>{{ $p.Title }}</title><link>{{ $.Site.baseURL }}{{ $p.URL }}</link></item>
{{- end }}{{ end }}{{ end }}
</channel>
</rss>
```

## Step 5 — Turn the sitemap on

From Go, set `Sitemap`; an empty `BaseURL` takes `site.yaml`'s `baseURL`. From `examples/site-feeds-sitemap/main.go`:

<!-- snippet: examples/site-feeds-sitemap/main.go lines=39-43 -->
```go
	return site.New(ctx, site.Options{
		FS:      root.FS(),
		Engine:  brick.Options{Scripting: sc},
		Sitemap: &site.Sitemap{},
	})
```

For the `brick` command, `brick.yaml`:

<!-- snippet: examples/site-feeds-sitemap/theme/brick.yaml -->
```yaml
sitemap: {}
```

Leave a page out with `brick: {sitemap: false}`. `content/private.html`:

<!-- snippet: examples/site-feeds-sitemap/theme/content/private.html -->
```html
---
title: Private
brick: {sitemap: false}
---
<p>no index</p>
```

## Try it

```sh
cd examples/site-feeds-sitemap
go run .
curl -i localhost:8093/feed.xml
```

```text
HTTP/1.1 200 OK
Content-Type: application/xml; charset=utf-8
Content-Length: 421

<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0">
<channel>
<title>Demo Blog</title>
<link>https://example.com/</link>
<item><title>Routing by file name</title><link>https://example.com/blog/routing/</link><description>Where URLs come from</description></item>
<item><title>Hello &amp; welcome</title><link>https://example.com/blog/hello/</link><description>The first post</description></item>
</channel>
</rss>
```

`curl -i localhost:8093/robots.txt`:

```text
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Content-Length: 75

User-agent: *
Disallow: /private/
Sitemap: https://example.com/sitemap.xml
```

`curl localhost:8093/sitemap.xml` (the routing post has `lastmod: 2026-10-09`, which wins over its `date:`):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">
  <url>
    <loc>https://example.com/</loc>
  </url>
  <url>
    <loc>https://example.com/blog/hello/</loc>
    <lastmod>2026-10-01</lastmod>
  </url>
  <url>
    <loc>https://example.com/blog/routing/</loc>
    <lastmod>2026-10-09</lastmod>
  </url>
</urlset>
```

`/tags/web/feed.xml` lists only the routing post; `/tags/rust/feed.xml` (not on the list) and `/feed.xml/` (a file URL with a slash) are 404s. `brick build -o public theme` prints `wrote 8 pages, 0 static files and 0 assets to public` and `wrote public/sitemap.xml`, and writes `feed.xml`, `robots.txt`, `tags/go/feed.xml`, `tags/web/feed.xml` and `sitemap.xml` with no `index.html` folders. `go test .` checks every body above, the 404s and the exported files. With locales on, each sitemap entry also lists its `hreflang` alternates ([languages](languages.md)).

## Pitfalls

- **Only `.xml`, `.rss`, `.atom` and `.txt`.** html/template escapes for HTML, which is valid XML and harmless in text, but breaks JSON, JS, CSS and CSV. `feed.json.html` stops the site: `content/feed.json.html: .json output pages are not supported: html/template escapes text for HTML, so a quote becomes &#34; and the JSON breaks; serve JSON from a Go handler, or pass data to the page through a loader`.
- **It is still HTML escaping.** A `&` in a title becomes `&amp;` (right for XML); in `robots.txt` a literal `&` is written `&amp;` too.
- **No layout unless you name one**, and nothing HTML is injected: no dev toolbar, live reload, `<base>` or URL rewrite. Under a `BasePath` nothing in an output page is rewritten, so always build links from `.Site.baseURL`.
- **The sitemap refuses to start without an absolute base**: `site: the sitemap needs an absolute base URL (https://example.com): set Options.Sitemap.BaseURL (brick.yaml sitemap: {baseURL: …}) or baseURL in site.yaml; got ""`.
- **What the sitemap leaves out:** drafts (even in `brick serve`), output pages, the 404 page, pattern pages without `paths:`, and pages with `brick: {sitemap: false}`. A page excluded from the sitemap is still served and exported; add it to `robots.txt` too if crawlers must stay away.
- **Your own sitemap wins.** A `content/sitemap.xml.html` or `static/sitemap.xml` replaces the built-in one.
- **Output pages are not in `.Site.Pages`**, so a feed never lists itself.

## Related

- [Dynamic routes](dynamic-routes.md) · [Languages](languages.md) · [Static export](static-export.md) · [Hosting under a path](subpath-and-mount.md)
- [Feature index](../../reference/features.md) rows: output pages (feeds, `robots.txt`), dynamic output pages, the sitemap.
- Long-form: [guide › Feeds and robots.txt](../../guide.md#feeds-and-robotstxt--output-pages) and [guide › The sitemap](../../guide.md#the-sitemap).
- Example: [`examples/site-feeds-sitemap`](../../../examples/site-feeds-sitemap/).
