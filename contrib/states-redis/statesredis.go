// Package statesredis is a brick.BytesStore on Redis (go-redis v9), so
// component state survives a restart and is shared by every server behind
// a load balancer. Wrap it with brick.Bytes, which
// makes the handles and owns the TTL, the codec and any encryption at
// rest; this package only moves bytes.
//
//	store := brick.Bytes(statesredis.New(rdb, statesredis.Options{Prefix: "app1:"}),
//		brick.BytesOptions{TTL: 30 * time.Minute})
//
// The host builds the client (a *redis.Client, *redis.ClusterClient or
// *redis.Ring); this package never opens a connection, so it has no
// opinion about addresses, passwords or TLS.
//
// Layout, every key under Options.Prefix:
//
//	<prefix>brick:st:<handle>     hash {d: bytes, t: TTL in ms, s: session}, PEXPIRE = TTL
//	<prefix>brick:sess:<session>  set of handles saved under that session
//
// The TTL is stored beside the bytes because Load has no TTL argument yet
// must refresh the expiry (a component in use stays alive). Every
// multi-step write touches one key per command, never two keys in one
// script or transaction, so the store works unchanged on Redis Cluster,
// where two keys rarely share a slot.
//
// Instance ends (brick.BytesEndWatcher): the store reports revoked only.
// Redis deletes an expired key itself and the value goes with it, so there
// is nothing left to report. Keyspace notifications were considered and
// left out: they need notify-keyspace-events set on the server (a host may
// not be allowed to, and a shared Redis must not be reconfigured for one
// app), they carry only the key name (no component, no state), and they
// are pub/sub, so an expiry while no server listens is lost and one heard
// by several servers is reported by each. A host that needs expiry
// reports uses a store that can see them (memory, SQL).
//
// Outages: every Redis failure is returned as an error, never as "not
// found". The engine maps an error to 503 and a miss to 410, and a 410
// makes the page recover the component, so an outage reported as a miss
// would reset every open tab at once.
package statesredis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ubgo/brick"
)

// Key layout. The names are part of the stored format: an operator greps
// for them, and two deploys sharing a Redis must agree on them.
const (
	stateSegment   = "brick:st:"
	sessionSegment = "brick:sess:"
	fieldData      = "d"
	fieldTTL       = "t"
	fieldSession   = "s"
)

// scanCount is the COUNT hint for SCAN in EachBytes and the batch size of
// RevokeSession: large enough that a listing is a handful of round trips,
// small enough that one batch never blocks Redis noticeably.
const scanCount = 256

// Options configures New.
type Options struct {
	// Prefix goes in front of every key, so several apps (or test runs)
	// can share one Redis. Empty means no prefix. End it with a separator
	// such as ":"; it is used as given.
	Prefix string
}

// Store is a brick.BytesStore on Redis. It is also a brick.Revoker, a
// brick.BytesLister and a brick.BytesEndWatcher (revoked only), so
// brick.Bytes over it is a Revoker, a Lister and an EndWatcher. It is not
// a Pruner: Redis expires keys itself. Safe for concurrent use.
type Store struct {
	c      redis.UniversalClient
	prefix string
	ends   atomic.Pointer[brick.BytesEndFunc]
}

var (
	_ brick.BytesStore      = (*Store)(nil)
	_ brick.BytesLister     = (*Store)(nil)
	_ brick.Revoker         = (*Store)(nil)
	_ brick.BytesEndWatcher = (*Store)(nil)
)

// New returns a store over c. A nil client is a programming error, so it
// panics here rather than failing every later call.
func New(c redis.UniversalClient, opts Options) *Store {
	if c == nil {
		panic("statesredis: New needs a Redis client")
	}
	return &Store{c: c, prefix: opts.Prefix}
}

func (s *Store) stateKey(handle string) string    { return s.prefix + stateSegment + handle }
func (s *Store) sessionKey(session string) string { return s.prefix + sessionSegment + session }

// saveScript writes the hash and its expiry in one step, so a failure can
// never leave a value that lives forever.
var saveScript = redis.NewScript(`
redis.call('HSET', KEYS[1], '` + fieldData + `', ARGV[1], '` + fieldTTL + `', ARGV[2], '` + fieldSession + `', ARGV[3])
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1`)

// loadScript reads the value and refreshes its expiry with the TTL saved
// beside it, in one step. A missing key returns nil (redis.Nil).
var loadScript = redis.NewScript(`
local v = redis.call('HMGET', KEYS[1], '` + fieldData + `', '` + fieldTTL + `', '` + fieldSession + `')
if not v[1] then return false end
if v[2] then redis.call('PEXPIRE', KEYS[1], v[2]) end
return {v[1], v[2] or '', v[3] or ''}`)

// revokeScript deletes one instance only if it is still bound to the
// session being revoked: the session set may hold a handle that was
// removed, or saved again under another session, and revoking one session
// must never end another's component. It returns the value it deleted
// (nil when it deleted nothing), read and deleted in one step, so when two
// servers revoke at once exactly one of them gets it to report.
var revokeScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], '` + fieldSession + `') == ARGV[1] then
	local d = redis.call('HGET', KEYS[1], '` + fieldData + `')
	redis.call('DEL', KEYS[1])
	return d
end
return false`)

// ttlMillis is the TTL as Redis wants it, at least 1ms: PEXPIRE 0 would
// delete the key at once.
func ttlMillis(ttl time.Duration) int64 { return max(ttl.Milliseconds(), 1) }

// Save stores data under key for ttl. With a session, the handle joins the
// session's set first, so a RevokeSession running at the same time can
// never miss a value that is already readable.
func (s *Store) Save(ctx context.Context, key string, data []byte, ttl time.Duration, session string) error {
	ms := ttlMillis(ttl)
	if session != "" {
		if err := s.touchSession(ctx, session, key, ms); err != nil {
			return err
		}
	}
	if err := saveScript.Run(ctx, s.c, []string{s.stateKey(key)}, data, ms, session).Err(); err != nil {
		return fmt.Errorf("statesredis: save: %w", err)
	}
	return nil
}

// touchSession adds handle (when not empty) to the session's set and
// pushes the set's expiry to ms from now. Every instance uses the same
// TTL, so the set then outlives each member it holds.
func (s *Store) touchSession(ctx context.Context, session, handle string, ms int64) error {
	sk := s.sessionKey(session)
	_, err := s.c.Pipelined(ctx, func(p redis.Pipeliner) error {
		if handle != "" {
			p.SAdd(ctx, sk, handle)
		}
		p.PExpire(ctx, sk, time.Duration(ms)*time.Millisecond)
		return nil
	})
	if err != nil {
		return fmt.Errorf("statesredis: session index: %w", err)
	}
	return nil
}

// Load returns the bytes under key and refreshes its expiry (and its
// session set's). A missing or expired key is (nil, false, nil); any Redis
// failure is an error.
func (s *Store) Load(ctx context.Context, key string) ([]byte, bool, error) {
	res, err := loadScript.Run(ctx, s.c, []string{s.stateKey(key)}).StringSlice()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("statesredis: load: %w", err)
	}
	if len(res) != 3 {
		return nil, false, fmt.Errorf("statesredis: load: unexpected reply of %d items", len(res))
	}
	data, ttl, session := res[0], res[1], res[2]
	if session != "" && ttl != "" {
		ms, err := strconv.ParseInt(ttl, 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("statesredis: load: stored TTL %q: %w", ttl, err)
		}
		if err := s.touchSession(ctx, session, "", ms); err != nil {
			return nil, false, err
		}
	}
	return []byte(data), true, nil
}

// Remove deletes key. Its handle stays in the session set until that set
// expires or is revoked; revokeScript ignores it then.
func (s *Store) Remove(ctx context.Context, key string) error {
	if err := s.c.Del(ctx, s.stateKey(key)).Err(); err != nil {
		return fmt.Errorf("statesredis: remove: %w", err)
	}
	return nil
}

// RevokeSession deletes every instance bound to session. It removes from
// the set only the handles it processed, so a handle a concurrent Save
// adds is never dropped from the index unrevoked.
func (s *Store) RevokeSession(ctx context.Context, session string) error {
	if session == "" {
		return nil
	}
	sk := s.sessionKey(session)
	var cursor uint64
	for {
		handles, next, err := s.c.SScan(ctx, sk, cursor, "", scanCount).Result()
		if err != nil {
			return fmt.Errorf("statesredis: revoke: %w", err)
		}
		if err := s.revokeBatch(ctx, sk, session, handles); err != nil {
			return err
		}
		if cursor = next; cursor == 0 {
			return nil
		}
	}
}

func (s *Store) revokeBatch(ctx context.Context, sk, session string, handles []string) error {
	if len(handles) == 0 {
		return nil
	}
	cmds := make([]*redis.Cmd, len(handles))
	_, err := s.c.Pipelined(ctx, func(p redis.Pipeliner) error {
		for i, h := range handles {
			// Eval, not EvalSha: a pipeline cannot fall back on NOSCRIPT.
			cmds[i] = revokeScript.Eval(ctx, p, []string{s.stateKey(h)}, session)
		}
		members := make([]any, len(handles))
		for i, h := range handles {
			members[i] = h
		}
		p.SRem(ctx, sk, members...)
		return nil
	})
	// A handle that was no longer the session's answers nil, which a
	// pipeline reports as its error; only a real failure is one.
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("statesredis: revoke: %w", err)
	}
	if fn := s.ends.Load(); fn != nil {
		for i, c := range cmds {
			if data, err := c.Text(); err == nil {
				(*fn)(ctx, handles[i], []byte(data), brick.EndRevoked)
			}
		}
	}
	return nil
}

// WatchEndsBytes installs fn (nil removes it). Redis reports revoked only:
// see the package comment for why expiry is not observable.
func (s *Store) WatchEndsBytes(fn brick.BytesEndFunc) []brick.EndReason {
	if fn == nil {
		s.ends.Store(nil)
	} else {
		s.ends.Store(&fn)
	}
	return []brick.EndReason{brick.EndRevoked}
}

// EachBytes walks every live instance under the prefix with SCAN, on every
// master of a cluster or every shard of a ring. fn is never called
// concurrently; returning false stops the walk. A key that expires between
// SCAN and its read is skipped.
func (s *Store) EachBytes(ctx context.Context, fn func(key string, data []byte) bool) error {
	w := &walker{s: s, fn: fn}
	var err error
	switch c := s.c.(type) {
	case *redis.ClusterClient:
		err = c.ForEachMaster(ctx, func(ctx context.Context, node *redis.Client) error { return w.scan(ctx, node) })
	case *redis.Ring:
		err = c.ForEachShard(ctx, func(ctx context.Context, node *redis.Client) error { return w.scan(ctx, node) })
	default:
		err = w.scan(ctx, s.c)
	}
	if err != nil {
		return fmt.Errorf("statesredis: list: %w", err)
	}
	return nil
}

// walker serializes fn across the nodes a cluster or ring scans in
// parallel, and stops every node once fn returns false.
type walker struct {
	s       *Store
	fn      func(string, []byte) bool
	mu      sync.Mutex
	stopped bool
}

func (w *walker) scan(ctx context.Context, c redis.UniversalClient) error {
	head := w.s.prefix + stateSegment
	pattern := escapeGlob(head) + "*"
	var cursor uint64
	for {
		keys, next, err := c.Scan(ctx, cursor, pattern, scanCount).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			cmds := make([]*redis.StringCmd, len(keys))
			_, err := c.Pipelined(ctx, func(p redis.Pipeliner) error {
				for i, k := range keys {
					cmds[i] = p.HGet(ctx, k, fieldData)
				}
				return nil
			})
			if err != nil && !errors.Is(err, redis.Nil) {
				return err
			}
			if !w.deliver(head, keys, cmds) {
				return nil
			}
		}
		if cursor = next; cursor == 0 {
			return nil
		}
	}
}

// deliver hands one SCAN page to fn; false means stop.
func (w *walker) deliver(head string, keys []string, cmds []*redis.StringCmd) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, k := range keys {
		if w.stopped {
			return false
		}
		data, err := cmds[i].Bytes()
		if err != nil {
			continue // expired between SCAN and HGET
		}
		if !w.fn(strings.TrimPrefix(k, head), data) {
			w.stopped = true
		}
	}
	return !w.stopped
}

// escapeGlob quotes the characters SCAN MATCH treats as a pattern, so a
// prefix such as "app[1]:" matches only itself.
func escapeGlob(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`*?[]\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
