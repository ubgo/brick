# Testing a theme with bricktest

A validated sign-up form, an avatar upload and a price whose loader adds a value the template never prints. `main_test.go` drives them with `bricktest.Live`: `Submit` with rule errors and a flash effect (`AssertErrors`, `AssertSet`, `AssertFlash`), `Upload` into `bricktest.MemoryUploads` and a refused type through `TryUpload`, the view log (`AssertViewHas`, `Views`), and a golden file (`bricktest.Golden`, rewritten with `-update`).

Run it from `examples/`:

```sh
go run ./tooling-bricktest
```

Then open http://localhost:8087/.

Test it:

```sh
go test ./tooling-bricktest/
```

Guide: [bricktest](../../docs/guides/tooling/bricktest.md).

## What the tests prove

- TestSignup: a form submit runs the action the way the runtime does.
- TestAvatarUpload: an upload goes through the whole pipeline, and a refusal is returned, not fatal.
- TestPriceLoader: the view log shows what a template saw.
- TestPriceGolden: one component compared with a golden file (`go test -update` rewrites it).

A passing run from `examples/`:

```text
$ go test ./tooling-bricktest/
ok  	github.com/ubgo/brick/examples/tooling-bricktest	0.024s
```
