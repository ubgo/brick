package statesredis

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ubgo/brick"
)

// deadClient points at a port nothing listens on, with retries off, so
// every command fails at once: the shape of a Redis outage.
func deadClient(t *testing.T) *redis.Client {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	c := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1, DialTimeout: time.Second})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// An outage must be an error at every entry point, never a miss: the
// engine answers a store error with 503 and a miss with 410, and a 410
// makes every open tab recover its component at once.
func TestOutageIsAnErrorNotAMiss(t *testing.T) {
	ctx := context.Background()
	s := New(deadClient(t), Options{Prefix: "x:"})

	data, ok, err := s.Load(ctx, "0123")
	if err == nil || ok || data != nil {
		t.Fatalf("Load during an outage = %q %v %v; want an error", data, ok, err)
	}
	if err := s.Save(ctx, "0123", []byte("v"), time.Minute, "sess"); err == nil {
		t.Error("Save during an outage must fail")
	}
	if err := s.Save(ctx, "0123", []byte("v"), time.Minute, ""); err == nil {
		t.Error("Save without a session during an outage must fail")
	}
	if err := s.Remove(ctx, "0123"); err == nil {
		t.Error("Remove during an outage must fail")
	}
	if err := s.RevokeSession(ctx, "sess"); err == nil {
		t.Error("RevokeSession during an outage must fail")
	}
	if err := s.EachBytes(ctx, func(string, []byte) bool { return true }); err == nil {
		t.Error("EachBytes during an outage must fail")
	}

	// Through brick.Bytes, the StateStore the engine sees: an error, so
	// the action maps it to ErrStoreUnavailable (503), not ErrGone (410).
	inst, ok, err := brick.Bytes(s, brick.BytesOptions{}).Get(ctx, "0123")
	if err == nil || ok || inst != nil {
		t.Fatalf("Bytes.Get during an outage = %v %v %v; want an error", inst, ok, err)
	}
	if errors.Is(err, brick.ErrGone) {
		t.Fatal("an outage must never read as ErrGone")
	}
}

func TestKeyLayout(t *testing.T) {
	s := New(deadClient(t), Options{Prefix: "app1:"})
	if got := s.stateKey("ab12"); got != "app1:brick:st:ab12" {
		t.Errorf("stateKey = %q", got)
	}
	if got := s.sessionKey("f00d"); got != "app1:brick:sess:f00d" {
		t.Errorf("sessionKey = %q", got)
	}
}

func TestEscapeGlob(t *testing.T) {
	for in, want := range map[string]string{
		"":         "",
		"app1:":    "app1:",
		"a*b?c":    `a\*b\?c`,
		"app[1]:":  `app\[1\]:`,
		`back\sl`:  `back\\sl`,
		"ünïcode:": "ünïcode:",
	} {
		if got := escapeGlob(in); got != want {
			t.Errorf("escapeGlob(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTTLMillis(t *testing.T) {
	for in, want := range map[time.Duration]int64{
		0:                       1,
		time.Microsecond:        1,
		1500 * time.Millisecond: 1500,
		30 * time.Minute:        1_800_000,
	} {
		if got := ttlMillis(in); got != want {
			t.Errorf("ttlMillis(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestNewNeedsAClient(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New(nil) must panic")
		}
	}()
	New(nil, Options{})
}
