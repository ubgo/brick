// Package pushredis is a brick.Broadcaster on Redis pub/sub (go-redis v9):
// a topic published on any server reaches the event streams of every
// server, so server push works behind a load balancer.
//
//	b, err := pushredis.New(ctx, rdb, pushredis.Options{Prefix: "app1:"})
//	defer b.Close()
//	push, err := brick.NewPush(brick.PushOptions{Broadcaster: b, Sealer: sealer})
//
// The host builds the client (a *redis.Client, *redis.ClusterClient or
// *redis.Ring); this package never opens a connection of its own beyond
// the one pub/sub connection go-redis takes for the subscription.
//
// Layout: each topic is published on the channel <prefix>brick:push:<topic>
// with an empty message (a topic is only a refresh signal). Each
// Broadcaster holds ONE pattern subscription, <prefix>brick:push:*, and
// fans every message out to its local streams through a
// brick.MemoryBroadcaster, so a thousand open streams cost one Redis
// connection, not a thousand. Every server therefore receives every
// topic under its prefix; that is the trade-off for one connection, and
// it is cheap while publishes are signals, not data.
//
// Revocation: brick.Push.RevokeSession (and Engine.RevokeSession) publishes
// an engine-owned "brick:revoke:<session hash>" topic through the
// Broadcaster, so a sign-out on one server ends that session's open
// streams on every server sharing the prefix, over the same subscription.
//
// Outages: Publish returns Redis's error. A dropped subscription is
// re-established by go-redis; topics published meanwhile are lost, which
// the browser runtime covers by refreshing its listeners when its own
// stream reconnects (brick.Broadcaster documents the contract).
package pushredis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
	"github.com/ubgo/brick"
)

// channelSegment is part of the wire format: two deploys sharing a Redis
// must agree on it, and an operator greps for it (redis-cli PSUBSCRIBE).
const channelSegment = "brick:push:"

// Options configures New.
type Options struct {
	// Prefix goes in front of every channel, so several apps (or test
	// runs) can share one Redis. Empty means no prefix. End it with a
	// separator such as ":"; it is used as given.
	Prefix string
}

// Broadcaster is a brick.Broadcaster over Redis pub/sub. Safe for
// concurrent use.
type Broadcaster struct {
	c      redis.UniversalClient
	prefix string // Options.Prefix + channelSegment
	local  *brick.MemoryBroadcaster
	ps     *redis.PubSub
	done   chan struct{}
	once   sync.Once
}

var _ brick.Broadcaster = (*Broadcaster)(nil)

// New subscribes to the prefix's topics and starts fanning them out; it
// returns once Redis confirmed the subscription, so a Publish right after
// New is not missed. Close ends it. A nil client is a programming error,
// so it panics.
func New(ctx context.Context, c redis.UniversalClient, opts Options) (*Broadcaster, error) {
	if c == nil {
		panic("pushredis: New needs a Redis client")
	}
	b := &Broadcaster{c: c, prefix: opts.Prefix + channelSegment, local: brick.NewMemoryBroadcaster(), done: make(chan struct{})}
	b.ps = c.PSubscribe(ctx, escapeGlob(b.prefix)+"*")
	if _, err := b.ps.Receive(ctx); err != nil {
		_ = b.ps.Close()
		return nil, fmt.Errorf("pushredis: subscribe: %w", err)
	}
	ch := b.ps.Channel()
	go func() {
		defer close(b.done)
		for m := range ch {
			topic, ok := strings.CutPrefix(m.Channel, b.prefix)
			// ValidBroadcastTopic, not ValidTopic: the engine's own
			// "brick:" topics (a session revocation) must reach every
			// server's streams too.
			if !ok || !brick.ValidBroadcastTopic(topic) {
				continue // not ours, or not a topic: never forwarded
			}
			_ = b.local.Publish(context.Background(), topic)
		}
	}()
	return b, nil
}

// Channel is the Redis channel a topic is published on.
func (b *Broadcaster) Channel(topic string) string { return b.prefix + topic }

// Publish implements brick.Broadcaster: it reaches every server's
// subscribers, this one's included, through Redis.
func (b *Broadcaster) Publish(ctx context.Context, topic string) error {
	if !brick.ValidBroadcastTopic(topic) {
		return fmt.Errorf("pushredis: %q is not a topic", topic)
	}
	return b.c.Publish(ctx, b.Channel(topic), "").Err()
}

// Subscribe implements brick.Broadcaster, from the local fan-out.
func (b *Broadcaster) Subscribe(ctx context.Context, topics []string) (<-chan string, func(), error) {
	select {
	case <-b.done:
		return nil, nil, errClosed
	default:
	}
	return b.local.Subscribe(ctx, topics)
}

var errClosed = errors.New("pushredis: the broadcaster is closed")

// Close ends the Redis subscription; later Subscribes fail. Streams
// already open stop receiving topics and end when their clients leave.
func (b *Broadcaster) Close() error {
	var err error
	b.once.Do(func() {
		err = b.ps.Close()
		<-b.done
	})
	return err
}

// escapeGlob escapes the PSUBSCRIBE pattern characters in a literal prefix.
func escapeGlob(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '*', '?', '[', ']', '\\':
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
