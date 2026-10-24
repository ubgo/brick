# components-errors

Components chosen from data with `<brick:render>`, a `<brick:boundary>` that turns a failing loader into its fallback, the same page in Prod and Dev mode, an `OnError` hook that still hears the contained error, and `Engine.CompileAll` reporting a misspelled tag and an unclosed element with file and line.

```sh
go run .      # print what the guide shows
go test .   # check the exact output (testdata/output.txt)
```

Run both from this folder. The guide that walks through it: [When things go wrong](../../docs/guides/components/errors.md).

`go run .` prints the page in Prod and then Dev mode, each followed by what the `OnError` hook heard, then the check report; it starts with `== content/index.html in Prod mode` and ends with `broken/unbalanced.html:4:1: warning: </main> closes <div> (line 2), which is still open`. A passing `go test .` prints `ok  	github.com/ubgo/brick/examples/components-errors` followed by the time. The warning lines of the unbalanced-HTML check and the Dev-only `data-brick-loc` attribute are both in `testdata/output.txt`. The guide: [When things go wrong](../../docs/guides/components/errors.md).
