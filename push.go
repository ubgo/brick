package brick

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Server push. A stateful component declares the topics it
// listens to (brick: {listen: [orders]}); the page opens one EventSource
// at {AssetBase}events for every topic it rendered, and a Publish of a
// topic makes each listening component run $refresh. An event carries a
// topic name and nothing else: the component refetches through its own
// authorised render, so push never becomes a second, unchecked way to read
// data.
//
// Subscribing is closed by default. A topic name is not a secret, so the
// browser never names one: each listening root carries a listen token, the
// topics it may hear sealed by the engine (with the request's session and
// an expiry) when the component rendered. The events endpoint opens the
// tokens and subscribes to exactly the topics inside; a client can only
// hear what a page the server rendered for it listed.
const (
	// EventsEndpoint is the server push route relative to the asset base,
	// beside ActionEndpoint; runtime.js derives the same URL from the
	// action URL.
	EventsEndpoint = "events"
	// EventsContentType is the endpoint's answer: a text/event-stream of
	// "refresh" events whose data is the topic.
	EventsContentType = "text/event-stream"
	// EventRefresh is the one event the endpoint sends.
	EventRefresh = "refresh"
	// listenTokenParam is the query parameter holding one listen token;
	// it repeats, one per listening component.
	listenTokenParam = "l"
	// listenAD is the additional data a listen token is sealed with. No
	// other sealed blob (a state handle, an island ticket) uses it, so a
	// handle never opens as a listen token, nor a token as a handle.
	listenAD = "brick:listen v1"
	// MaxTopicLen is the longest topic name.
	MaxTopicLen = 200
	// DefaultListenTTL bounds a listen token's age: a stream cannot open
	// with an older one, and an open stream ends when its earliest token
	// expires (after one refresh per topic, so the page renews its tokens).
	DefaultListenTTL = 24 * time.Hour
	// DefaultPushRecheck is how often an open stream re-asks
	// Options.Sessions and PushOptions.AuthorizeTopic, so a session
	// revoked or signed out in a session store, or access withdrawn,
	// stops the stream within this interval even with no RevokeSession.
	DefaultPushRecheck = time.Minute
	// DefaultMaxPushConnections bounds the open event streams per Push
	// (per process): each holds a goroutine and a subscription.
	DefaultMaxPushConnections = 1000
	// DefaultMaxListenTopics bounds the topics one connection subscribes
	// to, over all its tokens.
	DefaultMaxListenTopics = 64
	// DefaultPushHeartbeat is how often an idle stream writes a comment
	// line, so proxies that close quiet connections keep it open and a
	// vanished client is noticed by a failed write.
	DefaultPushHeartbeat = 25 * time.Second
	// DefaultPushRetry is the reconnect delay the stream asks the browser
	// for (the SSE retry: field).
	DefaultPushRetry = 2 * time.Second
	// engineTopicPrefix marks the engine's own control topics. "brick:" is
	// engine-owned everywhere (attributes, tags, and here), so ValidTopic
	// refuses it: no component can listen to, and no action or job can
	// publish, a control topic.
	engineTopicPrefix = "brick:"
	// revokeTopicPrefix + the session hash is the topic RevokeSession
	// publishes; every stream with a session listens to its own.
	revokeTopicPrefix = engineTopicPrefix + "revoke:"
	// memorySubscriberBuffer is how many undelivered topics one memory
	// subscription holds before it drops (see MemoryBroadcaster).
	memorySubscriberBuffer = 64
)

// ErrPushOff is returned by Engine.Publish when Options.Push is nil.
var ErrPushOff = errors.New("brick: server push is off (Options.Push is nil)")

// topicRE is a topic name: letters, digits and _ . : / @ - only, so a topic
// is safe in a Redis channel, a log line and an HTML attribute alike, and a
// space can separate topics in data-brick-listen.
var topicRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:/@-]*$`)

// ValidTopic reports whether s is a topic name Publish and brick: {listen:}
// accept: 1 to MaxTopicLen characters from [A-Za-z0-9_.:/@-], not starting
// with a punctuation character, and not starting with "brick:", which the
// engine owns.
func ValidTopic(s string) bool {
	return wellFormedTopic(s) && !strings.HasPrefix(s, engineTopicPrefix)
}

// ValidBroadcastTopic reports whether a Broadcaster should carry s: a
// ValidTopic or one of the engine's own "brick:" control topics (a
// session revocation). A Broadcaster that filters what it forwards uses
// this, never ValidTopic, or revocations stop at the server they were
// made on.
func ValidBroadcastTopic(s string) bool { return wellFormedTopic(s) }

// wellFormedTopic is the syntax shared by user and control topics.
func wellFormedTopic(s string) bool {
	return len(s) > 0 && len(s) <= MaxTopicLen && topicRE.MatchString(s)
}

// revokeTopic is the control topic that ends the streams of the session
// whose hash is sessionHash.
func revokeTopic(sessionHash string) string { return revokeTopicPrefix + sessionHash }

// Broadcaster carries published topics to every subscriber, in this process
// (NewMemoryBroadcaster) or across processes (contrib/push-redis). Delivery
// is best effort and at most once: a subscriber that lags drops topics, a
// reconnecting browser misses what was published while it was away, and
// the runtime covers both by refreshing every listening component once
// when its stream reconnects. A topic is only a "refresh" signal, so
// nothing is lost but a refresh.
//
// A Broadcaster carries every topic Push hands it, the engine's "brick:"
// control topics included (see ValidBroadcastTopic): RevokeSession
// reaches the other servers that way.
type Broadcaster interface {
	// Publish sends topic to every current subscriber of it.
	Publish(ctx context.Context, topic string) error
	// Subscribe delivers each later Publish of one of topics on the
	// channel until cancel is called or ctx ends; either closes the
	// channel. cancel is safe to call more than once.
	Subscribe(ctx context.Context, topics []string) (ch <-chan string, cancel func(), err error)
}

// MemoryBroadcaster is the in-process Broadcaster: one Publish reaches the
// subscribers of this process only, which is right for one server and for
// tests; several servers behind a balancer need a shared one
// (contrib/push-redis). A subscriber whose buffer is full drops the topic
// rather than block the publisher.
type MemoryBroadcaster struct {
	mu   sync.Mutex
	subs map[string]map[*memorySub]struct{}
}

type memorySub struct {
	ch     chan string
	topics []string
	once   sync.Once
	done   chan struct{}
}

// NewMemoryBroadcaster makes an empty in-process Broadcaster.
func NewMemoryBroadcaster() *MemoryBroadcaster {
	return &MemoryBroadcaster{subs: map[string]map[*memorySub]struct{}{}}
}

// Publish implements Broadcaster.
func (b *MemoryBroadcaster) Publish(ctx context.Context, topic string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs[topic] {
		select {
		case s.ch <- topic:
		default: // a lagging subscriber drops; its stream catches up on reconnect
		}
	}
	return nil
}

// Subscribe implements Broadcaster.
func (b *MemoryBroadcaster) Subscribe(ctx context.Context, topics []string) (<-chan string, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s := &memorySub{ch: make(chan string, memorySubscriberBuffer), topics: slices.Clone(topics), done: make(chan struct{})}
	b.mu.Lock()
	for _, t := range s.topics {
		set := b.subs[t]
		if set == nil {
			set = map[*memorySub]struct{}{}
			b.subs[t] = set
		}
		set[s] = struct{}{}
	}
	b.mu.Unlock()
	cancel := func() {
		s.once.Do(func() {
			b.mu.Lock()
			for _, t := range s.topics {
				delete(b.subs[t], s)
				if len(b.subs[t]) == 0 {
					delete(b.subs, t)
				}
			}
			// Closed under the lock Publish sends under, so no send can
			// race the close.
			close(s.ch)
			b.mu.Unlock()
			close(s.done)
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-s.done:
		}
	}()
	return s.ch, cancel, nil
}

// subscribers counts the live subscriptions, for tests that check cleanup.
func (b *MemoryBroadcaster) subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := map[*memorySub]bool{}
	for _, set := range b.subs {
		for s := range set {
			seen[s] = true
		}
	}
	return len(seen)
}

// PushOptions configures server push. Every field but Broadcaster has a
// default; the zero value of a limit means its default.
type PushOptions struct {
	// Broadcaster carries topics between publishers and streams. Nil means
	// NewMemoryBroadcaster(): this process only.
	Broadcaster Broadcaster
	// Sealer seals listen tokens. Nil means a fresh AES-GCM key made by
	// NewPush, which lives as long as the Push: right for one process,
	// while several servers behind a balancer must share a Sealer (the one
	// their sealed states use, for example), or a token rendered by one
	// is refused by another.
	Sealer Sealer
	// TokenTTL bounds a listen token's age when a stream opens with it.
	// Zero means DefaultListenTTL.
	TokenTTL time.Duration
	// MaxConnections bounds the open streams; one more is answered 503
	// with Retry-After. Zero means DefaultMaxPushConnections.
	MaxConnections int
	// MaxTopics bounds the topics of one stream. Zero means
	// DefaultMaxListenTopics.
	MaxTopics int
	// Heartbeat is the idle comment interval. Zero means
	// DefaultPushHeartbeat.
	Heartbeat time.Duration
	// Retry is the reconnect delay sent to the browser. Zero means
	// DefaultPushRetry.
	Retry time.Duration
	// AuthorizeTopic is a second check at subscribe time, after the token
	// opened: false for any topic refuses the stream with 403. Use it when
	// access can be revoked while a page is open (a user removed from a
	// channel): it is asked again every Recheck, and a false then ends the
	// stream. Nil means the token alone decides.
	AuthorizeTopic func(r *http.Request, topic string) bool
	// Recheck is how often an open stream re-asks Options.Sessions (the
	// session must still answer the same, without an error) and
	// AuthorizeTopic. Zero means DefaultPushRecheck. It runs only when
	// one of the two is set; RevokeSession ends a stream at once.
	Recheck time.Duration
	// Clock replaces time.Now, for tests.
	Clock Clock
	// Presence configures who-is-on-a-topic (presence.go). Its zero value
	// works on one server; several servers set Presence.Store.
	Presence PresenceOptions
}

// Push is server push, shared by every Engine an Options value makes: a
// site's reload builds a new Engine with the same *Push, so the connection
// count and the token key survive it.
type Push struct {
	opts  PushOptions
	conns atomic.Int64
}

// NewPush makes a Push, filling every default.
func NewPush(opts PushOptions) (*Push, error) {
	if opts.Broadcaster == nil {
		opts.Broadcaster = NewMemoryBroadcaster()
	}
	if opts.Sealer == nil {
		key := make([]byte, StateKeySize)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("brick: push key: %w", err)
		}
		ring, err := StaticKeys(key)
		if err != nil {
			return nil, err
		}
		opts.Sealer = NewAESGCMSealer(ring, nil)
	}
	if opts.TokenTTL <= 0 {
		opts.TokenTTL = DefaultListenTTL
	}
	if opts.MaxConnections <= 0 {
		opts.MaxConnections = DefaultMaxPushConnections
	}
	if opts.MaxTopics <= 0 {
		opts.MaxTopics = DefaultMaxListenTopics
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = DefaultPushHeartbeat
	}
	if opts.Retry <= 0 {
		opts.Retry = DefaultPushRetry
	}
	if opts.Recheck <= 0 {
		opts.Recheck = DefaultPushRecheck
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	fillPresence(&opts.Presence, opts.Broadcaster, opts.Clock)
	return &Push{opts: opts}, nil
}

// Publish sends topic to every stream listening to it; an invalid topic is
// an error, never a silent no-op.
func (p *Push) Publish(ctx context.Context, topic string) error {
	if strings.HasPrefix(topic, engineTopicPrefix) {
		return fmt.Errorf("brick: publish: %q: topics starting with %q are the engine's own", topic, engineTopicPrefix)
	}
	if !ValidTopic(topic) {
		return fmt.Errorf("brick: publish: %q is not a topic (1-%d characters from A-Z a-z 0-9 _ . : / @ -)", topic, MaxTopicLen)
	}
	return p.opts.Broadcaster.Publish(ctx, topic)
}

// RevokeSession ends every open event stream of session (the id
// Options.Sessions answers), on every server sharing the Broadcaster: call
// it when the session is signed out or revoked, AFTER ending it in the
// session store. It holds no list of revoked sessions (that would be
// per-process state): a reconnect is refused only because Options.Sessions
// no longer answers that session, and the runtime then gives up with
// push-error. An empty session is a no-op, since a stream without a
// session belongs to no one in particular.
func (p *Push) RevokeSession(ctx context.Context, session string) error {
	if session == "" {
		return nil
	}
	return p.opts.Broadcaster.Publish(ctx, revokeTopic(hashSession(session)))
}

// Connections is the number of open event streams.
func (p *Push) Connections() int { return int(p.conns.Load()) }

// Publish sends topic to every component listening to it, from
// any Go code: a webhook, a job, another request. An action publishes
// with Effects.Publish (brick.publish in Starlark) instead, which waits
// until the action succeeded.
func (e *Engine) Publish(ctx context.Context, topic string) error {
	if e.opts.Push == nil {
		return ErrPushOff
	}
	return e.opts.Push.Publish(ctx, topic)
}

// listenClaims is a listen token's sealed payload.
type listenClaims struct {
	Topics  []string `json:"t"`
	Expires int64    `json:"x"`
	// Session is the hashed session the page was rendered for, "" for
	// none; a stream opened under another session is refused.
	Session string `json:"s,omitempty"`
	// Presence are the presence topics the stream joins and hears changes
	// of (presence.go); a token without them joins nothing.
	Presence []string `json:"p,omitempty"`
}

// listenToken seals topics for the session hash into a token.
func (p *Push) listenToken(ctx context.Context, topics []string, sessionHash string) (string, error) {
	return p.listenTokenWith(ctx, topics, nil, sessionHash)
}

// listenTokenWith seals topics and presence topics for the session hash.
func (p *Push) listenTokenWith(ctx context.Context, topics, presence []string, sessionHash string) (string, error) {
	b, err := json.Marshal(listenClaims{Topics: topics, Presence: presence, Expires: p.opts.Clock().Add(p.opts.TokenTTL).Unix(), Session: sessionHash})
	if err != nil {
		return "", err
	}
	sealed, err := p.opts.Sealer.Seal(ctx, b, []byte(listenAD))
	if err != nil {
		return "", fmt.Errorf("brick: listen token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// openListenToken returns the claims of a token that opens, has not
// expired, belongs to sessionHash and lists only valid topics.
func (p *Push) openListenToken(ctx context.Context, tok, sessionHash string) (*listenClaims, error) {
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		return nil, ErrForbidden
	}
	b, err := p.opts.Sealer.Open(ctx, raw, []byte(listenAD))
	if err != nil {
		return nil, ErrForbidden
	}
	var c listenClaims
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, ErrForbidden
	}
	if p.opts.Clock().Unix() > c.Expires {
		return nil, ErrGone
	}
	if subtle.ConstantTimeCompare([]byte(c.Session), []byte(sessionHash)) != 1 {
		return nil, ErrForbidden
	}
	for _, t := range c.Topics {
		if !ValidTopic(t) {
			return nil, ErrForbidden
		}
	}
	for _, t := range c.Presence {
		if !ValidPresenceTopic(t) {
			return nil, ErrForbidden
		}
	}
	return &c, nil
}

// EventsHandler serves server push: GET {AssetBase}events?l=TOKEN&l=…, a
// text/event-stream (the site layer mounts it beside the action endpoint;
// a Go host mounts or wraps it). Every token must open, be unexpired and
// belong to the request's session (Options.Sessions), and
// PushOptions.AuthorizeTopic, when set, must allow every topic; otherwise
// the answer is 403 (410 for an expired token) and nothing is subscribed.
// Over PushOptions.MaxConnections it answers 503 with Retry-After.
//
// A stream is never trusted for longer than what opened it. It ends, and
// its subscription with it, when the client goes away; when its earliest
// token expires (after one refresh event per topic, so each listening
// component re-renders and the page holds fresh tokens when the runtime
// reconnects); when a recheck finds the session changed or a topic no
// longer authorized (PushOptions.Recheck); and at once on RevokeSession.
func (e *Engine) EventsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		p := e.opts.Push
		if p == nil {
			http.Error(w, ErrPushOff.Error(), http.StatusNotFound)
			return
		}
		ls, err := e.listenTopics(r, p)
		if err != nil {
			switch {
			case errors.Is(err, ErrStoreUnavailable):
				e.reportError(r.Context(), err)
				w.Header().Set("Retry-After", "1")
				http.Error(w, ErrStoreUnavailable.Error(), http.StatusServiceUnavailable)
			case errors.Is(err, ErrGone):
				http.Error(w, "brick: the page's listen token expired; reload the page", http.StatusGone)
			case errors.Is(err, errBadListen):
				http.Error(w, err.Error(), http.StatusBadRequest)
			default:
				http.Error(w, ErrForbidden.Error(), http.StatusForbidden)
			}
			return
		}
		if n := p.conns.Add(1); n > int64(p.opts.MaxConnections) {
			p.conns.Add(-1)
			w.Header().Set("Retry-After", "5")
			http.Error(w, "brick: too many event streams", http.StatusServiceUnavailable)
			return
		}
		defer p.conns.Add(-1)
		e.stream(w, r, p, ls)
	})
}

// errBadListen is a malformed events request: no token, or too many topics.
var errBadListen = errors.New("brick: bad events request")

// listenSet is what an events request was allowed to hear, and the facts a
// stream re-checks while it is open.
type listenSet struct {
	// topics are deduplicated and sorted.
	topics []string
	// expires is the earliest token expiry (Unix seconds): the stream
	// must not outlive it.
	expires int64
	// session is the hashed session the tokens were sealed for, "" for
	// none.
	session string
	// presence are the presence topics the stream joins, deduplicated
	// and sorted; it also hears each one's control topic.
	presence []string
}

// subscribed is every topic the stream subscribes to: its own topics and
// the control topic of each presence topic.
func (ls *listenSet) subscribed() []string {
	out := slices.Clone(ls.topics)
	for _, p := range ls.presence {
		out = append(out, presenceControl(p))
	}
	return out
}

// requestSession returns the hashed session of r, "" without
// Options.Sessions.
func (e *Engine) requestSession(r *http.Request) (string, error) {
	if e.opts.Sessions == nil {
		return "", nil
	}
	s, err := e.opts.Sessions.Session(r)
	if err != nil {
		return "", fmt.Errorf("%w: session: %v", ErrStoreUnavailable, err)
	}
	return hashSession(s), nil
}

// authorized reports whether AuthorizeTopic, when set, allows every topic.
func (p *Push) authorized(r *http.Request, topics []string) bool {
	if p.opts.AuthorizeTopic == nil {
		return true
	}
	for _, t := range topics {
		if !p.opts.AuthorizeTopic(r, t) {
			return false
		}
	}
	return true
}

// listenTopics opens every token of r and returns their topics,
// deduplicated and sorted, with the earliest expiry and the session.
func (e *Engine) listenTopics(r *http.Request, p *Push) (*listenSet, error) {
	toks := r.URL.Query()[listenTokenParam]
	if len(toks) == 0 {
		return nil, fmt.Errorf("%w: no listen token", errBadListen)
	}
	if len(toks) > p.opts.MaxTopics {
		return nil, fmt.Errorf("%w: more than %d listen tokens", errBadListen, p.opts.MaxTopics)
	}
	hash, err := e.requestSession(r)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	pseen := map[string]bool{}
	var topics, presence []string
	var expires int64
	for i, tok := range toks {
		c, err := p.openListenToken(r.Context(), tok, hash)
		if err != nil {
			return nil, err
		}
		if i == 0 || c.Expires < expires {
			expires = c.Expires
		}
		for _, t := range c.Topics {
			if !seen[t] {
				seen[t] = true
				topics = append(topics, t)
			}
		}
		for _, t := range c.Presence {
			if !pseen[t] {
				pseen[t] = true
				presence = append(presence, t)
			}
		}
	}
	if len(topics)+len(presence) > p.opts.MaxTopics {
		return nil, fmt.Errorf("%w: more than %d topics", errBadListen, p.opts.MaxTopics)
	}
	if len(presence) > p.opts.Presence.MaxTopics {
		return nil, fmt.Errorf("%w: more than %d presence topics", errBadListen, p.opts.Presence.MaxTopics)
	}
	// AuthorizeTopic is asked about a presence topic too: hearing who is
	// on a topic is reading that topic.
	if !p.authorized(r, topics) || !p.authorized(r, presence) {
		return nil, ErrForbidden
	}
	slices.Sort(topics)
	slices.Sort(presence)
	return &listenSet{topics: topics, expires: expires, session: hash, presence: presence}, nil
}

// stillAllowed is the recheck of an open stream: the request must still
// answer the session it opened with (an error counts as no: a stream is
// never kept on a session nobody can confirm), and every topic must still
// be authorized.
func (e *Engine) stillAllowed(r *http.Request, p *Push, ls *listenSet) bool {
	hash, err := e.requestSession(r)
	if err != nil {
		e.reportError(r.Context(), err)
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(ls.session)) == 1 && p.authorized(r, ls.topics) && p.authorized(r, ls.presence)
}

// stream writes the event stream until the client leaves, the subscription
// closes, a write fails, the earliest token expires, a recheck fails or
// the session is revoked. A burst is coalesced: every topic already
// waiting is drained first, and each distinct one is written once.
func (e *Engine) stream(w http.ResponseWriter, r *http.Request, p *Push, ls *listenSet) {
	ctx, stop := context.WithCancel(r.Context())
	defer stop()
	unavailable := func(err error) {
		e.reportError(ctx, err)
		w.Header().Set("Retry-After", "1")
		http.Error(w, "brick: the broadcaster is unavailable", http.StatusServiceUnavailable)
	}
	subscribed := ls.subscribed()
	ch, cancel, err := p.opts.Broadcaster.Subscribe(ctx, subscribed)
	if err != nil {
		unavailable(err)
		return
	}
	defer cancel()
	// The revocation has its own subscription, so a burst of topics that
	// fills the topic buffer can never crowd it out. A nil channel (no
	// session) never fires.
	var revoked <-chan string
	if ls.session != "" {
		rch, rcancel, err := p.opts.Broadcaster.Subscribe(ctx, []string{revokeTopic(ls.session)})
		if err != nil {
			unavailable(err)
			return
		}
		defer rcancel()
		revoked = rch
	}
	// A token is valid through its Expires second (openListenToken), so
	// the stream ends at the start of the next one.
	expiry := time.NewTimer(time.Unix(ls.expires+1, 0).Sub(p.opts.Clock()))
	defer expiry.Stop()
	var recheck <-chan time.Time
	if e.opts.Sessions != nil || p.opts.AuthorizeTopic != nil {
		t := time.NewTicker(p.opts.Recheck)
		defer t.Stop()
		recheck = t.C
	}
	rc := http.NewResponseController(w)
	// A long-lived stream outlives a server's WriteTimeout; each write
	// below is short, so the deadline is lifted for this response only.
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", EventsContentType)
	h.Set("X-Accel-Buffering", "no") // nginx: do not buffer the stream
	w.WriteHeader(http.StatusOK)
	write := func(s string) bool {
		if _, err := io.WriteString(w, s); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !write(fmt.Sprintf("retry: %d\n: open\n\n", p.opts.Retry.Milliseconds())) {
		return
	}
	// Presence joins once the stream is subscribed, so this page hears its
	// own join; it leaves on every way out below.
	var renew <-chan time.Time
	j := e.joinPresence(r, p, ls.presence)
	if j != nil && len(j.entries) > 0 {
		defer j.leave(e)
		t := time.NewTicker(p.opts.Presence.TTL / presenceRenewDivisor)
		defer t.Stop()
		renew = t.C
	}
	beat := time.NewTicker(p.opts.Heartbeat)
	defer beat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-expiry.C:
			// One refresh per topic first: each listening component
			// re-renders, which seals it a fresh token, so the runtime's
			// reconnect (refused 410 with the old URL, then retried from
			// the page) opens with tokens that are valid again.
			var b []byte
			for _, t := range subscribed {
				b = fmt.Appendf(b, "event: %s\ndata: %s\n\n", EventRefresh, t)
			}
			write(string(b) + ": expired\n\n")
			return
		case _, ok := <-revoked:
			if ok {
				write(": revoked\n\n")
			}
			return
		case <-recheck:
			if !e.stillAllowed(r, p, ls) {
				write(": revoked\n\n")
				return
			}
		case <-beat.C:
			if !write(": ping\n\n") {
				return
			}
		case <-renew:
			j.renew(ctx, e)
		case t, ok := <-ch:
			if !ok {
				return
			}
			batch := []string{t}
		drain:
			for {
				select {
				case t2, ok := <-ch:
					if !ok {
						break drain
					}
					if !slices.Contains(batch, t2) {
						batch = append(batch, t2)
					}
				default:
					break drain
				}
			}
			var b []byte
			for _, t := range batch {
				b = fmt.Appendf(b, "event: %s\ndata: %s\n\n", EventRefresh, t)
			}
			if !write(string(b)) {
				return
			}
		}
	}
}
