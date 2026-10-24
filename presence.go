package brick

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Presence: who is on a topic right now, built on server push.
// A stateful component declares the topics it shows presence for,
//
//	brick: {listen: {presence: "room:{{ .Props.id }}"}}
//
// and renders the list with {{ .Presence "room:7" }}. Being present means
// having an open event stream whose tokens list that presence topic: the
// stream joins when it opens, renews while it stays open and leaves when it
// closes, and each join or leave publishes the engine-owned control topic
// "brick:presence:<topic>", which every presence component on that topic
// hears as an ordinary refresh. The runtime needs nothing new: the control
// topic is written into data-brick-listen beside the component's own.
//
// Who someone is stays the host's choice. PresenceOptions.Identify turns
// the stream's request into the entry other viewers see; without it an
// entry is an opaque random id per connection with no data, so nothing
// personal leaves the server unless the host puts it there.
//
// Presence is closed exactly like push: a stream joins, and hears changes
// of, only the presence topics sealed into its own tokens, and a render
// reads only the presence topics its component declared.
const (
	// presenceTopicPrefix + a topic is the control topic a join or leave
	// publishes. "brick:" is engine-owned, so no component listens to it
	// by name and no Publish can fake it.
	presenceTopicPrefix = engineTopicPrefix + "presence:"
	// MaxPresenceTopicLen is the longest presence topic: its control topic
	// must still be a well-formed broadcast topic (MaxTopicLen), or a
	// Broadcaster such as contrib/push-redis would drop it.
	MaxPresenceTopicLen = MaxTopicLen - len(presenceTopicPrefix)
	// DefaultPresenceTTL is how long an entry outlives its last renewal.
	// An open stream renews every TTL/presenceRenewDivisor, so an entry
	// disappears this long after its server vanished without a leave.
	DefaultPresenceTTL = 90 * time.Second
	// presenceRenewDivisor sets the renewal interval as a fraction of the
	// TTL: three tries before an entry lapses, so one slow store round
	// trip never makes a present viewer blink out.
	presenceRenewDivisor = 3
	// DefaultMaxPresenceEntries bounds the connections one topic lists in
	// the store; a join beyond it is refused (ErrPresenceFull) and that
	// stream keeps working, unlisted.
	DefaultMaxPresenceEntries = 1000
	// DefaultPresenceListMax bounds the entries one {{ .Presence }} returns
	// (PresenceList.Total still counts them all).
	DefaultPresenceListMax = 100
	// DefaultMaxPresenceTopics bounds the presence topics one stream joins.
	DefaultMaxPresenceTopics = 8
	// MaxPresenceIDLen bounds an entry's ID.
	MaxPresenceIDLen = 200
	// MaxPresenceDataKeys bounds an entry's Data keys.
	MaxPresenceDataKeys = 16
	// MaxPresenceDataLen bounds each Data key and value, in bytes.
	MaxPresenceDataLen = 256
	// presenceLeaveTimeout bounds the leave a closing stream does after
	// its request context ended.
	presenceLeaveTimeout = 5 * time.Second
	// presenceConnIDBytes is the randomness of a connection id, the
	// default entry ID: unguessable and unrelated to the viewer.
	presenceConnIDBytes = 16
)

// ErrPresenceFull is returned by PresenceStore.Join when the topic already
// lists max connections.
var ErrPresenceFull = errors.New("brick: presence: topic is full")

// PresenceEntry is one viewer on a topic as other viewers see it.
type PresenceEntry struct {
	// ID identifies the viewer: entries with the same ID are one viewer
	// with several connections (tabs). The host's Identify chooses it; by
	// default it is a random id per connection.
	ID string `json:"id"`
	// Data is what the host chose to show (a display name, an avatar
	// URL), never filled by the engine.
	Data map[string]string `json:"d,omitempty"`
	// Conns is how many open connections the viewer has on the topic; set
	// by .Presence, ignored by a store.
	Conns int `json:"-"`
}

// PresenceList is what {{ .Presence "topic" }} returns.
type PresenceList struct {
	// Entries are the viewers, one per ID, sorted by ID (so the same
	// viewers render the same bytes), at most PresenceOptions.MaxList.
	Entries []PresenceEntry
	// Total is the number of distinct viewers, Entries' length or more.
	Total int
}

// PresenceStore keeps who is on which topic. MemoryPresenceStore keeps it
// in this process; several servers need a shared one (contrib/push-redis
// NewPresenceStore). Every method is keyed by an opaque connection id.
type PresenceStore interface {
	// Join lists conn on topic until ttl from now, replacing its entry;
	// calling it again renews. A topic already listing max other
	// connections answers ErrPresenceFull.
	Join(ctx context.Context, topic, conn string, e PresenceEntry, ttl time.Duration, max int) error
	// Leave removes conn from topic; an absent conn is not an error.
	Leave(ctx context.Context, topic, conn string) error
	// List returns the unexpired entries of topic, one per connection, in
	// any order.
	List(ctx context.Context, topic string) ([]PresenceEntry, error)
}

// PresenceOptions configures presence; it lives in PushOptions.Presence.
// Every field has a default.
type PresenceOptions struct {
	// Store keeps the entries. Nil means a MemoryPresenceStore when the
	// Broadcaster is the in-process one; with any other Broadcaster (the
	// servers are several) nil leaves presence off and a component that
	// declares it fails to render, so a per-process list never passes for
	// the whole site.
	Store PresenceStore
	// Identify turns the stream's request into the entry shown for it on
	// topic; false keeps this connection off topic's list (the stream
	// still works). An empty ID means the connection id. Nil means every
	// connection is an anonymous entry: a random ID and no Data.
	Identify func(r *http.Request, topic string) (PresenceEntry, bool)
	// TTL is how long an entry outlives its last renewal. Zero means
	// DefaultPresenceTTL.
	TTL time.Duration
	// MaxEntries bounds the connections one topic lists. Zero means
	// DefaultMaxPresenceEntries.
	MaxEntries int
	// MaxList bounds the entries one {{ .Presence }} returns. Zero means
	// DefaultPresenceListMax.
	MaxList int
	// MaxTopics bounds the presence topics one stream joins. Zero means
	// DefaultMaxPresenceTopics.
	MaxTopics int
}

// errPresenceStore is the render error of a presence component when the
// Push has no store (several servers, no shared store configured).
var errPresenceStore = errors.New("brick: presence: the Broadcaster is not the in-process one, so presence needs a shared PushOptions.Presence.Store (contrib/push-redis NewPresenceStore); a per-process list would show only this server's viewers")

// fillPresence sets the defaults of o; b is the Push's Broadcaster.
func fillPresence(o *PresenceOptions, b Broadcaster, clock Clock) {
	if o.Store == nil {
		if _, ok := b.(*MemoryBroadcaster); ok {
			o.Store = NewMemoryPresenceStore(clock)
		}
	}
	if o.TTL <= 0 {
		o.TTL = DefaultPresenceTTL
	}
	if o.MaxEntries <= 0 {
		o.MaxEntries = DefaultMaxPresenceEntries
	}
	if o.MaxList <= 0 {
		o.MaxList = DefaultPresenceListMax
	}
	if o.MaxTopics <= 0 {
		o.MaxTopics = DefaultMaxPresenceTopics
	}
}

// ValidPresenceTopic reports whether s may be a presence topic: a
// ValidTopic short enough that its control topic is one too.
func ValidPresenceTopic(s string) bool {
	return ValidTopic(s) && len(s) <= MaxPresenceTopicLen
}

// presenceControl is the control topic a change of topic's presence
// publishes, and the name a presence component listens to.
func presenceControl(topic string) string { return presenceTopicPrefix + topic }

// checkPresenceEntry enforces the size limits on what Identify returned.
func checkPresenceEntry(e PresenceEntry) error {
	if len(e.ID) > MaxPresenceIDLen {
		return fmt.Errorf("brick: presence: ID is %d bytes, over %d", len(e.ID), MaxPresenceIDLen)
	}
	if len(e.Data) > MaxPresenceDataKeys {
		return fmt.Errorf("brick: presence: %d Data keys, over %d", len(e.Data), MaxPresenceDataKeys)
	}
	for k, v := range e.Data {
		if len(k) > MaxPresenceDataLen || len(v) > MaxPresenceDataLen {
			return fmt.Errorf("brick: presence: Data %q is over %d bytes", k, MaxPresenceDataLen)
		}
	}
	return nil
}

// newConnID is a random connection id.
func newConnID() (string, error) {
	b := make([]byte, presenceConnIDBytes)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("brick: presence: connection id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// presenceJoin is one stream's membership: the topics it joined, renewed
// on a ticker and left when the stream ends.
type presenceJoin struct {
	p       *Push
	conn    string
	entries map[string]PresenceEntry // topic → entry, joined topics only
}

// joinPresence joins every presence topic of ls that Identify lets this
// connection join, publishing each join. A store error is reported and
// that topic skipped: presence is best effort and never ends a stream.
func (e *Engine) joinPresence(r *http.Request, p *Push, topics []string) *presenceJoin {
	po := &p.opts.Presence
	if len(topics) == 0 || po.Store == nil {
		return nil
	}
	conn, err := newConnID()
	if err != nil {
		e.reportError(r.Context(), err)
		return nil
	}
	j := &presenceJoin{p: p, conn: conn, entries: map[string]PresenceEntry{}}
	for _, t := range topics {
		ent := PresenceEntry{}
		if po.Identify != nil {
			var ok bool
			ent, ok = po.Identify(r, t)
			if !ok {
				continue
			}
			if err := checkPresenceEntry(ent); err != nil {
				e.reportError(r.Context(), err)
				continue
			}
		}
		if ent.ID == "" {
			ent.ID = conn
		}
		ent.Conns = 0
		ent.Data = cloneData(ent.Data)
		if err := po.Store.Join(r.Context(), t, conn, ent, po.TTL, po.MaxEntries); err != nil {
			if !errors.Is(err, ErrPresenceFull) {
				e.reportError(r.Context(), fmt.Errorf("brick: presence: join %q: %w", t, err))
			}
			continue
		}
		j.entries[t] = ent
		if err := p.opts.Broadcaster.Publish(r.Context(), presenceControl(t)); err != nil {
			e.reportError(r.Context(), err)
		}
	}
	return j
}

// renew extends every joined entry by the TTL.
func (j *presenceJoin) renew(ctx context.Context, e *Engine) {
	po := &j.p.opts.Presence
	for t, ent := range j.entries {
		if err := po.Store.Join(ctx, t, j.conn, ent, po.TTL, po.MaxEntries); err != nil {
			e.reportError(ctx, fmt.Errorf("brick: presence: renew %q: %w", t, err))
		}
	}
}

// leave removes every joined entry and publishes each leave. It runs after
// the request ended, so it has its own short deadline.
func (j *presenceJoin) leave(e *Engine) {
	ctx, cancel := context.WithTimeout(context.Background(), presenceLeaveTimeout)
	defer cancel()
	for t := range j.entries {
		if err := j.p.opts.Presence.Store.Leave(ctx, t, j.conn); err != nil {
			e.reportError(ctx, fmt.Errorf("brick: presence: leave %q: %w", t, err))
		}
		if err := j.p.opts.Broadcaster.Publish(ctx, presenceControl(t)); err != nil {
			e.reportError(ctx, err)
		}
	}
}

func cloneData(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Presence returns who is on topic: one entry per viewer ID, sorted by ID,
// capped at PresenceOptions.MaxList. topic must be one of the presence
// topics this component declares (brick: {listen: {presence: ...}}), as
// rendered now; anything else is an error, so a component can never read
// the presence of a topic its own token does not carry.
func (s *Scope) Presence(topic string) (*PresenceList, error) {
	if s.file == nil || s.file.meta.listen == nil || s.st == nil {
		return nil, fmt.Errorf("brick: .Presence %q: the component declares no presence (brick: {listen: {presence: ...}})", topic)
	}
	p := s.st.e.opts.Push
	if p == nil {
		return nil, ErrPushOff
	}
	declared, err := s.presenceTopics()
	if err != nil {
		return nil, err
	}
	if !slices.Contains(declared, topic) {
		return nil, fmt.Errorf("brick: .Presence %q: not a presence topic of this component (it declares %s)", topic, strings.Join(quoteAll(declared), ", "))
	}
	if p.opts.Presence.Store == nil {
		return nil, errPresenceStore
	}
	raw, err := p.opts.Presence.Store.List(s.st.ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("brick: .Presence %q: %w", topic, err)
	}
	return aggregatePresence(raw, p.opts.Presence.MaxList), nil
}

// aggregatePresence folds per-connection entries into one per ID (the
// first Data seen in connection order wins, which is stable for a store
// that lists in a stable order), sorts by ID and caps at max.
func aggregatePresence(raw []PresenceEntry, max int) *PresenceList {
	byID := map[string]*PresenceEntry{}
	ids := []string{}
	for _, r := range raw {
		if cur, ok := byID[r.ID]; ok {
			cur.Conns++
			continue
		}
		ent := PresenceEntry{ID: r.ID, Data: cloneData(r.Data), Conns: 1}
		byID[r.ID] = &ent
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	out := &PresenceList{Total: len(ids)}
	for i, id := range ids {
		if i == max {
			break
		}
		out.Entries = append(out.Entries, *byID[id])
	}
	return out
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// MemoryPresenceStore is the in-process PresenceStore: right for one
// server and for tests. Expired entries are dropped when read.
type MemoryPresenceStore struct {
	mu     sync.Mutex
	clock  Clock
	topics map[string]map[string]memoryPresence // topic → conn → entry
}

type memoryPresence struct {
	e       PresenceEntry
	expires time.Time
}

// NewMemoryPresenceStore makes an empty store; a nil clock means time.Now.
func NewMemoryPresenceStore(clock Clock) *MemoryPresenceStore {
	if clock == nil {
		clock = time.Now
	}
	return &MemoryPresenceStore{clock: clock, topics: map[string]map[string]memoryPresence{}}
}

// prune drops topic's expired entries; the caller holds mu.
func (m *MemoryPresenceStore) prune(topic string, now time.Time) {
	set := m.topics[topic]
	for c, p := range set {
		if !now.Before(p.expires) {
			delete(set, c)
		}
	}
	if len(set) == 0 {
		delete(m.topics, topic)
	}
}

// Join implements PresenceStore.
func (m *MemoryPresenceStore) Join(ctx context.Context, topic, conn string, e PresenceEntry, ttl time.Duration, max int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	m.prune(topic, now)
	set := m.topics[topic]
	if _, ok := set[conn]; !ok && len(set) >= max {
		return ErrPresenceFull
	}
	if set == nil {
		set = map[string]memoryPresence{}
		m.topics[topic] = set
	}
	e.Conns = 0
	e.Data = cloneData(e.Data)
	set[conn] = memoryPresence{e: e, expires: now.Add(ttl)}
	return nil
}

// Leave implements PresenceStore.
func (m *MemoryPresenceStore) Leave(ctx context.Context, topic, conn string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.topics[topic], conn)
	if len(m.topics[topic]) == 0 {
		delete(m.topics, topic)
	}
	return nil
}

// List implements PresenceStore, ordered by connection id.
func (m *MemoryPresenceStore) List(ctx context.Context, topic string) ([]PresenceEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(topic, m.clock())
	set := m.topics[topic]
	conns := make([]string, 0, len(set))
	for c := range set {
		conns = append(conns, c)
	}
	slices.Sort(conns)
	out := make([]PresenceEntry, 0, len(conns))
	for _, c := range conns {
		e := set[c].e
		e.Data = cloneData(e.Data)
		out = append(out, e)
	}
	return out, nil
}
