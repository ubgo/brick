# brick/contrib/states-redis

A `brick.BytesStore` on Redis ([go-redis v9](https://github.com/redis/go-redis)), so component state survives a restart and is shared by every server behind a load balancer. Wrap it with `brick.Bytes`, which makes the handles and owns the TTL, the codec and any encryption at rest; this module only moves bytes.

## Use it

```go
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"}) // your client, your way
store := brick.Bytes(statesredis.New(rdb, statesredis.Options{Prefix: "app1:"}), brick.BytesOptions{
    TTL:    30 * time.Minute,
    Sealer: nil, // set brick.NewAESGCMSealer(keys, nil) to encrypt what Redis holds
})
eng, err := brick.New(brick.Options{FS: fsys, States: store})
```

Runnable version: `Example` wires a default store and a named `cart` store (`brick: {store: cart}`) into `brick.Options` without contacting Redis; `ExampleNew_outage` points at a closed local port and shows an outage reads as a store error (503), never as `ErrGone` (410). Neither needs a server; `task test:redis` runs the store against a real one. Run it with `go test -run Example .` in this folder ([example_test.go](example_test.go)).

Guide: [state-stores.md](../../docs/guides/production/state-stores.md).

The host builds the client (`*redis.Client`, `*redis.ClusterClient` or `*redis.Ring`); this module never opens a connection.

| Option | Default | Meaning |
|---|---|---|
| `Prefix` | none | goes in front of every key, so several apps or test runs can share one Redis |

**Layout.** Each instance is a hash at `<prefix>brick:st:<handle>` holding the bytes, the TTL and the session, with `PEXPIRE` set to the TTL. A read refreshes the expiry, so a component in use stays alive. Each session has a set `<prefix>brick:sess:<session>` of its handles, refreshed with them so it outlives every member.

**Upgrades.** The store is a `brick.Revoker` and a `brick.BytesLister`, so `brick.Bytes` over it is a `Revoker` (`Engine.RevokeSession` ends a session's live components at once) and a `Lister` (the live-instance inspector). `RevokeSession` deletes only instances still bound to that session; a handle saved again under another session survives. Listing uses `SCAN` on every master of a cluster or every shard of a ring. It is not a `Pruner`: Redis expires keys itself.

**Instance ends (v19).** The store reports revoked instances to `OnInstanceEnd` hooks and nothing else. Redis deletes an expired key and its value itself; keyspace notifications would need a server setting, carry only the key and are lossy pub/sub, so an expiry is not reported rather than faked. Use the memory or SQL store if a hook must hear expiries.

**Outages.** Every Redis failure is an error, never "not found", so the engine answers an action with 503 and `Retry-After` instead of 410, and open tabs do not all recover their components at once.

**Cluster.** No command or script touches two keys, so the store works on Redis Cluster unchanged. The shared Redis should use `noeviction` or a `volatile-*` policy: an evicted session set would let `RevokeSession` miss live instances until their TTL.

## Tests

`go test ./...` needs no network: it checks that an outage is an error at every entry point. `task test:redis` runs the root's `statetest` conformance suite (plain and encrypted at rest) and the TTL-refresh, revocation and listing tests against the shared local Redis (`BRICK_REDIS_TEST=1`; override `BRICK_REDIS_ADDR`, default `localhost:7012`, and `BRICK_REDIS_PASSWORD`). Each run writes under a fresh `brick-test:<random>:` prefix and deletes only those keys.
