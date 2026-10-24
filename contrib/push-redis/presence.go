package pushredis

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ubgo/brick"
)

// presenceSegment is part of the wire format, like channelSegment: one
// hash per presence topic at <prefix>brick:presence:<topic>, field = the
// connection id, value = presenceValue as JSON.
const presenceSegment = "brick:presence:"

// presenceKeyTTLFactor keeps a topic's hash alive this many entry TTLs past
// its last join or renewal, so a topic whose every server died is deleted
// by Redis itself and never lingers as an empty key.
const presenceKeyTTLFactor = 2

// presenceValue is one connection's stored entry with its own expiry:
// Redis expires keys, not hash fields on every version, so each field
// carries its deadline and List drops the ones past it.
type presenceValue struct {
	ID      string            `json:"id"`
	Data    map[string]string `json:"d,omitempty"`
	Expires int64             `json:"x"` // Unix milliseconds
}

// PresenceStore is a brick.PresenceStore on Redis hashes, shared by every
// server on the same prefix: a viewer whose stream opened on server A is
// listed by a render on server B. An entry whose server died without a
// leave lapses after its TTL. Safe for concurrent use.
//
//	store := pushredis.NewPresenceStore(rdb, pushredis.Options{Prefix: "app1:"})
//	push, err := brick.NewPush(brick.PushOptions{Broadcaster: b, Sealer: sealer,
//		Presence: brick.PresenceOptions{Store: store}})
type PresenceStore struct {
	c      redis.UniversalClient
	prefix string
	clock  func() time.Time
}

var _ brick.PresenceStore = (*PresenceStore)(nil)

// NewPresenceStore makes a store on c under opts.Prefix (the Broadcaster's
// prefix, normally). A nil client is a programming error, so it panics.
func NewPresenceStore(c redis.UniversalClient, opts Options) *PresenceStore {
	if c == nil {
		panic("pushredis: NewPresenceStore needs a Redis client")
	}
	return &PresenceStore{c: c, prefix: opts.Prefix + presenceSegment, clock: time.Now}
}

// Key is the Redis hash holding topic's entries.
func (s *PresenceStore) Key(topic string) string { return s.prefix + topic }

// joinScript enforces the cap and writes the entry in one step, so two
// servers joining at once can never both take the last place: an existing
// field (a renewal) always succeeds, a new one only below max live fields.
// Expired fields are removed first so they never hold a place.
var joinScript = redis.NewScript(`
local key, conn, val, now, max, keyttl = KEYS[1], ARGV[1], ARGV[2], tonumber(ARGV[3]), tonumber(ARGV[4]), tonumber(ARGV[5])
if redis.call('HEXISTS', key, conn) == 0 then
  local all = redis.call('HGETALL', key)
  local live = 0
  for i = 1, #all, 2 do
    local ok, v = pcall(cjson.decode, all[i+1])
    if ok and type(v) == 'table' and tonumber(v.x) and tonumber(v.x) > now then
      live = live + 1
    else
      redis.call('HDEL', key, all[i])
    end
  end
  if live >= max then return 0 end
end
redis.call('HSET', key, conn, val)
redis.call('PEXPIRE', key, keyttl)
return 1
`)

// Join implements brick.PresenceStore.
func (s *PresenceStore) Join(ctx context.Context, topic, conn string, e brick.PresenceEntry, ttl time.Duration, max int) error {
	now := s.clock()
	b, err := json.Marshal(presenceValue{ID: e.ID, Data: e.Data, Expires: now.Add(ttl).UnixMilli()})
	if err != nil {
		return err
	}
	n, err := joinScript.Run(ctx, s.c, []string{s.Key(topic)}, conn, string(b), now.UnixMilli(), max, (ttl * presenceKeyTTLFactor).Milliseconds()).Int()
	if err != nil {
		return fmt.Errorf("pushredis: presence join: %w", err)
	}
	if n == 0 {
		return brick.ErrPresenceFull
	}
	return nil
}

// Leave implements brick.PresenceStore.
func (s *PresenceStore) Leave(ctx context.Context, topic, conn string) error {
	if err := s.c.HDel(ctx, s.Key(topic), conn).Err(); err != nil {
		return fmt.Errorf("pushredis: presence leave: %w", err)
	}
	return nil
}

// List implements brick.PresenceStore. A field that does not decode or has
// expired is skipped (and deleted, best effort), never an error: one bad
// field must not hide everyone else.
func (s *PresenceStore) List(ctx context.Context, topic string) ([]brick.PresenceEntry, error) {
	all, err := s.c.HGetAll(ctx, s.Key(topic)).Result()
	if err != nil {
		return nil, fmt.Errorf("pushredis: presence list: %w", err)
	}
	now := s.clock().UnixMilli()
	out := make([]brick.PresenceEntry, 0, len(all))
	var stale []string
	conns := make([]string, 0, len(all))
	for conn := range all {
		conns = append(conns, conn)
	}
	slices.Sort(conns) // a stable order, so the same viewers render the same bytes
	for _, conn := range conns {
		raw := all[conn]
		var v presenceValue
		if json.Unmarshal([]byte(raw), &v) != nil || v.Expires <= now {
			stale = append(stale, conn)
			continue
		}
		out = append(out, brick.PresenceEntry{ID: v.ID, Data: v.Data})
	}
	if len(stale) > 0 {
		_ = s.c.HDel(ctx, s.Key(topic), stale...).Err()
	}
	return out, nil
}
