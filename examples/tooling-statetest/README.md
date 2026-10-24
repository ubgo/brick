# Proving a custom state store with statetest

A whole `brick.StateStore` (not a `BytesStore`) that keeps codec bytes under random handles and adds the optional `Revoker` and `Lister` upgrades. `main_test.go` runs `statetest.Store` against it (all eight cases run, none skip), `statetest.Codec` on the JSON codec, and wires the store into an engine by name.

Run it from `examples/`:

```sh
go run ./tooling-statetest
```

Test it:

```sh
go test ./tooling-statetest/
```

Guide: [statetest](../../docs/guides/tooling/statetest.md).

## What the tests prove

- TestMemStoreConforms: the whole `statetest` suite; the store also implements `Revoker` and `Lister`, so those cases run instead of skipping.
- TestCodecConforms: the same proof for a codec.
- TestWiresIntoEngine: the store works in an engine under a name.

A passing run from `examples/`:

```text
$ go test ./tooling-statetest/
ok  	github.com/ubgo/brick/examples/tooling-statetest	0.015s
```
