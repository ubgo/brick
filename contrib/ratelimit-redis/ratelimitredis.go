// Package ratelimitredis is an actionmw.Limiter on Redis (go-redis v9):
// the same token bucket as actionmw.MemoryLimiter, n actions per period
// with a burst of n, but held in Redis so every server behind a load
// balancer spends one shared budget per key.
//
//	limiter := ratelimitredis.New(rdb, 20, time.Minute, ratelimitredis.Options{Prefix: "app1:"})
//	eng.ActionMiddleware = append(eng.ActionMiddleware, actionmw.RateLimit(limiter, actionmw.BySession))
//
// The host builds the client (a *redis.Client, *redis.ClusterClient or
// *redis.Ring); this package never opens a connection.
//
// Layout: one hash per key at <prefix>brick:rl:<key> holding the tokens
// left and the time they were counted. Each Allow is one Lua script that
// refills, spends and writes back atomically, so concurrent callers on
// any number of servers never spend more than the bucket holds. The
// script reads the time from Redis (TIME), never from the caller, so
// servers with skewed clocks still agree on one refill rate. Every write
// sets PEXPIRE to one period: a bucket idle that long is full again, so
// deleting it changes nothing and idle keys never pile up. One key per
// script, so it runs on Redis Cluster unchanged. TIME before a write
// needs effects replication, the default since Redis 5.
//
// Outages: a Redis failure is returned as an error, never as an allow.
// actionmw.RateLimit answers an error with 503, so an outage refuses
// actions rather than silently removing the limit; a host that prefers
// to fail open wraps the Limiter and decides so explicitly.
package ratelimitredis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ubgo/brick/actionmw"
)

// keySegment is part of the stored format: an operator greps for it, and
// two deploys sharing a Redis must agree on it.
const keySegment = "brick:rl:"

// Options configures New.
type Options struct {
	// Prefix goes in front of every key, so several apps (or test runs)
	// can share one Redis. Empty means no prefix. End it with a separator
	// such as ":"; it is used as given.
	Prefix string
}

// Limiter is an actionmw.Limiter whose buckets live in Redis. Safe for
// concurrent use, within one process and across many.
type Limiter struct {
	c      redis.UniversalClient
	prefix string
	burst  int64
	perUS  int64 // the period in microseconds, TIME's resolution
}

var _ actionmw.Limiter = (*Limiter)(nil)

// New returns a limiter allowing n actions per period per key. A nil
// client, n < 1 or a period under a microsecond is a programming error,
// so it panics here rather than failing or refusing every later call.
func New(c redis.UniversalClient, n int, per time.Duration, opts Options) *Limiter {
	if c == nil {
		panic("ratelimitredis: New needs a Redis client")
	}
	if n < 1 {
		panic(fmt.Sprintf("ratelimitredis: n must be at least 1, got %d", n))
	}
	if per < time.Microsecond {
		panic(fmt.Sprintf("ratelimitredis: the period must be at least 1µs, got %s", per))
	}
	return &Limiter{c: c, prefix: opts.Prefix, burst: int64(n), perUS: per.Microseconds()}
}

// Key is the Redis key that holds the bucket for key.
func (l *Limiter) Key(key string) string { return l.prefix + keySegment + key }

// allowScript is the whole decision, run atomically inside Redis.
// KEYS[1] the bucket; ARGV[1] burst n; ARGV[2] period in µs.
// Tokens refill at n per period and cap at n; one action spends one.
// Returns {1, 0} when allowed, or {0, µs until one token is back}.
// The retry is rounded up so a caller that waits exactly that long finds
// the token there. The expiry is the period rounded up to whole ms.
var allowScript = redis.NewScript(`
local burst = tonumber(ARGV[1])
local per = tonumber(ARGV[2])
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000000 + tonumber(t[2])
local v = redis.call('HMGET', KEYS[1], 'tk', 'ts')
local tokens = tonumber(v[1])
local ts = tonumber(v[2])
if tokens == nil or ts == nil then
	tokens = burst
elseif now > ts then
	tokens = math.min(burst, tokens + (now - ts) * burst / per)
end
local ok, retry = 0, 0
if tokens >= 1 then
	tokens = tokens - 1
	ok = 1
else
	retry = math.ceil((1 - tokens) * per / burst)
end
redis.call('HSET', KEYS[1], 'tk', string.format('%.17g', tokens), 'ts', string.format('%d', now))
redis.call('PEXPIRE', KEYS[1], math.max(1, math.ceil(per / 1000)))
return {ok, retry}`)

// Allow spends one token from key's bucket if there is one. A refusal
// carries the wait until the next token. A Redis failure returns the
// error and false: the caller decides, and actionmw.RateLimit refuses.
func (l *Limiter) Allow(ctx context.Context, key string) (bool, time.Duration, error) {
	res, err := allowScript.Run(ctx, l.c, []string{l.Key(key)}, l.burst, l.perUS).Int64Slice()
	if err != nil {
		return false, 0, fmt.Errorf("ratelimitredis: %w", err)
	}
	if len(res) != 2 {
		return false, 0, fmt.Errorf("ratelimitredis: script returned %d values, want 2", len(res))
	}
	if res[0] == 1 {
		return true, 0, nil
	}
	return false, time.Duration(res[1]) * time.Microsecond, nil
}
