# interactivity-nested

A `ContactsApp` holding one stateful `ContactRow` per contact. Each row keeps its own open or closed state when the parent acts, follows the parent's data through a `reactive: true` prop, and is keyed with `:key=".id"` so state follows the contact and not its position. The host side shows `Engine.Instance` (a read-only look at a live instance, served at `/inspect?handle=…`) and an `InstanceEndHook` registered in `Options.Hooks` that hears an instance end when its session is revoked.

The guide that walks through it: [Stateful components and actions](../../docs/guides/interactivity/stateful-components.md), Steps 6 and 7.

## Run it

```sh
cd examples/interactivity-nested
go run .
```

Then open http://localhost:8096. Open a row, then copy its `data-brick-state` value from the page source into `http://localhost:8096/inspect?handle=default.…` to see its state as JSON. `-addr` changes the listen address and `-theme` the project directory.

## Test it

```sh
cd examples
go test -v ./interactivity-nested/
```

```text
--- PASS: TestRowsKeepStateAndFollowReactiveProps (0.01s)
--- PASS: TestMissingKeyWarns (0.00s)
--- PASS: TestInspectRoute (0.00s)
--- PASS: TestEndHookHearsRevoke (0.00s)
ok  	github.com/ubgo/brick/examples/interactivity-nested	0.046s
```
