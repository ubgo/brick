# brick/contrib/actions-otel

A `brick.ActionMiddleware` that traces and measures every action with OpenTelemetry: one span per action, a duration histogram and a count by outcome. Uploads run through the chain as the `$upload` action, so they get spans and metrics too.

## Use it

```go
eng, err := brick.New(brick.Options{
    ActionMiddleware: []brick.ActionMiddleware{
        actionmw.Recover(),
        otelactions.Middleware(tracerProvider, otelactions.WithMeterProvider(meterProvider)),
    },
    /* … */
})
```

Runnable version: `Example` runs a real `$set` action through an engine with the SDK's in-memory span recorder and a manual metric reader, and prints the span (`Counter.$set` with its attributes) and the two metric names. Run it with `go test -run Example .` in this folder ([example_test.go](example_test.go)).

Guide: [action-middleware.md](../../docs/guides/production/action-middleware.md).

The host passes its own providers; this module never reads or sets OpenTelemetry's globals. A nil `TracerProvider` records metrics only, and without `WithMeterProvider` no metrics are recorded.

**Spans** are named `<Component>.<action>` (`Counter.save`, `Uploader.$upload`). Under a request span (for example from `otelhttp`) the action span is an internal child; with no span in the context it is a server span. A failed action records the error and sets the span status to Error.

| Attribute | Example | Where |
|---|---|---|
| `brick.component` | `Counter` | span, metrics |
| `brick.component.path` | `components/Counter.html` | span |
| `brick.action` | `save`, `$set`, `$upload` | span, metrics |
| `brick.store` | `default` | span, metrics |
| `brick.action.status` | `200`, `403`, `410`, `422`, `429`, `503` | span, metrics |
| `brick.action.renderless` | `true` (called `brick.skip_render`) | span |
| `brick.action.stream.chunks` | `12` (streamed output) | span |

**Metrics:** `brick.action.duration` (histogram, seconds) and `brick.action.count` (counter), both by component, action, store and status.

**Never recorded:** state, arguments, deferred field values, streamed text, handles, tokens and session ids. They are user data or secrets, and a tracing backend is usually readable by more people than the database. A test fails if any of them reaches a span or a metric.

**Statuses** follow brick's default error envelope (`otelactions.Status`): `*brick.ActionError` → its status, `ErrStoreUnavailable` → 503, `ErrGone` → 410, `ErrForbidden` → 403, `ErrUnknownAction` → 400, upload limits → 413 / 415, anything else → 422, a client that went away → 499. A host with its own `Options.ActionErrors` passes `WithStatus`. An action name the engine refused is recorded as `_unknown`, so client-invented names never become new span names or metric series.

**Placement:** innermost, spans cover only the actions the other middleware let through; outermost, refusals (429, 403) are measured too.

## Tests

`go test ./...` uses the SDK's in-memory span recorder and a manual metric reader; it needs no network.
