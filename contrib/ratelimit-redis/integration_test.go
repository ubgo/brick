package ratelimitredis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// The integration tests run against a real Redis only when
// BRICK_REDIS_TEST=1 (task test:redis), so the default gate needs no
// server. The defaults match the shared local Redis 8. Every run writes
// under a fresh prefix and deletes only that prefix: the Redis is shared,
// so the tests never flush or reconfigure it.
const (
	integrationEnv = "BRICK_REDIS_TEST"
	defaultAddr    = "localhost:7012"
)

func integrationClient(t *testing.T) (*redis.Client, string) {
	t.Helper()
	if os.Getenv(integrationEnv) != "1" {
		t.Skipf("set %s=1 to run against a real Redis", integrationEnv)
	}
	addr := os.Getenv("BRICK_REDIS_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	c := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("BRICK_REDIS_PASSWORD")})
	ctx := context.Background()
	if err := c.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis at %s: %v", addr, err)
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	prefix := "brick-test:" + hex.EncodeToString(b[:]) + ":"
	t.Cleanup(func() {
		// The prefix is hex and fixed text, so it holds no glob characters.
		iter := c.Scan(ctx, 0, prefix+"*", 256).Iterator()
		for iter.Next(ctx) {
			if err := c.Del(ctx, iter.Val()).Err(); err != nil {
				t.Errorf("cleanup %s: %v", iter.Val(), err)
			}
		}
		if err := iter.Err(); err != nil {
			t.Errorf("cleanup scan: %v", err)
		}
		_ = c.Close()
	})
	return c, prefix
}

func TestIntegrationLimit(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	const n = 5
	l := New(c, n, time.Minute, Options{Prefix: prefix})
	for i := range n {
		ok, retry, err := l.Allow(ctx, "k")
		if err != nil || !ok || retry != 0 {
			t.Fatalf("call %d = %v %v %v; want allowed", i+1, ok, retry, err)
		}
	}
	ok, retry, err := l.Allow(ctx, "k")
	if err != nil || ok {
		t.Fatalf("call %d = %v %v; want refused", n+1, ok, err)
	}
	// One token comes back every minute/n = 12s; a little has refilled
	// since the bucket was emptied, so the wait is just under that.
	if per := time.Minute / n; retry <= per-time.Second || retry > per {
		t.Fatalf("retry-after = %s, want just under %s", retry, per)
	}
	// Another key has its own bucket.
	if ok, _, err := l.Allow(ctx, "other"); err != nil || !ok {
		t.Fatalf("a fresh key = %v %v; want allowed", ok, err)
	}
}

func TestIntegrationRefill(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	const per = 300 * time.Millisecond
	l := New(c, 2, per, Options{Prefix: prefix})
	for range 2 {
		if ok, _, err := l.Allow(ctx, "k"); err != nil || !ok {
			t.Fatalf("draining = %v %v", ok, err)
		}
	}
	ok, retry, err := l.Allow(ctx, "k")
	if err != nil || ok || retry <= 0 || retry > per/2 {
		t.Fatalf("empty bucket = %v %v %v; want refused with a wait up to %s", ok, retry, err, per/2)
	}
	time.Sleep(retry)
	if ok, _, err := l.Allow(ctx, "k"); err != nil || !ok {
		t.Fatalf("after waiting the retry-after = %v %v; want allowed", ok, err)
	}
	time.Sleep(per)
	for i := range 2 {
		if ok, _, err := l.Allow(ctx, "k"); err != nil || !ok {
			t.Fatalf("after a full period, call %d = %v %v; want a full bucket", i+1, ok, err)
		}
	}
	if ok, _, _ := l.Allow(ctx, "k"); ok {
		t.Fatal("the refill exceeded the burst")
	}
}

// Two limiters (two servers) over one Redis spend one budget.
func TestIntegrationSharedAcrossInstances(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	a := New(c, 4, time.Minute, Options{Prefix: prefix})
	b := New(c, 4, time.Minute, Options{Prefix: prefix})
	for i, l := range []*Limiter{a, b, a, b} {
		if ok, _, err := l.Allow(ctx, "k"); err != nil || !ok {
			t.Fatalf("call %d = %v %v; want allowed", i+1, ok, err)
		}
	}
	for name, l := range map[string]*Limiter{"a": a, "b": b} {
		if ok, _, err := l.Allow(ctx, "k"); err != nil || ok {
			t.Fatalf("instance %s after the shared budget = %v %v; want refused", name, ok, err)
		}
	}
}

// Keys carry the prefix and expire after one period, so an idle bucket
// never outlives the time it takes to refill.
func TestIntegrationKeyPrefixAndTTL(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	const per = 30 * time.Second
	l := New(c, 3, per, Options{Prefix: prefix})
	if _, _, err := l.Allow(ctx, "s:abc"); err != nil {
		t.Fatal(err)
	}
	var keys []string
	iter := c.Scan(ctx, 0, prefix+"*", 256).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	want := prefix + "brick:rl:s:abc"
	if len(keys) != 1 || keys[0] != want {
		t.Fatalf("keys = %v, want [%s]", keys, want)
	}
	ttl, err := c.PTTL(ctx, want).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= per-5*time.Second || ttl > per {
		t.Fatalf("PTTL = %s, want just under %s", ttl, per)
	}
}

// Many concurrent callers never get more than n through.
func TestIntegrationConcurrentNeverExceeds(t *testing.T) {
	c, prefix := integrationClient(t)
	const n, callers = 10, 64
	l := New(c, n, time.Minute, Options{Prefix: prefix})
	var allowed atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, _, err := l.Allow(context.Background(), "hot")
			if err != nil {
				t.Error(err)
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := allowed.Load(); got != n {
		t.Fatalf("%d of %d concurrent callers were allowed, want exactly %d", got, callers, n)
	}
}
