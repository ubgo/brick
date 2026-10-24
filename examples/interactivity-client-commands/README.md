# interactivity-client-commands

A `Menu` that opens and closes with `brick:js` client commands and no server, and a stateful `Todos` list with `brick:key` rows, `brick:ignore` and `brick:replace` elements, a `brick:transition` root, actions that choose their own transition with `brick.transition`, and `brick:enter` / `brick:leave` on a `brick:show` panel. The Menu also has a `<brick:client>` block that calls `brick.js(menu, menu.getAttribute("data-on-close"))` when the page navigates. The test checks the server side, that the client module is served, and pins the compile errors.

The guide that walks through it: [Morph controls, transitions and client commands](../../docs/guides/interactivity/morph-transitions-client-commands.md).

## Run it

```sh
cd examples/interactivity-client-commands
go run .
```

Then open http://localhost:8095. `-addr` changes the listen address and `-theme` the project directory. The menu opens and closes with no request; only a browser shows that.

## Test it

```sh
cd examples
go test -v ./interactivity-client-commands/
```

```text
--- PASS: TestTodos (0.02s)
--- PASS: TestRefused (0.00s)
--- PASS: TestMenuClientServed (0.01s)
ok  	github.com/ubgo/brick/examples/interactivity-client-commands	0.102s
```
