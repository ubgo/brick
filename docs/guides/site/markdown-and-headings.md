# Markdown pages and a table of contents

## What it is and when you want it

A `.md` file under `content/` is a page like an `.html` one: same URL rules, same frontmatter, same layouts. Its layout can read `.Page.Headings` to print a table of contents that links to each heading. You want it for posts and docs you would rather write in markdown than in HTML.

## Before you start

- Read [pages and routes](pages-and-routes.md); this page uses the same [`examples/site-blog`](../../../examples/site-blog/).
- Markdown needs a backend. The engine names none; [`contrib/markdown-goldmark`](../../../contrib/markdown-goldmark/) is the one in this repository, and the `brick` command registers it for you.

## Step 1 — Register the backend with heading ids

Heading ids are what the table of contents links to, so turn them on. From `examples/site-blog/main.go`:

<!-- snippet: examples/site-blog/main.go lines=42-47 -->
```go
// ds:def id=newsitefs-5nq2ppfu
func newSiteFS(ctx context.Context, fsys fs.FS, dev, drafts bool) (*site.Site, error) {
	return site.New(ctx, site.Options{
		FS:     fsys,
		Engine: brick.Options{Markdown: markdowngoldmark.New(markdowngoldmark.WithHeadingIDs())},
		Dev:    dev,
```

For the `brick` command the same switch lives in `brick.yaml`:

<!-- snippet: examples/site-blog/theme/brick.yaml -->
```yaml
markdown: {headingIDs: true}
```

## Step 2 — Write the post

`content/blog/tide-tables.md` names its own layout:

<!-- snippet: examples/site-blog/theme/content/blog/tide-tables.md -->
```markdown
---
title: Reading tide tables
date: 2026-10-01
layout: doc
---
# Reading tide tables

Two highs a day, give or take.

## Spring tides

Bigger range at full and new moon.

## Neap tides

Smaller range in between.
```

## Step 3 — Print the headings in the layout

`.Page.Headings` lists each heading with `Level` (1 to 6), `ID` (empty when the backend writes none) and `Text` (plain text). `layouts/doc.html`:

<!-- snippet: examples/site-blog/theme/layouts/doc.html -->
```html
<!doctype html>
<html>
<head><title>{{ .Title }} · {{ .Site.title }}</title></head>
<body>
<nav class="toc">{{ range .Page.Headings }}{{ if .ID }}<a class="h{{ .Level }}" href="#{{ .ID }}">{{ .Text }}</a>{{ else }}<span class="h{{ .Level }}">{{ .Text }}</span>{{ end }}
{{ end }}</nav>
<main>{{ children }}</main>
</body>
</html>
```

## Try it

```sh
cd examples/site-blog
go run .
curl -s localhost:8090/blog/tide-tables/
```

```html
<!doctype html>
<html>
<head><title>Reading tide tables · Field Notes</title></head>
<body>
<nav class="toc"><a class="h1" href="#reading-tide-tables">Reading tide tables</a>
<a class="h2" href="#spring-tides">Spring tides</a>
<a class="h2" href="#neap-tides">Neap tides</a>
</nav>
<main><h1 id="reading-tide-tables">Reading tide tables</h1>
<p>Two highs a day, give or take.</p>
<h2 id="spring-tides">Spring tides</h2>
<p>Bigger range at full and new moon.</p>
<h2 id="neap-tides">Neap tides</h2>
<p>Smaller range in between.</p>
</main>
</body>
</html>
```

`go test .` (`TestMarkdownTableOfContents`) checks this exact page.

## Pitfalls

- **No ids, no links.** Without `WithHeadingIDs()` (or `markdown: {headingIDs: true}`) the headings are still listed but every `.ID` is empty, so the layout above prints `<span class="h1">Reading tide tables</span>` and the table of contents cannot link. Ids are a slug of the text, made unique as `intro`, `intro-1`.
- **No backend, no page.** Without `brick.Options.Markdown` a `.md` file is skipped with the warning `a .md page needs brick.Options.Markdown (for example contrib/markdown-goldmark); skipped`.
- **Only markdown pages have headings.** An `.html` page's `.Page.Headings` is empty, and so is every page's when the backend does not implement the optional `brick.MarkdownHeadings` interface; the engine never guesses headings from HTML.
- **Markdown is not a template.** Components and `{{ }}` inside a `.md` body stay plain text; put dynamic parts in the layout.
- **A Go host with its own router** passes `brick.WithHeadings(hs)` to `Render`, since only the site layer collects headings for you.

## Related

- [Pages and routes](pages-and-routes.md) · [Collections and drafts](collections-and-drafts.md)
- [Feature index](../../reference/features.md) rows: markdown pages, `.Page.Headings`.
- Long-form: [guide › A table of contents for markdown pages](../../guide.md#a-table-of-contents-for-markdown-pages).
- Example: [`examples/site-blog`](../../../examples/site-blog/).
