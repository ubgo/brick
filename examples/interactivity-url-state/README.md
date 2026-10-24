# interactivity-url-state

A `Catalog` whose search, tab and page live in the address bar (`query:`), with a `params:` hook that clamps the page, a `remember:` field kept per session, and a `temporary:` field that is never saved; a `RowEditor` with a `locked:` field and a `Profile` closed to the client except its `writable:` field. The test posts to the real action endpoint and shows the 422 a refused write gets.

The guide that walks through it: [State in the URL, the session and the server](../../docs/guides/interactivity/url-and-server-state.md).

## Run it

```sh
cd examples/interactivity-url-state
go run .
```

Then open http://localhost:8091/?search=Acme&page=9. `-addr` changes the listen address and `-theme` the project directory.

## Test it

```sh
cd examples
go test -v ./interactivity-url-state/
```

```text
--- PASS: TestURLState (0.01s)
--- PASS: TestRememberAndTemporary (0.00s)
--- PASS: TestLockedAndWritable (0.00s)
ok  	github.com/ubgo/brick/examples/interactivity-url-state	0.037s
```
