# interactivity-counter

A stateful `Counter` with actions that take arguments, the built-ins `$set`, `$toggle` and `$reset`, an `init:` hook that seeds the count from a prop, an `updated:` hook that trims a typed label, and a renderless `peek` action that answers with `brick.reply`. A second page nests a Modelable `Stepper` inside a `Cart` with `brick:model`. The engine is used alone, so the action endpoint and the asset handler are mounted by hand.

The guide that walks through it: [Stateful components and actions](../../docs/guides/interactivity/stateful-components.md).

## Run it

```sh
cd examples/interactivity-counter
go run .
```

Then open http://localhost:8090. `-addr` changes the listen address and `-theme` the project directory.

## Test it

```sh
cd examples
go test -v ./interactivity-counter/
```

```text
--- PASS: TestCounterOverHTTP (0.00s)
--- PASS: TestCounterLive (0.00s)
--- PASS: TestCartModelable (0.00s)
--- PASS: TestCounterLinksRuntime (0.00s)
ok  	github.com/ubgo/brick/examples/interactivity-counter	0.030s
```

`TestCounterOverHTTP` posts one action to `/_brick/action` exactly as the runtime does; `TestCounterLive` drives clicks, a typed field and a renderless `peek` with `bricktest.Live`; `TestCartModelable` checks the Stepper inside the Cart.
