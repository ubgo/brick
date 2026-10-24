# components-loaders

A small `net/http` host whose components fetch their own data: `load:` and `load_many:` loaders in Starlark, a `.star` module that `load()`s another module and a data file, an inline `<brick:script>` function, a host global, and a page loader that answers with a redirect, a status or a header, which the host turns into the HTTP response. `go run . -addr :8090` serves it for a browser.

```sh
go run . / /team /posts/hello /posts/old-hello /posts/gone /posts/nope      # print what the guide shows
go test .   # check the exact output (testdata/output.txt)
```

Run both from this folder. The guide that walks through it: [Scripts and loaders](../../docs/guides/components/loaders.md).
