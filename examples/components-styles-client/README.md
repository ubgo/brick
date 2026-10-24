# components-styles-client

A page whose components carry their own CSS and browser JavaScript: scoped and inclusive `<brick:style>`, a sibling `Badge.css`, a host `StyleTransform` (a minifier), and `<brick:client>` modules mounted with `brick.mount`, one importing another through `brick:Modal`. The program prints the page and every asset file `{{ assets }}` links.

```sh
go run .      # print what the guide shows
go test .   # check the exact output (testdata/output.txt)
```

Run both from this folder. The guide that walks through it: [Styles and client code](../../docs/guides/components/styles-and-client.md).

`go run .` prints the page (the scoped `data-brick-s` attributes) and then each asset file, headed `== c/Badge.556dd832.css` and so on (the runtime is listed but not shown), so you see the CSS and JavaScript the browser would fetch. A passing `go test .` prints `ok  	github.com/ubgo/brick/examples/components-styles-client` followed by the time. The asset file names carry a content hash, so they change only when a stylesheet or module changes; the test compares the whole output including the hashes.
