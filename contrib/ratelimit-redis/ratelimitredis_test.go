package ratelimitredis

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
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

// An outage must be an error and a refusal, never an allow: a limiter
// that lets everything through while Redis is down removes the limit
// exactly when nobody is watching.
func TestOutageIsAnErrorNotAnAllow(t *testing.T) {
	l := New(deadClient(t), 5, time.Minute, Options{Prefix: "x:"})
	ok, retry, err := l.Allow(context.Background(), "s:abc")
	if err == nil || ok || retry != 0 {
		t.Fatalf("Allow during an outage = %v %v %v; want false, 0 and an error", ok, retry, err)
	}
}

func TestKeyLayout(t *testing.T) {
	l := New(deadClient(t), 1, time.Second, Options{Prefix: "app1:"})
	if got, want := l.Key("ip:10.0.0.1"), "app1:brick:rl:ip:10.0.0.1"; got != want {
		t.Fatalf("Key = %q, want %q", got, want)
	}
}

func TestNewRejectsMisuse(t *testing.T) {
	c := deadClient(t)
	for name, f := range map[string]func(){
		"nil client":  func() { New(nil, 1, time.Second, Options{}) },
		"zero n":      func() { New(c, 0, time.Second, Options{}) },
		"zero period": func() { New(c, 1, 0, Options{}) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("New did not panic")
				}
			}()
			f()
		})
	}
}
