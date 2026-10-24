# components-layouts-data

A layout chain (`post` inside `default`), `.Ctx` through `provide:` and `inject:`, site config and data files loaded with `LoadSite` (with `${BASE_URL}` from an explicit environment lookup), a data file imported with `use:`, and translations with plurals, a localized date and a language variant (`about.fr.html`).

```sh
go run .      # print what the guide shows
go test .   # check the exact output (testdata/output.txt)
```

Run both from this folder. The guide that walks through it: [Layouts, context, site data and translations](../../docs/guides/components/layouts-and-data.md).

`go run .` prints the post page wrapped in both layouts (it starts with `== content/index.html` and `<!doctype html>`), then the same page as a fragment, a blog post in its layout chain, a plans page, the cart in English and in French (plurals), and the about page with its French variant. A passing `go test .` prints `ok  	github.com/ubgo/brick/examples/components-layouts-data` followed by the time. `TestPitfalls` and `TestChainErrors` check the traps the guide lists, so they are tested, not only described. The `${BASE_URL}` placeholder in `site.yaml` is filled from the lookup function in `main.go`, never from the process environment.
