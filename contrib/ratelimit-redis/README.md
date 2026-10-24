# brick/contrib/ratelimit-redis

An `actionmw.Limiter` on Redis ([go-redis v9](https://github.com/redis/go-redis)). It is the same token bucket as `actionmw.MemoryLimiter` (n actions per period, a burst of n, a retry-after when refused), held in Redis so every server behind a load balancer spends one shared budget per key.

## Use it

```go
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"}) // your client, your way
limiter := ratelimitredis.New(rdb, 20, time.Minute, ratelimitredis.Options{Prefix: "app1:"})
eng, err := brick.New(brick.Options{
    ActionMiddleware: []brick.ActionMiddleware{actionmw.RateLimit(limiter, actionmw.BySession)},
})
```

Runnable version: `Example` wires the limiter into `brick.Options.ActionMiddleware` and prints the Redis key a bucket lives under, without contacting Redis; `ExampleLimiter_Allow_outage` points at a closed local port and shows `actionmw.RateLimit` refusing the action with 503 instead of letting it through. `task test:redis` runs the limiter against a real Redis. Run it with `go test -run Example .` in this folder ([example_test.go](example_test.go)).

Guide: [action-middleware.md](../../docs/guides/production/action-middleware.md).

The host builds the client (`*redis.Client`, `*redis.ClusterClient` or `*redis.Ring`); this module never opens a connection. `New` panics on a nil client, `n < 1` or a period under a microsecond, because each is a programming error.

| Option | Default | Meaning |
|---|---|---|
| `Prefix` | none | goes in front of every key, so several apps or test runs can share one Redis |

**Atomic.** Each `Allow` is one Lua script that refills the bucket, spends a token if there is one and writes it back, so concurrent callers on any number of servers never get more than the bucket holds. The script reads the time with Redis's `TIME`, never the caller's clock, so servers with skewed clocks agree on one refill rate. `TIME` before a write needs effects replication, the default since Redis 5.

**Layout.** Each key's bucket is a hash at `<prefix>brick:rl:<key>` holding the tokens left (`tk`) and when they were counted (`ts`, in microseconds). Every call sets `PEXPIRE` to one period: a bucket idle that long is full again, so deleting it changes nothing, and idle keys never pile up. `Limiter.Key(key)` returns the Redis key, for an operator who wants to inspect or reset one.

**Outages.** A Redis failure is returned as an error, never as an allow. `actionmw.RateLimit` answers an error with 503 and `Retry-After`, so an outage refuses actions instead of silently removing the limit. A host that prefers to fail open wraps the limiter and makes that choice explicitly.

**Cluster.** The script touches one key, so it runs on Redis Cluster unchanged. Use `noeviction` or a `volatile-*` policy; evicting a bucket only refills it early.

## Tests

`go test ./...` needs no network: it checks that an outage is an error and a refusal, the key layout and the constructor's refusals. `task test:redis` runs the live tests against the shared local Redis (`BRICK_REDIS_TEST=1`; override `BRICK_REDIS_ADDR`, default `localhost:7012`, and `BRICK_REDIS_PASSWORD`): the call after n is refused with a retry-after just under one token's time, the bucket refills after the period and never beyond the burst, two limiters share one bucket, keys carry the prefix and a TTL of one period, and 64 concurrent callers get exactly n through. Each run writes under a fresh `brick-test:<random>:` prefix and deletes only those keys.
