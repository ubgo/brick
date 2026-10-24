# components-frontmatter

Generates [docs/reference/frontmatter.md](../../docs/reference/frontmatter.md), the table of every frontmatter key and `brick:` setting, from `brick.FrontmatterSchema()` (the JSON Schema `brick schema` prints). Where a key applies, its default and its guide link live in this program; the program refuses to run when the schema has a key it does not describe, or describes a key the schema no longer has.

```sh
go run . > ../../docs/reference/frontmatter.md   # regenerate the page
go test .                                       # fail if the committed page is stale
```

The generated page has one row per key, so a new frontmatter key without a note makes `go test .` fail with a message naming the key. A passing `go test .` prints `ok  	github.com/ubgo/brick/examples/components-frontmatter` followed by the time. Run from this folder, `go run .` prints the page to the terminal; redirect it as above to update the file. The guides the rows link to are in [docs/guides/components](../../docs/guides/components/), and `syntax`, `unknown` and `passthrough` point at [Components and props](../../docs/guides/components/props.md#step-6--choose-how-a-files-tags-are-read).
