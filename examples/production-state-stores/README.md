# Memory, named and custom state stores

Two stores in one engine: a search box in `NewMemoryStates` (the default store) and a cart in a named store, `cart`, picked with `brick: {store: cart}` and backed by a `BytesStore` the host writes (a map here, wrapped with `brick.Bytes`). The store implements the optional `Revoker` upgrade, so `Engine.RevokeSession` on sign-out removes the cart rows. The tests run `statetest.Store` on the host's store, the same conformance suite brick's own stores pass.

Run it from `examples/`:

```sh
go run ./production-state-stores
```

Then open http://localhost:8088/?user=ada.

Test it:

```sh
go test ./production-state-stores/
```

Guide: [State stores](../../docs/guides/production/state-stores.md), [statetest](../../docs/guides/tooling/statetest.md).

## What the tests prove

- TestNamedStore: each component's handle names its store, the cart lands in the host's `BytesStore`, and sign-out removes it.
- TestMapStoreConforms: the host's store passes the same conformance suite as brick's own.
- TestUnknownStore: an unknown store name is a compile error listing the stores that exist.

A passing run from `examples/`:

```text
$ go test ./production-state-stores/
ok  	github.com/ubgo/brick/examples/production-state-stores	0.023s
```
