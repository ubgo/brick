# Action middleware, rate limits and the error envelope

Every action runs through a chain: `actionmw.Recover`, `actionmw.Slog`, `actionmw.RequireSession`, `actionmw.RateLimit` (three actions a minute per session), `actionmw.Timeout`, and an app rule that only admins may call `wipe`. `Options.ActionRequest` gives each re-render the user signed in now, and `Options.ActionErrors` answers refusals in the app's own JSON envelope. The stand-in auth is two headers, `X-User` and `X-Role`.

Run it from `examples/`:

```sh
go run ./production-middleware
```

Then open http://localhost:8085/ (send `X-User: sara` to sign in).

Test it:

```sh
go test ./production-middleware/
```

Guide: [Action middleware](../../docs/guides/production/action-middleware.md).

## What the tests prove

- TestChain: a signed-in like runs and the re-render sees the user, the app's own middleware refuses `wipe` for a non-admin with its 403 envelope, the same session passes once it is an admin, and the fourth action is a 429 with `Retry-After`.
- TestGuest: an action with no session is refused with 403 `Forbidden` before the limiter spends a token.

A passing run from `examples/`:

```text
$ go test ./production-middleware/
ok  	github.com/ubgo/brick/examples/production-middleware	0.023s
```
