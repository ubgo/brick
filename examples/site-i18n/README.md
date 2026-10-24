# site-i18n

One site in three languages (`en`, `fr`, `de`). The default language has no URL prefix, the others do; `about.fr.html` replaces `about.html` in French; `team.fr.html` and `team.de.html` exist only as variants and still link to each other; `t` reads `i18n/<locale>.yaml`; and the layout prints `hreflang` links and a language switcher from `.Page.Translations`. The sitemap lists every alternate.

```sh
go run .                                   # http://localhost:8094/fr/
brick serve -locales en,fr,de theme        # the same with the brick command
go test .                                  # the pages, the routes, the sitemap entry
```

Guide: [languages: URLs, variants and translations](../../docs/guides/site/languages.md).

## What the tests prove

- TestLanguages: every language's URLs, the `about.fr.html` variant, `t` lookups, `hreflang` links and `Routes()` per locale.

A passing run from `examples/`:

```text
$ go test ./site-i18n/
ok  	github.com/ubgo/brick/examples/site-i18n	0.026s
```
