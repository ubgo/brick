package pushredis

import (
	"context"
	"net"
	"strings"
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

// An unreachable Redis is an error from New, never a broadcaster that
// silently delivers nothing.
func TestNewFailsWithoutRedis(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if b, err := New(ctx, deadClient(t), Options{Prefix: "x:"}); err == nil {
		_ = b.Close()
		t.Fatal("New succeeded without a Redis")
	}
}

func TestChannelLayoutAndGlob(t *testing.T) {
	b := &Broadcaster{prefix: "app1:" + channelSegment}
	if got, want := b.Channel("order:42"), "app1:brick:push:order:42"; got != want {
		t.Errorf("Channel = %q, want %q", got, want)
	}
	if got, want := escapeGlob(`a*b?[c]\`), `a\*b\?\[c\]\\`; got != want {
		t.Errorf("escapeGlob = %q, want %q", got, want)
	}
}

func TestNewPanicsOnNilClient(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New(nil) did not panic")
		}
	}()
	_, _ = New(context.Background(), nil, Options{})
}

// Publish accepts the engine's "brick:" control topics (a revocation must
// cross servers) and still refuses a name that is no topic at all; the
// check runs before any Redis call, so the dead client never matters.
func TestPublishAcceptsControlTopics(t *testing.T) {
	b := &Broadcaster{c: deadClient(t), prefix: "x:" + channelSegment}
	if err := b.Publish(context.Background(), "bad topic"); err == nil || !strings.Contains(err.Error(), "not a topic") {
		t.Fatalf("bad topic: %v", err)
	}
	if err := b.Publish(context.Background(), "brick:revoke:abc"); err == nil || strings.Contains(err.Error(), "not a topic") {
		t.Fatalf("control topic refused as a name: %v", err)
	}
}
