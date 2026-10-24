# Collections and drafts

## What it is and when you want it

A section of the site (every page under `content/blog/`) can declare the frontmatter its pages must have, in `_collection.yaml`; `brick check` and `Site.Diagnostics()` then name any post that breaks it. A page with `draft: true` exists only while you write: the dev server shows it, a normal build leaves it out. You want both once more than one person, or one tired person, adds posts.

## Before you start

- Read [pages and routes](pages-and-routes.md); this page uses [`examples/site-blog`](../../../examples/site-blog/).
- The schema uses the same grammar as a component's `props:` block: `string`, `int`, `float`, `bool`, `list`, `map`, `any`, with `required`, `default` and `oneof`.

## Step 1 — Declare the collection

`content/blog/_collection.yaml` lists the fields under `fields:`. There is no date type, so a date is `any` (YAML already reads `2026-10-01` as a date).

<!-- snippet: examples/site-blog/theme/content/blog/_collection.yaml -->
```yaml
fields:
  title: {type: string, required: true}
  date: {type: any, required: true}
```

The section's own `index.html` lists the collection and is not checked against it.

## Step 2 — Mark a post as a draft

`content/blog/half-written.html`:

<!-- snippet: examples/site-blog/theme/content/blog/half-written.html -->
```html
---
title: Half written
date: 2026-10-10
draft: true
---
<h1>Half written</h1>
```

## Step 3 — Choose when drafts show

`site.Options.Drafts` decides. The example passes its `-drafts` flag straight through:

<!-- snippet: examples/site-blog/main.go lines=42-47 -->
```go
// ds:def id=newsitefs-5nq2ppfu
func newSiteFS(ctx context.Context, fsys fs.FS, dev, drafts bool) (*site.Site, error) {
	return site.New(ctx, site.Options{
		FS:     fsys,
		Engine: brick.Options{Markdown: markdowngoldmark.New(markdowngoldmark.WithHeadingIDs())},
		Dev:    dev,
```

With the CLI, `brick serve` always shows drafts, and `check`, `routes` and `build` show them only with `-drafts`.

## Try it

A post without a title, `content/blog/untitled.html`:

```html
---
date: 2026-10-12
---
<p>No title.</p>
```

```sh
brick check theme
```

```text
content/blog/untitled.html:1: error: frontmatter breaks the blog collection: title is required
```

The exit code is 1. From Go, `s.Diagnostics()` returns the same diagnostic (`File: content/blog/untitled.html`, code `collection`), and the site still builds and serves; `TestCollectionSchema` checks this.

Drafts, without and with `-drafts`:

```sh
brick routes -drafts theme
```

```text
/                    content/index.html
/about/              content/about.html
/blog/               content/blog/index.html
/blog/first-light/   content/blog/first-light.html
/blog/half-written/  content/blog/half-written.html
/blog/tide-tables/   content/blog/tide-tables.md
/terms/              content/legal.html
```

Without `-drafts` the `/blog/half-written/` line is gone, the URL answers 404, and the blog index does not list it. With drafts on, the index gains the line `<li><a class="post" href="/blog/half-written/">Half written</a> · 10 Oct 2026</li>`, first because it is the newest. `go test .` (`TestDrafts`) checks all three.

## Pitfalls

- **A schema error does not stop the server.** The site is built and served; only `brick check` exits 1. Run `brick check` in CI.
- **A broken `_collection.yaml` is itself a diagnostic** on that file (code `collection`), and that section is then not checked.
- **`brick serve` always shows drafts.** What you see in the dev server is not what `brick build` writes; run `brick routes` without `-drafts` to see the published set.
- **Undeclared keys are allowed.** The schema checks the fields it names, the same rule as undeclared props, so a typo in a key name (`tilte:`) is reported as the missing `title`, not as an unknown key.

## Related

- [Pages and routes](pages-and-routes.md) · [Static export](static-export.md) · [Dev server](dev-server.md)
- [Feature index](../../reference/features.md) rows: content collections (required fields, `Schema`), drafts, `brick check`.
- Long-form: [guide › A whole site](../../guide.md#a-whole-site--urls-serving-building).
- Example: [`examples/site-blog`](../../../examples/site-blog/).
