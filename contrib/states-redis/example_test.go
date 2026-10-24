package statesredis_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing/fstest"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ubgo/brick"
	statesredis "github.com/ubgo/brick/contrib/states-redis"
)

// stateTTL is how long an instance lives after its last use; a read
// refreshes it, so a component in use stays alive.
const stateTTL = 30 * time.Minute

// cartTheme has one component in the default store and one that picks the
// named store "cart" with brick: {store: cart}.
var cartTheme = fstest.MapFS{
	"components/Counter.html": {Data: []byte("---\nstate:\n  n: 0\n---\n<p>{{ .State.n }}</p>\n")},
	"components/Cart.html":    {Data: []byte("---\nbrick: {store: cart}\nstate:\n  items: 0\n---\n<p>{{ .State.items }}</p>\n")},
	"content/index.html":      {Data: []byte("<Counter />\n<Cart />\n")},
}

// Redis holds the state of every server behind a load balancer. The host
// builds the client (address, password, TLS are its choice); the store
// never opens a connection itself, and brick.Bytes owns the handles, the
// TTL, the codec and any encryption at rest. This example only builds the
// configuration; `task test:redis` runs the store against a real Redis.
func Example() {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer rdb.Close()

	states := brick.Bytes(statesredis.New(rdb, statesredis.Options{Prefix: "shop:"}), brick.BytesOptions{TTL: stateTTL})
	carts := brick.Bytes(statesredis.New(rdb, statesredis.Options{Prefix: "shop:cart:"}), brick.BytesOptions{TTL: 24 * time.Hour})

	_, err := brick.New(brick.Options{
		FS:          cartTheme,
		States:      states,                                     // the store named "default"
		StateStores: map[string]brick.StateStore{"cart": carts}, // brick: {store: cart}
	})
	fmt.Println("engine built:", err == nil)
	// Output:
	// engine built: true
}

// A Redis outage is an error, never "not found": the engine answers an
// action with 503 and Retry-After instead of 410, so open tabs do not all
// reset their components at once.
func ExampleNew_outage() {
	// A port nothing listens on, with retries off: the shape of an outage.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println(err)
		return
	}
	addr := l.Addr().String()
	_ = l.Close() // freeing the port is the point: nothing listens there now, so Redis is "down"
	rdb := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1, DialTimeout: time.Second})
	defer rdb.Close()

	states := brick.Bytes(statesredis.New(rdb, statesredis.Options{}), brick.BytesOptions{TTL: stateTTL})
	_, found, err := states.Get(context.Background(), "0123")
	fmt.Println("found:", found)
	fmt.Println("store error:", err != nil)
	fmt.Println("reads as gone:", errors.Is(err, brick.ErrGone))
	// Output:
	// found: false
	// store error: true
	// reads as gone: false
}
