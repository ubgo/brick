# components-slots

Pages that pass markup into components: `{{ children }}`, named blocks (`<brick:prop>`, `node`, `hasNode`), a scoped slot (`.Slot`), declared slots (`brick: {slots:}`), child tags read as data (`.Children`, `child`) and raw bodies (`raw: true`, `src=`). The tests also pin the declared-slot errors and the `.Children` trap.

```sh
go run .      # print what the guide shows
go test .   # check the exact output (testdata/output.txt)
```

Run both from this folder. The guide that walks through it: [Children, blocks and slots](../../docs/guides/components/slots.md).

`go run .` prints each page's markup, with `== content/index.html` first and `== content/tabs.html` second. A passing `go test .` prints `ok  	github.com/ubgo/brick/examples/components-slots` followed by the time. Every page here is a few lines of HTML, so read `theme/content/` next to `testdata/output.txt` to see exactly which markup each line produces.
