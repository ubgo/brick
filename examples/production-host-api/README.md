# The engine from Go

The engine with no site layer: `brick.New`, `Engine.Render` for a page, `Engine.RenderComponent` for one component (`/preview/Greeting?name=Ada`), `RenderChunks` + `Fill` for a page rendered once with a per-request island, `ActionHandler` mounted beside the host's routes, and `RunAction` + `Instance` for code that calls an action without HTTP. `/stats` returns what a request rendered (`WithStats`, with `Team` as an island that renders components of its own). The tests also pin determinism (32 concurrent renders give the same bytes, and a pinned `Rand` and `Clock` fix the action token), the render limits (`MaxComponents`, `MaxOutputBytes`, `MaxDepth`), the action size cap and error envelope (`MaxActionBytes`, `ActionErrors`), `Engine.Instance`, and the script step and time limits.

Run it from `examples/`:

```sh
go run ./production-host-api   # or: go run ./production-host-api -render static
```

Then open http://localhost:8086/?user=Ada.

Test it:

```sh
go test ./production-host-api/
```

```text
ok  	github.com/ubgo/brick/examples/production-host-api	0.036s
```

Guide: [The Go host API](../../docs/guides/production/host-api.md), [Limits and determinism](../../docs/guides/production/limits-determinism.md).
