# Action middleware, rate limits and the error envelope

## What it is and when you want it

Every action a browser sends (a click, a submit, a `$set`) runs through one pipeline you can wrap, the same shape as `net/http` middleware. Use it for the checks every action needs and no template should repeat: "is anyone signed in", "is this user allowed to do that", "too many clicks, slow down", a log line or a trace span per action, a timeout. The same page covers the request values an action's re-render sees (`Options.ActionRequest`) and the shape of the error the browser gets back (`Options.ActionErrors`).

## Before you start

- A stateful component with an action ([Stateful components](../interactivity/stateful-components.md)).
- Sessions wired up if you want per-user limits or `RequireSession` ([Sessions and sign-out](sessions.md)).
- The example this page builds is [examples/production-middleware](../../../examples/production-middleware/). Its "auth" is two request headers, `X-User` and `X-Role`, so it stays small; a real app reads its session.

## Step 1 — The component

Two actions, one of which only admins may call. The template injects the signed-in user, falling back to `guest`:

<!-- snippet: examples/production-middleware/theme/components/Likes.html -->
```html
---
state:
  likes: 0
inject:
  user: {default: "guest"}
---
<brick:script>
def like(state, props):
    return dict(state, likes = state["likes"] + 1)

def wipe(state, props):
    return dict(state, likes = 0)
</brick:script>
<div class="likes">
  <p>{{ .State.likes }} likes, seen by {{ .Ctx.user }}</p>
  <button brick:click="like">Like</button>
  <button brick:click="wipe">Wipe (admins)</button>
</div>
```

## Step 2 — Build the chain

`Options.ActionMiddleware` lists middleware outermost first. Package `actionmw` has the common ones, all standard library: `Recover` (a panic becomes an error), `Slog` (one log line per action, refusals included), `RequireSession` (no session, `403`), `RateLimit` (a `Limiter` and a key function, `429` with `Retry-After`) and `Timeout` (`504` past a deadline).

<!-- snippet: examples/production-middleware/main.go lines=53-91 -->
```go
// newApp builds the engine. logOut receives one log line per action.
func newApp(dir string, logOut io.Writer) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{ReplaceAttr: dropTime}))
	return brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		Sessions:  brick.SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get(userHeader), nil }),
		// Outermost first: Recover sees every panic, Slog logs every
		// outcome (refusals included), RequireSession turns guests away
		// before they get a rate-limit bucket, and the app's own rule runs
		// last, so a refused wipe still spends a token (no free probing).
		ActionMiddleware: []brick.ActionMiddleware{
			actionmw.Recover(),
			actionmw.Slog(logger),
			actionmw.RequireSession(),
			actionmw.RateLimit(actionmw.MemoryLimiter(actionsPerMinute, time.Minute), actionmw.BySession),
			actionmw.Timeout(actionTimeout),
			adminOnly("wipe"),
		},
		// Request values reach the re-render; they are never stored with
		// the instance, so a role change shows on the next click.
		ActionRequest: func(r *http.Request) []brick.RenderOption {
			if u := r.Header.Get(userHeader); u != "" {
				return []brick.RenderOption{brick.Provide("user", u)}
			}
			return nil
		},
		ActionErrors: writeError,
	})
}
```

The engine resolves the store, loads the instance and checks its token and session *before* the chain runs, so a middleware only ever sees a real, authorised component. `actionmw.BySession` keys the limit by the hashed session and falls back to `ByIP` for guests.

## Step 3 — Write your own

A middleware is a function from the next step to a new step. Refuse by returning an error without calling `next`: `brick.ErrForbidden` is `403`, `brick.ErrGone` is `410`, a `*brick.ActionError` carries its own status and a message the client may see, anything else is `422`.

<!-- snippet: examples/production-middleware/main.go lines=50-51 -->
```go
// errAdminOnly is the app's refusal; wrapping ErrForbidden makes it a 403.
var errAdminOnly = &brick.ActionError{Status: http.StatusForbidden, Message: "only admins may wipe", Err: brick.ErrForbidden}
```

<!-- snippet: examples/production-middleware/main.go lines=93-105 -->
```go
// adminOnly refuses the named action unless the caller is an admin. It
// runs after the engine has loaded and checked the instance, so it only
// ever sees real components.
func adminOnly(action string) brick.ActionMiddleware {
	return func(next brick.ActionFunc) brick.ActionFunc {
		return func(ctx context.Context, a *brick.ActionCall) (*brick.ActionResult, error) {
			if a.Action == action && (a.Request == nil || a.Request.Header.Get(roleHeader) != adminRole) {
				return nil, errAdminOnly
			}
			return next(ctx, a)
		}
	}
}
```

`ActionCall` also carries the component's tag and file, the store, the arguments, a copy of the state before the action, and the deferred field values (`Sets`), which a middleware may drop or rewrite. After `next` returns, it may edit the result's `Effects` or `Reply`.

## Step 4 — Give the re-render today's request values

State is stored; request values are not. `Options.ActionRequest` returns render options for each action's re-render, so the component sees the user signed in *now*, not the one who loaded the page. It runs before the re-render and is never stored with the instance, so a role change shows on the very next click:

<!-- snippet: examples/production-middleware/main.go lines=81-88 -->
```go
		// Request values reach the re-render; they are never stored with
		// the instance, so a role change shows on the next click.
		ActionRequest: func(r *http.Request) []brick.RenderOption {
			if u := r.Header.Get(userHeader); u != "" {
				return []brick.RenderOption{brick.Provide("user", u)}
			}
			return nil
		},
```

## Step 5 — Answer errors in your own envelope

By default an action error is a plain-text body with brick's status (`ActionStatus` documents the mapping). `Options.ActionErrors` replaces the whole answer for a host whose gateway or client expects its own shape. Keep the status from `brick.ActionStatus(err)` and show only an `ActionError`'s `Message`; other errors may carry a file path or a store's detail:

<!-- snippet: examples/production-middleware/main.go lines=126-147 -->
```go
// writeError is Options.ActionErrors. It keeps brick's status mapping
// (ActionStatus) and the Retry-After of a rate limit, and changes only
// the body. Only an ActionError's Message reaches the client: any other
// error may carry a file path or a store's detail, so it answers with the
// status text.
func writeError(w http.ResponseWriter, _ *http.Request, err error) {
	var body errorBody
	body.Error.Status = brick.ActionStatus(err)
	body.Error.Message = http.StatusText(body.Error.Status)
	var ae *brick.ActionError
	if errors.As(err, &ae) {
		body.Error.Message = ae.Message
		if ae.RetryAfter > 0 {
			// Rounded up, as brick's own envelope does: never invite a retry
			// before the limiter allows it.
			w.Header().Set("Retry-After", strconv.Itoa(int((ae.RetryAfter+time.Second-1)/time.Second)))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(body.Error.Status)
	_ = json.NewEncoder(w).Encode(body)
}
```

`Options.MaxActionBytes` bounds an action request's JSON (and a form post's body); over it, the request is refused with `400 bad action request` before any middleware runs.

## Rate limits across servers

`actionmw.MemoryLimiter` is a token bucket per key in this process, so with three servers a user gets three budgets. [contrib/ratelimit-redis](../../../contrib/ratelimit-redis/) is the same `Limiter` over a shared Redis; a Redis outage refuses the action with `503` instead of letting it through:

<!-- snippet: contrib/ratelimit-redis/example_test.go lines=27-30 -->
```go
	limiter := ratelimitredis.New(rdb, actionsPerMinute, time.Minute, ratelimitredis.Options{Prefix: "shop:"})
	opts := brick.Options{
		ActionMiddleware: []brick.ActionMiddleware{actionmw.RateLimit(limiter, actionmw.BySession)},
	}
```

## Traces and metrics

[contrib/actions-otel](../../../contrib/actions-otel/) is one middleware that turns every action into an OpenTelemetry span named `<Component>.<action>` with `brick.component`, `brick.component.path`, `brick.action`, `brick.store` and `brick.action.status` attributes, plus `brick.action.duration` and `brick.action.count` metrics. You pass your own providers; it never touches OpenTelemetry's globals:

<!-- snippet: contrib/actions-otel/example_test.go lines=34-46 -->
```go
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	eng, err := brick.New(brick.Options{
		FS:     counterTheme,
		States: brick.NewMemoryStates(0, 0),
		ActionMiddleware: []brick.ActionMiddleware{
			actionmw.Recover(),
			otelactions.Middleware(tp, otelactions.WithMeterProvider(mp)),
		},
	})
```

## Try it

From `examples/`:

```console
$ go run ./production-middleware
2026/10/14 10:07:24 serving on :8085
```

In another terminal: a like, a wipe as a user, a wipe as an admin, and a fourth action past the budget of three a minute.

```sh
curl -s -H 'X-User: sara' http://localhost:8085/ > page.html
handle=$(grep -o 'data-brick-state="[^"]*"' page.html | cut -d'"' -f2)
token=$(grep -o 'data-brick-token="[^"]*"' page.html | cut -d'"' -f2)
act() { curl -s -i -H 'Content-Type: application/json' -H 'Brick-Action: 1' "$@" -d "{\"handle\":\"$handle\",\"token\":\"$token\",\"action\":\"$action\"}" http://localhost:8085/_brick/action | grep -E '^HTTP|^Retry-After|^\{|<p>'; }
action=like act -H 'X-User: sara'
action=wipe act -H 'X-User: sara'
action=wipe act -H 'X-User: sara' -H 'X-Role: admin'
action=like act -H 'X-User: sara'
```

```console
HTTP/1.1 200 OK
  <p>1 likes, seen by sara</p>
HTTP/1.1 403 Forbidden
{"error":{"status":403,"message":"only admins may wipe"}}
HTTP/1.1 200 OK
  <p>0 likes, seen by sara</p>
HTTP/1.1 429 Too Many Requests
Retry-After: 20
{"error":{"status":429,"message":"too many actions; slow down"}}
```

The server's log has one line per action from `actionmw.Slog`:

```console
level=INFO msg="brick action" component=Likes action=like store=default took=434.917µs version=1
level=WARN msg="brick action failed" component=Likes action=wipe store=default took=12.708µs err="only admins may wipe"
level=INFO msg="brick action" component=Likes action=wipe store=default took=567.708µs version=2
level=WARN msg="brick action failed" component=Likes action=like store=default took=1.709µs err="too many actions; slow down"
```

`go test -v ./production-middleware/` runs the same sequence, plus a guest whose action `RequireSession` refuses with `{"error":{"status":403,"message":"Forbidden"}}`.

## Pitfalls

- **Order is policy.** The list runs outermost first. Put `Recover` first so it sees every panic and `Slog` second so refusals are logged. Anything after `RateLimit` spends a token even when it refuses: in the example a refused `wipe` counts against the budget, which stops free probing; move your check above `RateLimit` if refusals should be free.
- **Only `ActionError.Message` is safe to show.** A plain error from a script or a store can name a file or a query. The default envelope already hides it (a `422` reports the detail to your `ErrorHook`s and answers `the action failed (error <id>)` unless `Options.Mode` is `Dev`); a custom `ActionErrors` must do the same, as `writeError` does with `http.StatusText`.
- **`a.Request` is nil outside HTTP.** `Engine.RunAction` and `bricktest` call the pipeline with no request. A middleware that reads headers must treat a nil request as "not allowed", as `adminOnly` does, or `RunAction` panics in your tests.
- **`Provide` an empty value only when you mean it.** A provided value counts even when empty and hides the component's `inject:` default; the example provides `user` only when there is one, so a guest sees `guest`.
- **`ActionRequest` is not middleware.** It cannot refuse; it only adds render options. Refusals belong in `ActionMiddleware`.
- **Retry-After rounds up.** brick's own envelope rounds a limiter's wait up to whole seconds, so a client never retries early; a custom envelope should too.

## Related

- [Sessions and sign-out](sessions.md) · [The Go host API](host-api.md) · [bricktest](../tooling/bricktest.md)
- [Feature index: State storage and security](../../reference/features.md#state-storage-and-security)
- Example: [examples/production-middleware](../../../examples/production-middleware/) · contrib: [ratelimit-redis](../../../contrib/ratelimit-redis/), [actions-otel](../../../contrib/actions-otel/)
