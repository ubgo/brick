# components-options

The engine settings that decide how a theme's components are found and written, each with the page that uses it: two component roots (`Options.Roots`), per-file tag spelling (`syntax:`), tags left as plain HTML (`passthrough:` and `unknown:`), a custom `.kv` data file imported with `use:` through `Options.DataFormats`, `dedent:`, `tag` and `endtag`, component docs (`doc:`, `examples:`, `Engine.ComponentDocs`), the Starlark step limit, the html-balance warning, `data-brick-loc`, a compile and an after-render hook, `Engine.InheritAssets` and `WithStats`.

```sh
go run .      # print every section the guides show
go test .     # check the exact output (testdata/output.txt) and the traps
```

Run both from this folder. A passing run ends with `ok  	github.com/ubgo/brick/examples/components-options`. Guides that teach it: [Components and props](../../docs/guides/components/props.md), [Layouts, context and data](../../docs/guides/components/layouts-and-data.md), [Children, blocks and slots](../../docs/guides/components/slots.md), [Scripts and loaders](../../docs/guides/components/loaders.md), [When things go wrong](../../docs/guides/components/errors.md) and [Styles and client code](../../docs/guides/components/styles-and-client.md).
