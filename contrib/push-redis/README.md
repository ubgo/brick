# brick/contrib/push-redis

A `brick.Broadcaster` on Redis pub/sub ([go-redis v9](https://github.com/redis/go-redis)), so server push works behind a load balancer: a topic published on any server refreshes the listening components on every server.

```go
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"}) // your client, your way
b, err := pushredis.New(ctx, rdb, pushredis.Options{Prefix: "app1:"})
if err != nil { ... }
defer b.Close()
push, err := brick.NewPush(brick.PushOptions{Broadcaster: b, Sealer: sealer}) // the Sealer every server shares
opts.Push = push
```

The host builds the client (`*redis.Client`, `*redis.ClusterClient` or `*redis.Ring`). `New` returns once Redis confirmed the subscription, so a publish right after it is not missed, and fails if Redis is unreachable. A nil client panics, because it is a programming error.

| Option | Default | Meaning |
|---|---|---|
| `Prefix` | none | goes in front of every channel, so several apps or test runs can share one Redis |

**Share the Sealer.** A listen token is sealed by the server that rendered the page and opened by whichever server the browser's event stream reaches. With several servers, give every `brick.NewPush` the same `PushOptions.Sealer` (for example the `brick.NewAESGCMSealer` your sealed states use), or a stream that lands on another server is refused with 403.

**Layout.** Each topic is published on `<prefix>brick:push:<topic>` with an empty message: an event is a refresh signal, never data. Each `Broadcaster` holds one pattern subscription, `<prefix>brick:push:*`, and fans every message out to its own streams in memory, so a thousand open streams cost one Redis connection. Every server therefore receives every topic under its prefix; that is the price of one connection, and it is cheap while publishes are signals. `Broadcaster.Channel(topic)` returns the channel, for `redis-cli PSUBSCRIBE` while debugging. Pub/sub writes no keys.

**Revocation.** `brick.Push.RevokeSession` publishes the engine-owned topic `brick:revoke:<session hash>` through the broadcaster, so a sign-out on one server ends that session's open event streams on every server under the prefix, over the same subscription. Call it after ending the session in your session store: a reconnect is refused because the store no longer answers that session.

**Presence across servers (v22).** A component that shows who is on a topic (`brick: {listen: {presence: "room:7"}}`, `{{ .Presence "room:7" }}`) needs one list every server shares. `pushredis.NewPresenceStore(rdb, pushredis.Options{Prefix: "app1:"})` goes in `brick.PushOptions{Presence: brick.PresenceOptions{Store: store, Identify: ...}}`; without it, a presence component under this broadcaster fails to render with an error naming it, so a per-process list never passes for the whole site. Layout: one hash per presence topic at `<prefix>brick:presence:<topic>`, field = connection id, value = JSON `{id, d, x}` with its own expiry (works on every Redis version, no `HPEXPIRE`). A join runs a Lua script, so the per-topic cap (`PresenceOptions.MaxEntries`, 1 000 connections) holds across servers, and the key expires 2× the entry TTL after the last join, so a topic whose servers all died cleans itself up.

**Outages.** `Publish` returns Redis's error (an action's `brick.publish` reports it and the action still succeeds, because its write already happened). go-redis re-establishes a dropped subscription by itself; topics published meanwhile are lost, and the browser runtime covers that by refreshing every listening component once when its own stream reconnects.

## A complete example

`Example` wires the broadcaster and the presence store on one prefix and builds an engine with them. It has no `// Output:` line on purpose: `pushredis.New` subscribes to a real Redis before it returns, so running it needs one, and an example that needs a service must not fail `go test` on a machine without it. It is compiled by `go test -run Example .`, so the wiring cannot rot; the module's integration tests run it against a live Redis.

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

## Tests

`go test ./...` needs no network: it checks that `New` fails without a Redis, the channel layout and pattern escaping, and the nil-client panic. `task test:redis` runs the live tests against the shared local Redis (`BRICK_REDIS_TEST=1`; override `BRICK_REDIS_ADDR`, default `localhost:7012`, and `BRICK_REDIS_PASSWORD`): two broadcasters on one prefix deliver each other's topics and only the topics subscribed, another prefix hears nothing, the whole path across two servers, where server A renders the page, the event stream opens on server B, and a publish on A reaches it, and a `RevokeSession` on A ending that stream on B. The presence tests check a join and a leave seen across two servers and the store's atomic cap and expiry. Each run uses a fresh `brick-test:<random>:` prefix.
