package pushredis_test

import (
	"context"
	"fmt"
	"testing/fstest"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ubgo/brick"
	pushredis "github.com/ubgo/brick/contrib/push-redis"
)

// connectTimeout bounds the subscription handshake in New.
const connectTimeout = 5 * time.Second

// pushTheme is a page with no components: the example is about the wiring.
var pushTheme = fstest.MapFS{
	"content/index.html": {Data: []byte("<h1>Live</h1>\n")},
}

// Redis carries a published topic to the event streams of every server,
// and holds who is present on each topic. The host builds the client;
// both pieces share one Prefix. This example has no Output line on
// purpose: pushredis.New subscribes to a real Redis before it returns, so
// running it needs one (the module's integration tests run against the
// shared local Redis), and an example that needs a service must not fail
// `go test` on a machine without it. It still compiles, so the wiring
// cannot rot.
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
