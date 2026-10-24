# Languages: URLs, variants and translations

## What it is and when you want it

`site.Options.Locales` (or `brick serve -locales en,fr,de`) serves the whole site once per language: the first locale has no URL prefix (`/about/`), the others do (`/fr/about/`). A file named `about.fr.html` replaces `about.html` in French, `t` looks text up in `i18n/<locale>.yaml`, dates follow the language, and `.Page.Translations` lists the same page in the other languages so the layout can print `hreflang` links and a language switcher. You want it for any site with more than one audience language.

## Before you start

- Read [pages and routes](pages-and-routes.md). The `t` function and message files are covered in the guide's [translations](../../guide.md#translations) section.
- The example is [`examples/site-i18n`](../../../examples/site-i18n/).

## Step 1 — List the locales

The default language comes first. From `examples/site-i18n/main.go`:

<!-- ds:block id=locales-cwhnbuv4 title="examples/site-i18n/main.go" -->
**examples/site-i18n/main.go** · [`examples/site-i18n/main.go:28`](../../../examples/site-i18n/main.go#L28-L28)

```go
var locales = []string{"en", "fr", "de"}
```
<!-- /ds:block hash=3aafe9 -->

<!-- ds:block id=newsite-w885xjxc title="examples/site-i18n/main.go" -->
**examples/site-i18n/main.go** · [`examples/site-i18n/main.go:33-39`](../../../examples/site-i18n/main.go#L33-L39)

```go
func newSite(ctx context.Context, dir string) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{FS: root.FS(), Locales: locales, Sitemap: &site.Sitemap{}})
}
```
<!-- /ds:block hash=d66b9a -->

## Step 2 — Add the messages

`i18n/en.yaml`:

<!-- snippet: examples/site-i18n/theme/i18n/en.yaml -->
```yaml
welcome: Welcome to the workshop
languages: Languages
```

`i18n/fr.yaml`:

<!-- snippet: examples/site-i18n/theme/i18n/fr.yaml -->
```yaml
welcome: Bienvenue à l'atelier
languages: Langues
```

There is deliberately no `de.yaml`: German falls back to the default language.

## Step 3 — Link the languages in the layout

`.Page.Locale` is the language being rendered, `.Page.URL` the URL, and `.Page.Translations` the other languages' versions (each with `.Locale` and `.URL`, in `Locales` order). `layouts/default.html`:

<!-- snippet: examples/site-i18n/theme/layouts/default.html -->
```html
<!doctype html>
<html lang="{{ .Page.Locale }}">
<head>
<title>{{ .Title }}</title>
<link rel="alternate" hreflang="{{ .Page.Locale }}" href="{{ .Page.URL }}">
{{- range .Page.Translations }}
<link rel="alternate" hreflang="{{ .Locale }}" href="{{ .URL }}">
{{- end }}
</head>
<body>
<nav>{{ t "languages" }}:{{ range .Page.Translations }} <a href="{{ .URL }}">{{ .Locale }}</a>{{ end }}</nav>
<main>{{ children }}</main>
</body>
</html>
```

## Step 4 — Write shared pages and variants

A shared page renders in every language; `t` and `dateFormat` follow the locale. `content/index.html`:

<!-- snippet: examples/site-i18n/theme/content/index.html -->
```html
---
title: Atelier
---
<h1>{{ t "welcome" }}</h1>
<p>{{ dateFormat "Monday 2 January 2006" "2026-09-27" }}</p>
```

`content/about.fr.html` sits beside `content/about.html` and replaces it at `/fr/about/`:

<!-- snippet: examples/site-i18n/theme/content/about.fr.html -->
```html
---
title: À propos
---
<h1>À propos de nous</h1>
```

A page can also exist only as variants. `content/team.fr.html` moves its URL, `content/team.de.html` keeps `/de/team/`, and the two still pair up as translations because they share the stem `team`:

<!-- snippet: examples/site-i18n/theme/content/team.fr.html -->
```html
---
title: Équipe
brick: {url: /equipe/}
---
<h1>L'équipe</h1>
```

## Try it

```sh
cd examples/site-i18n
go run .
curl -s localhost:8094/fr/
```

```html
<!doctype html>
<html lang="fr">
<head>
<title>Atelier</title>
<link rel="alternate" hreflang="fr" href="/fr/">
<link rel="alternate" hreflang="en" href="/">
<link rel="alternate" hreflang="de" href="/de/">
</head>
<body>
<nav>Langues: <a href="/">en</a> <a href="/de/">de</a></nav>
<main><h1>Bienvenue à l&#39;atelier</h1>
<p>dimanche 27 septembre 2026</p>
</main>
</body>
</html>
```

`curl -s localhost:8094/de/team/` (no German messages, so the nav falls back to English):

```html
<!doctype html>
<html lang="de">
<head>
<title>Team</title>
<link rel="alternate" hreflang="de" href="/de/team/">
<link rel="alternate" hreflang="fr" href="/fr/equipe/">
</head>
<body>
<nav>Languages: <a href="/fr/equipe/">fr</a></nav>
<main><h1>Das Team</h1>
</main>
</body>
</html>
```

`brick routes -locales en,fr,de theme`:

```text
/            content/index.html
/about/      content/about.html
/de/         content/index.html
/de/about/   content/about.html
/de/team/    content/team.de.html
/fr/         content/index.html
/fr/about/   content/about.fr.html
/fr/equipe/  content/team.fr.html
```

The sitemap lists each URL with its alternates, for example:

```xml
  <url>
    <loc>https://example.com/fr/equipe/</loc>
    <xhtml:link rel="alternate" hreflang="fr" href="https://example.com/fr/equipe/"/>
    <xhtml:link rel="alternate" hreflang="de" href="https://example.com/de/team/"/>
  </url>
```

`go test .` checks the three pages above, the route list, the sitemap entry and that `/team/` is a 404.

## Pitfalls

- **The route names the file that renders.** `/fr/about/` is listed as `content/about.fr.html` (by `brick routes` and `Route.Path`), while `/de/about/` has no German variant and is listed as the shared `content/about.html`. A route's path is the file its locale actually renders.
- **Forget the locales and variants become pages.** `brick routes theme` without `-locales` lists `/equipe/` and `/team/` as ordinary pages, and every `t` warns `translation "welcome" is missing for every locale: no locale is set (Options.DefaultLocale, brick.WithLocale, or {{ provide "locale" … }})`. Always pass the same `-locales` to `check`, `routes`, `build` and `serve`.
- **A missing message falls back, with a warning.** `brick check -locales en,fr,de` reports `translation "languages" is missing for de`; the page shows the default language's text, and a key missing everywhere shows as the key itself, never as a blank.
- **A variant-only page has no default-language URL.** `/team/` is a 404 here, and `.Page.Translations` lists only the languages that exist; a page in one language only has an empty list.
- **A dynamic page's other-language URLs carry the same values** and are not checked against that locale's `paths:` list.
- **Under a `BasePath`** translation URLs are rebased like every other root URL (`/docs/fr/about/`).

## Related

- [Feeds and sitemap](feeds-and-sitemap.md) · [Hosting under a path](subpath-and-mount.md) · [Pages and routes](pages-and-routes.md)
- [Feature index](../../reference/features.md) rows: language variants (`about.fr.html`) and `.Page.Translations`, per-language URLs (`Locales`), translations (`t`).
- Long-form: [guide › A whole site](../../guide.md#a-whole-site--urls-serving-building) and [guide › Translations](../../guide.md#translations).
- Example: [`examples/site-i18n`](../../../examples/site-i18n/).
