package statesredis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ubgo/brick"
	"github.com/ubgo/brick/statetest"
)

// The integration tests run against a real Redis only when
// BRICK_REDIS_TEST=1 (task test:redis), so the default gate needs no
// server. The defaults match the shared local Redis 8. Every run writes
// under a fresh prefix and deletes only that prefix: the Redis is shared,
// so the tests never flush or reconfigure it.
const (
	integrationEnv = "BRICK_REDIS_TEST"
	defaultAddr    = "localhost:7012"
	testTTL        = time.Minute
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
		deletePrefix(t, c, prefix)
		_ = c.Close()
	})
	return c, prefix
}

// deletePrefix removes every key this run wrote, and nothing else.
func deletePrefix(t *testing.T, c *redis.Client, prefix string) {
	t.Helper()
	ctx := context.Background()
	iter := c.Scan(ctx, 0, escapeGlob(prefix)+"*", scanCount).Iterator()
	for iter.Next(ctx) {
		if err := c.Del(ctx, iter.Val()).Err(); err != nil {
			t.Errorf("cleanup %s: %v", iter.Val(), err)
		}
	}
	if err := iter.Err(); err != nil {
		t.Errorf("cleanup scan: %v", err)
	}
}

func TestIntegrationConformance(t *testing.T) {
	c, prefix := integrationClient(t)
	var n atomic.Int64
	// Each subtest gets its own prefix, so it starts with an empty store.
	fresh := func() *Store { return New(c, Options{Prefix: fmt.Sprintf("%s%d:", prefix, n.Add(1))}) }

	t.Run("Plain", func(t *testing.T) {
		statetest.Store(t, func(*testing.T) brick.StateStore {
			return brick.Bytes(fresh(), brick.BytesOptions{TTL: testTTL})
		}, statetest.StoreOptions{})
	})
	t.Run("EncryptedAtRest", func(t *testing.T) {
		keys, err := brick.StaticKeys(make([]byte, brick.StateKeySize))
		if err != nil {
			t.Fatal(err)
		}
		sealer := brick.NewAESGCMSealer(keys, nil)
		statetest.Store(t, func(*testing.T) brick.StateStore {
			return brick.Bytes(fresh(), brick.BytesOptions{TTL: testTTL, Sealer: sealer})
		}, statetest.StoreOptions{})
	})
}

// Load refreshes the expiry of the value and of its session set to the
// TTL it was saved with.
func TestIntegrationLoadRefreshesTTL(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	s := New(c, Options{Prefix: prefix})
	if err := s.Save(ctx, "h1", []byte("v"), testTTL, "sessA"); err != nil {
		t.Fatal(err)
	}
	const short = 2 * time.Second
	for _, k := range []string{s.stateKey("h1"), s.sessionKey("sessA")} {
		if err := c.PExpire(ctx, k, short).Err(); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := s.Load(ctx, "h1")
	if err != nil || !ok || string(got) != "v" {
		t.Fatalf("Load = %q %v %v", got, ok, err)
	}
	for _, k := range []string{s.stateKey("h1"), s.sessionKey("sessA")} {
		if ttl := c.PTTL(ctx, k).Val(); ttl <= short {
			t.Errorf("%s: TTL %v after Load, want refreshed to about %v", k, ttl, testTTL)
		}
	}
}

// RevokeSession removes exactly that session's instances: not another
// session's, and not a handle saved again under another session since.
func TestIntegrationRevokeExactlyThatSession(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	s := New(c, Options{Prefix: prefix})
	save := func(key, session string) {
		t.Helper()
		if err := s.Save(ctx, key, []byte(key), testTTL, session); err != nil {
			t.Fatal(err)
		}
	}
	save("a1", "sessA")
	save("a2", "sessA")
	save("b1", "sessB")
	save("none", "")
	save("moved", "sessA")
	save("moved", "sessB") // re-bound: still in sessA's set, but no longer sessA's

	if err := s.RevokeSession(ctx, "sessA"); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]bool{"a1": false, "a2": false, "b1": true, "none": true, "moved": true} {
		if _, ok, err := s.Load(ctx, key); err != nil || ok != want {
			t.Errorf("after revoking sessA, %s present = %v (err %v), want %v", key, ok, err, want)
		}
	}
	if n := c.Exists(ctx, s.sessionKey("sessA")).Val(); n != 0 {
		t.Error("the revoked session's set must be emptied")
	}
	if err := s.RevokeSession(ctx, "sessA"); err != nil {
		t.Errorf("revoking twice: %v", err)
	}
}

// EachBytes sees only this prefix's instances, even when another prefix
// in the same Redis holds some too.
func TestIntegrationEachStaysInPrefix(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	mine := New(c, Options{Prefix: prefix + "[mine]:"})
	other := New(c, Options{Prefix: prefix + "other:"})
	for _, k := range []string{"m1", "m2"} {
		if err := mine.Save(ctx, k, []byte(k), testTTL, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := other.Save(ctx, "o1", []byte("o1"), testTTL, ""); err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	if err := mine.EachBytes(ctx, func(k string, b []byte) bool { seen[k] = string(b); return true }); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen["m1"] != "m1" || seen["m2"] != "m2" {
		t.Fatalf("EachBytes saw %v, want m1 and m2 only", seen)
	}
}
