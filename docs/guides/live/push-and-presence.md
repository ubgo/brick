# Push and presence — live updates from other people

## What it is and when you want it

Two people look at the same order list. One ships an order; the other's list still says "pending", because an action re-renders only the component that ran it, in the tab that ran it, and [events](events.md) stay on one page. Server push fixes that: a component says which topics it follows (`brick: {listen:}`), an action or any Go code publishes a topic when it changed something, and every open page holding a listening component re-renders it. Presence builds on the same stream to show who else is on a page right now. You want push whenever data changes from somewhere other than the page itself: another user, a webhook, a background job.

## Before you start

- A listening component is stateful (`state:`), because a refresh is an action.
- The engine has `Options.Push` (`brick.NewPush`); without it a listening file fails to compile.
- The page has `{{ assets }}`; the runtime opens one event stream per page for every listening component on it.
- Your router serves the engine's events endpoint, `/_brick/events` (`Engine.EventsHandler()`). `site.Site` and the `brick` CLI do this for you.
- The example is [examples/live-push-presence](../../../examples/live-push-presence/). Run it from the `examples` folder.

## Step 1 — Listen to a topic

The order list follows the `orders` topic. It reads the orders through `orders`, a Go function of the app, so a refresh always shows the current data. Its `ship` action changes the data through `shop.ship` (a capability the host gives scripts) and publishes the topic.

`examples/live-push-presence/theme/components/Orders.html`

<!-- snippet: examples/live-push-presence/theme/components/Orders.html -->
```html
---
state: {}
brick: {listen: orders}
---
<brick:script>
def ship(state, props, id):
    shop.ship(id)
    brick.publish("orders")
    return state
</brick:script>
<ul class="orders">{{ range orders }}
  <li>#{{ .ID }} {{ .Status }}{{ if eq .Status "pending" }} <button brick:click="ship({{ .ID }})">Ship</button>{{ end }}</li>{{ end }}
</ul>
```

`brick: {listen:}` takes one topic, a list (`[orders, "shop:{{ .Props.shop }}"]`), or `{topics: [...], keep_alive: true, presence: …}`. A topic is 1 to 200 characters from `A-Z a-z 0-9 _ . : / @ -`, and may use `{{ .Props.x }}`, `{{ .State.x }}`, `{{ .Params.x }}` and `{{ .Key }}`.

## Step 2 — Turn push on and decide who may listen

`brick.NewPush` with no broadcaster keeps topics in this process. `AuthorizeTopic` runs when a stream opens and again on every recheck (each minute, `PushOptions.Recheck`), so access you revoke ends the stream. `Presence.Identify` says how a viewer appears on a presence list.

`examples/live-push-presence/main.go`

<!-- snippet: examples/live-push-presence/main.go lines=95-137 -->
```go
// newApp wires push into the engine. The in-memory broadcaster serves one
// process; several servers need contrib/push-redis and a shared Sealer.
func newApp(dir string) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	s := &shop{orders: []order{{1, statusPending}, {2, statusPending}}}
	push, err := brick.NewPush(brick.PushOptions{
		// Only a signed-in viewer may follow a topic. It is asked when the
		// stream opens and again on every recheck, so a sign-out ends it.
		AuthorizeTopic: func(r *http.Request, topic string) bool { return viewer(r) != "" },
		Presence: brick.PresenceOptions{
			// Identify says how a viewer appears on a presence list. Without
			// it an entry is an anonymous id with no data.
			Identify: func(r *http.Request, topic string) (brick.PresenceEntry, bool) {
				name := viewer(r)
				if name == "" {
					return brick.PresenceEntry{}, false
				}
				return brick.PresenceEntry{ID: name, Data: map[string]string{"name": name}}, true
			},
		},
	})
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{Globals: starlark.StringDict{"shop": shopModule(s)}})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		Push:      push,
		Funcs:     map[string]any{"orders": s.list},
	})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng, shop: s}, nil
}
```

The `shop` capability the action uses:

<!-- ds:block id=shopmodule-ctac59hf title="examples/live-push-presence/main.go" -->
**examples/live-push-presence/main.go** · [`examples/live-push-presence/main.go:142-155`](../../../examples/live-push-presence/main.go#L142-L155)

```go
func shopModule(s *shop) *starlarkstruct.Module {
	return &starlarkstruct.Module{Name: "shop", Members: starlark.StringDict{
		"ship": starlark.NewBuiltin("shop.ship", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var id int
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id); err != nil {
				return nil, err
			}
			if !s.ship(id) {
				return nil, fmt.Errorf("shop.ship: no order %d", id)
			}
			return starlark.None, nil
		}),
	}}
}
```
<!-- /ds:block hash=d3c3e0 -->

## Step 3 — Publish from Go code too

Data also changes outside actions: a webhook, a job, an admin tool. `Engine.Publish(ctx, topic)` refreshes every listener the same way. The handler below changes the data first, then publishes with a context that outlives the request, so a caller that hangs up cannot cancel the publish after the order already changed.

<!-- ds:block id=app-shipped-r6s4gpsd title="examples/live-push-presence/main.go" -->
**examples/live-push-presence/main.go** · [`examples/live-push-presence/main.go:193-207`](../../../examples/live-push-presence/main.go#L193-L207)

```go
func (a *app) shipped(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || !a.shop.ship(id) {
		http.Error(w, "no such order", http.StatusNotFound)
		return
	}
	// Engine.Publish refreshes every open listener of the topic. The
	// context is detached so a caller that hangs up cannot cancel the
	// publish after the data already changed.
	if err := a.eng.Publish(context.WithoutCancel(r.Context()), topicOrders); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```
<!-- /ds:block hash=d8ee61 -->

The routes, including the events endpoint:

<!-- ds:block id=app-handler-gqqj2b9h title="examples/live-push-presence/main.go" -->
**examples/live-push-presence/main.go** · [`examples/live-push-presence/main.go:161-174`](../../../examples/live-push-presence/main.go#L161-L174)

```go
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.Handle("POST "+brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle("GET "+brick.DefaultAssetBase+brick.EventsEndpoint, a.eng.EventsHandler())
	mux.HandleFunc("GET /orders/{$}", a.page("content/orders/index.html"))
	mux.HandleFunc("GET /room/{$}", a.page("content/room/index.html"))
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: userCookie, Value: r.URL.Query().Get("as"), Path: "/"})
		http.Redirect(w, r, "/orders/", http.StatusSeeOther)
	})
	mux.HandleFunc("POST /webhook/shipped/{id}", a.shipped)
	return mux
}
```
<!-- /ds:block hash=2da40d -->

## Step 4 — Show who is here

A presence topic is declared under `listen:` and read with `{{ .Presence "topic" }}`. Each open page is one connection; entries with the same `ID` are one viewer, with `Conns` counting their tabs. A join or a leave refreshes every viewer on the topic.

`examples/live-push-presence/theme/components/Room.html`

<!-- snippet: examples/live-push-presence/theme/components/Room.html -->
```html
---
state: {}
props: {id: {type: string}}
brick: {listen: {presence: "room:{{ .Props.id }}"}}
---
{{ $p := .Presence (printf "room:%s" .Props.id) }}
<div class="room">
  <p>{{ $p.Total }} here</p>
  <ul>{{ range $p.Entries }}<li>{{ index .Data "name" }}{{ if gt .Conns 1 }} ({{ .Conns }} tabs){{ end }}</li>{{ end }}</ul>
</div>
```

## Step 5 — Run more than one server

The in-memory broadcaster knows only the streams of its own process, so behind a load balancer a topic published on server A would never reach a page whose stream is open on server B. `contrib/push-redis` is a `brick.Broadcaster` on Redis pub/sub: you build the Redis client yourself, give `pushredis.New` a prefix, and hand the result to `brick.NewPush`. Presence needs one list every server shares, so `pushredis.NewPresenceStore` goes in `PresenceOptions.Store`. Every server must also share the same `PushOptions.Sealer` (`brick.NewAESGCMSealer` with one key), because a listen token sealed by the server that rendered the page is opened by whichever server the stream reaches.

The wiring, from the module's own example (`Example` in `contrib/push-redis/example_test.go`). It has no `// Output` line on purpose: `pushredis.New` subscribes to a real Redis before it returns, so the example only compiles under `go test` and runs when a Redis is on `localhost:6379` (the module's integration tests use the shared local one). It also leaves out the shared `Sealer`, which your own `PushOptions` adds:

<!-- snippet: contrib/push-redis/example_test.go lines=30-55 -->
```go
func Example() {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer rdb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	opts := pushredis.Options{Prefix: "app1:"}

	b, err := pushredis.New(ctx, rdb, opts)
	if err != nil {
		fmt.Println("no Redis:", err)
		return
	}
	defer b.Close()

	push, err := brick.NewPush(brick.PushOptions{
		Broadcaster: b,
		Presence:    brick.PresenceOptions{Store: pushredis.NewPresenceStore(rdb, opts)},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	_, err = brick.New(brick.Options{FS: pushTheme, Push: push})
	fmt.Println("engine built:", err == nil)
}
```

That example has no `Output:` line on purpose: `pushredis.New` subscribes to a real Redis before it returns, so running it needs one, and a documentation example must not fail `go test` on a machine without Redis. It still compiles, so the wiring cannot rot. The module's own tests (`task test:redis` in `contrib/push-redis`) run the whole path against the shared local Redis: server A renders the page, the event stream opens on server B, and a publish on A reaches it. Read the [module README](../../../contrib/push-redis/README.md) for the channel layout, revocation across servers and what happens in an outage. There is no runnable copy under `examples/`, because that module would need its own Redis client dependency.

## Try it

From the `examples` folder:

```sh
go run ./live-push-presence -demo
```

The demo signs in Ada and Bob (a `user` cookie each), opens real event streams on a local test server, and refreshes a component whenever its stream says so, as the runtime does:

<!-- snippet: examples/live-push-presence/testdata/transcript.txt -->
```text
Ada: GET /orders/
  <ul class="orders"><li>#1 pending <button brick:click="ship(1)">Ship</button></li><li>#2 pending <button brick:click="ship(2)">Ship</button></li></ul>
Ada: GET /_brick/events?l=<listen token>  (open)
Bob: POST Orders.ship(1)
  effects: 0
Ada: ← event: refresh  data: orders
Ada: POST Orders.$refresh()
  <ul class="orders"><li>#1 shipped</li><li>#2 pending <button brick:click="ship(2)">Ship</button></li></ul>
webhook: POST /webhook/shipped/2 → 204
Ada: ← event: refresh  data: orders
Ada: POST Orders.$refresh()
  <ul class="orders"><li>#1 shipped</li><li>#2 shipped</li></ul>
signed out: GET /_brick/events → events: 403
Ada: GET /room/
  <div class="room"><p>0 here</p><ul></ul></div>
Ada: ← event: refresh  data: brick:presence:room:lobby
Ada: POST Room.$refresh()
  <div class="room"><p>1 here</p><ul><li>Ada</li></ul></div>
Bob: GET /room/ and its stream
Ada: ← event: refresh  data: brick:presence:room:lobby
Ada: POST Room.$refresh()
  <div class="room"><p>2 here</p><ul><li>Ada</li><li>Bob</li></ul></div>
Bob closes the tab
Ada: ← event: refresh  data: brick:presence:room:lobby
Ada: POST Room.$refresh()
  <div class="room"><p>1 here</p><ul><li>Ada</li></ul></div>
```

Notice that Bob's `ship` answer carries no effects: `brick.publish` is never sent to the client, it fires on the server once the action succeeded and its state was saved. An event carries only the topic; each listening component then reads its data again through its own render, with every check that render has. Ada is not on the lobby list until her stream opens, and presence events use the engine's own topic `brick:presence:room:lobby`.

`go test ./live-push-presence/` checks this transcript. To try it in two browsers, run `go run ./live-push-presence`, open http://localhost:8086/login?as=Ada in one and http://localhost:8086/login?as=Bob in a private window, then `/orders/` and `/room/` in both.

## What happens in the browser

- One stream per page, reopened only when the page's set of topics changes. Each listening component refreshes at most once per tick, however many events arrive.
- A hidden tab waits until it is shown before refreshing, unless the component says `keep_alive: true`.
- After a dropped connection the browser reconnects and every listener refreshes once, because events published meanwhile are not replayed.
- A refused stream is retried with backoff, 1 s doubling to 30 s, 8 times; then the runtime emits `push-error` with `{failures}` (see the [runtime events](../../reference/runtime-api.md#runtime-events)).

## Pitfalls

- **Push must be on.** A listening component without `Options.Push` fails to compile with `brick: listen: server push is off (Options.Push); set one, such as brick.NewPush(brick.PushOptions{}), so a listening component never silently stops updating`.
- **Listening needs state.** Without `state:` the file fails with `brick: listen: needs state: — a pushed topic refreshes the component, which is an action`.
- **A topic is a name, not a secret.** The browser never names its topics: each listening root carries a listen token the server sealed (the topics, the session, an expiry of 24 hours, `PushOptions.TokenTTL`). A forged token or one from another session is refused with 403, an expired one with 410. Put real access control in `AuthorizeTopic`; the demo's signed-out viewer gets `403`.
- **Bad topic names fail loudly.** `eng.Publish(ctx, "order 7")` returns `"order 7" is not a topic (1-200 characters from A-Z a-z 0-9 _ . : / @ -)`, and topics starting with `brick:` are the engine's own (`topics starting with "brick:" are the engine's own`).
- **Publishing waits for success.** `brick.publish` in an action fires only after the action succeeded and saved its state; a failed action publishes nothing, and a topic named twice in one action is published once.
- **Presence is per process unless you share a store.** With the in-memory broadcaster the list lives in this process. With any other `Broadcaster` (`contrib/push-redis` for several servers) set `PresenceOptions.Store` (`pushredis.NewPresenceStore`), or a presence component fails to render with an error that names it. The servers must also share `PushOptions.Sealer`, or a token rendered by one is refused by another.
- **Presence data is what you put there.** Without `Identify` an entry is an anonymous random id with no data. `Data` is fixed when the viewer joins. `.Presence` answers only for topics the component itself declares, sorted by `ID` and capped at 100 entries (`Total` counts all); an entry leaves when its stream closes and lapses after 90 s (`PresenceOptions.TTL`) if its server dies.
- **Limits.** 1,000 streams per process (`MaxConnections`, then 503), 64 topics per stream (`MaxTopics`), a heartbeat every 25 s (`Heartbeat`).
- **Sign-out should end streams.** `Engine.RevokeSession(ctx, sessionID)`, called after you end the session in your own store, ends that session's state and push streams on every server sharing the broadcaster.

## Related

- [Background jobs](background-jobs.md), which publish a topic when they end.
- [Events](events.md), for updates within one page.
- Feature index: [Push, presence and jobs](../../reference/features.md#push-presence-and-jobs).
- The one-page guide: [Server push](../../guide.md#server-push--live-updates-from-elsewhere).
- Several servers: [contrib/push-redis](../../../contrib/push-redis/README.md).
- Example: [examples/live-push-presence](../../../examples/live-push-presence/).
