package ratelimitredis_test

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ubgo/brick"
	"github.com/ubgo/brick/actionmw"
	ratelimitredis "github.com/ubgo/brick/contrib/ratelimit-redis"
)

// actionsPerMinute is each session's budget: 20 actions a minute, with a
// burst of 20, shared by every server that points at the same Redis.
const actionsPerMinute = 20

// The host builds the client; the limiter never opens a connection, and
// actionmw.RateLimit turns it into action middleware keyed by session.
// This example only builds the configuration; `task test:redis` runs the
// limiter against a real Redis.
func Example() {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer rdb.Close()

	limiter := ratelimitredis.New(rdb, actionsPerMinute, time.Minute, ratelimitredis.Options{Prefix: "shop:"})
	opts := brick.Options{
		ActionMiddleware: []brick.ActionMiddleware{actionmw.RateLimit(limiter, actionmw.BySession)},
	}
	fmt.Println(len(opts.ActionMiddleware), "middleware")
	fmt.Println(limiter.Key("session-hash")) // the Redis key an operator inspects or resets
	// Output:
	// 1 middleware
	// shop:brick:rl:session-hash
}

// A Redis outage refuses the action with 503 rather than letting it
// through, so an outage never silently removes the limit.
func ExampleLimiter_Allow_outage() {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println(err)
		return
	}
	addr := l.Addr().String()
	_ = l.Close() // freeing the port is the point: nothing listens there now, so Redis is "down"
	rdb := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1, DialTimeout: time.Second})
	defer rdb.Close()

	limiter := ratelimitredis.New(rdb, actionsPerMinute, time.Minute, ratelimitredis.Options{})
	ran := false
	next := func(context.Context, *brick.ActionCall) (*brick.ActionResult, error) {
		ran = true
		return &brick.ActionResult{}, nil
	}
	_, err = actionmw.RateLimit(limiter, actionmw.BySession)(next)(context.Background(), &brick.ActionCall{Session: "s1"})
	fmt.Println("action ran:", ran)
	fmt.Println("status:", brick.ActionStatus(err))
	// Output:
	// action ran: false
	// status: 503
}
