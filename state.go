package brick

import (
	"container/list"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ubgo/brick/internal/transpile"
)

// Defaults for NewMemoryStates.
const (
	DefaultMaxStates = 10_000
	DefaultStateTTL  = 30 * time.Minute
	// fnStateAttrs writes a stateful component's handle and token on its
	// root element; injected by the engine, never written by authors.
	fnStateAttrs = "__stateAttrs"
	// secretBytes is the size of a handle and of a token: 128 bits each.
	secretBytes = 16
	// DefaultMaxActionBytes bounds an action request's JSON when
	// Options.MaxActionBytes is zero.
	DefaultMaxActionBytes = 1 << 20
	// ActionEndpoint is the action route's name relative to the asset base:
	// actions are served at Options.AssetBase + ActionEndpoint ("/_brick/action"
	// by default). It is relative, not a full path, because the asset base
	// is configurable and a site may sit under a base path (/ui/_brick/action).
	// runtime/runtime.js posts to the same name beside its own script URL;
	// TestRuntimeActionEndpoint keeps the two in step.
	ActionEndpoint = "action"
	// actionHeader must be present on an action request; a cross-site form
	// cannot set it, so it doubles as CSRF protection with the token.
	actionHeader = "Brick-Action"
)

// DefaultStoreName is the name Options.States is registered under; a
// component without brick: {store:} uses it.
const DefaultStoreName = "default"

// RecoverHeader marks the runtime's recovery GET: the
// page is rendered as for any visit; the response varies on it and is
// never shared.
const RecoverHeader = "Brick-Recover"

// devStateWarnBytes is the encoded size above which Dev warns that a
// component's state belongs in a server store.
const devStateWarnBytes = 16 << 10

// Instance is one rendered stateful component, as a state store keeps it
// . Every field is set by the engine; a
// store only keeps it. Everything in Props, Ctx and State is
// JSON-compatible, so any store can encode it (EncodeInstance).
type Instance struct {
	// Handle is the store's key for this instance, as its Put returned it
	// (without the store-name prefix the page carries). Token must
	// accompany every action.
	Handle string
	Token  string
	// Store is the name of the store holding it ("default", "redis", …).
	Store string
	// Path is the component file, Name the tag as written, ID its instance
	// id in the page.
	Path string
	Name string
	ID   string
	// Props are the call site's props; Ctx the .Ctx it rendered under,
	// minus request-scoped values (Provide), which are re-provided per
	// action from the live request and never stored.
	Props  map[string]any
	Ctx    map[string]any
	Locale string
	// State is the current state.
	State map[string]any
	// Keys are the state: keys declared when the instance was made, so a
	// deploy that changes the declaration can migrate it (4.2).
	Keys []string
	// Touched are the field paths the user has sent ($set, deferred sets)
	// or an action validated, sorted. Only a component
	// with rules: records them; its errors are computed from them at every
	// render, so no error map is ever stored.
	Touched []string
	// Pending are the presigned upload ids issued to this instance and not
	// yet confirmed: $uploaded accepts no other id, so
	// a client cannot claim an object it was not issued.
	Pending []PendingUpload
	// Session is the hex SHA-256 of the session id it was rendered under,
	// "" when none (4.6).
	Session string
	// Version is 0 at render and +1 per applied action.
	Version int64
	// Issued is the first render's time.
	Issued time.Time
	// PagePath is the page it was rendered on, for .Page.
	PagePath string
	// Key is the call site's key=, formatted, or "" — what scopes its
	// remembered fields across pages.
	Key string
	// Route and RouteParts are the page's dynamic route values, so an action's re-render sees the same .Page.Route the first
	// render did; nil on a static page.
	Route      map[string]string
	RouteParts []string
	// Slots are the caller's children ("children") and <brick:prop> blocks
	// (by name), rendered to HTML once at the call site. An
	// action re-renders the component after the caller's template and data
	// are gone, and its own action cannot change what the caller wrote, so
	// the stored HTML is exact; only the caller's next render replaces it.
	// It is sealed and size-capped with the rest of the instance.
	Slots map[string]string
}

// StateStore keeps stateful components' instances between actions. It
// must be safe for concurrent use.
type StateStore interface {
	// Put saves inst and returns the handle the page carries for it. A
	// server store returns a stable key (it may keep inst.Handle when set);
	// a sealing store returns the whole sealed instance, so the handle
	// changes on every Put.
	Put(ctx context.Context, inst *Instance) (handle string, err error)
	// Get returns the instance for a handle; ok=false when it is missing,
	// expired or does not open. An error means the store itself failed
	// (the action answers 503, never 410). A store with expiry refreshes
	// it here.
	Get(ctx context.Context, handle string) (*Instance, bool, error)
	// Delete forgets one instance. A store that cannot (sealed) returns nil.
	Delete(ctx context.Context, handle string) error
}

// Revoker ends every instance bound to a session at once — sign-out, role
// change, "log out other devices". session is the hashed id, as in
// Instance.Session. Engine.RevokeSession calls it on every store that
// implements it; one returning errors.ErrUnsupported is skipped.
type Revoker interface {
	RevokeSession(ctx context.Context, session string) error
}

// Lister walks live instances, for an inspector and for operators. Order
// is unspecified; fn returning false stops.
type Lister interface {
	Each(ctx context.Context, fn func(*Instance) bool) error
}

// Pruner deletes expired instances in a store that does not expire them
// itself (SQL). The engine starts no goroutine; the site layer calls it on
// a timer.
type Pruner interface {
	Prune(ctx context.Context, now time.Time) (removed int, err error)
}

// StateScope is what the engine tells a store about the call it is
// making, through the context: the store's registered name, the hashed
// live session, and whether this is a static export. A store may bind a
// value to them (SealedStates puts Store and Session in its additional
// data); most ignore it.
type StateScope struct {
	Store   string
	Session string
	// NoExpiry marks a static export's render (WithStaticState): its blobs
	// hold only initial state anyone could load, so they never expire; the
	// first action re-seals with a TTL.
	NoExpiry bool
}

type stateScopeKey struct{}

// StateScopeFrom returns the scope the engine set on ctx (zero when none).
func StateScopeFrom(ctx context.Context) StateScope {
	s, _ := ctx.Value(stateScopeKey{}).(StateScope)
	return s
}

// WithStateScope sets the scope on ctx. The engine does this on every
// store call; a store's own tests use it to act as the engine.
func WithStateScope(ctx context.Context, s StateScope) context.Context {
	return context.WithValue(ctx, stateScopeKey{}, s)
}

// MemoryStates is an in-process StateStore: bounded, least recently used
// evicted first, each instance expiring TTL after its last use. A restart
// loses everything, which the runtime recovers in place; several servers
// need sticky routing. It sees every way an instance ends, so it is an
// EndWatcher for all three reasons, and a Pruner, so the site's prune
// timer removes (and reports) expired instances nobody looks up again.
type MemoryStates struct {
	mu    sync.Mutex
	max   int
	ttl   time.Duration
	order *list.List // front = most recently used
	items map[string]*list.Element
	now   Clock
	rand  io.Reader
	ends  EndFunc
}

// memEnded is an instance the store removed under its lock, reported after
// the lock is released (an end hook may call the store).
type memEnded struct {
	inst   *Instance
	reason EndReason
}

var (
	_ EndWatcher = (*MemoryStates)(nil)
	_ Pruner     = (*MemoryStates)(nil)
)

type memEntry struct {
	inst    *Instance
	expires time.Time
}

// NewMemoryStates returns an in-process store; zero arguments mean the
// defaults (10,000 instances, 30 minutes).
func NewMemoryStates(max int, ttl time.Duration) *MemoryStates {
	if max <= 0 {
		max = DefaultMaxStates
	}
	if ttl <= 0 {
		ttl = DefaultStateTTL
	}
	return &MemoryStates{max: max, ttl: ttl, order: list.New(), items: map[string]*list.Element{}, now: time.Now}
}

// SetClock replaces time.Now — for tests of expiry only.
func (m *MemoryStates) SetClock(now Clock) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = now
}

// SetRand replaces crypto/rand for handles — for tests only.
func (m *MemoryStates) SetRand(r io.Reader) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rand = r
}

// copyInstance copies an instance deeply enough that neither the store
// nor the caller can change the other's maps.
func copyInstance(inst *Instance) *Instance {
	cp := *inst
	cp.Props, _ = deepCopy(inst.Props).(map[string]any)
	cp.Ctx, _ = deepCopy(inst.Ctx).(map[string]any)
	cp.State, _ = deepCopy(inst.State).(map[string]any)
	cp.Keys = slices.Clone(inst.Keys)
	cp.Touched = slices.Clone(inst.Touched)
	cp.Pending = slices.Clone(inst.Pending)
	cp.Slots = maps.Clone(inst.Slots)
	cp.Route = maps.Clone(inst.Route)
	cp.RouteParts = slices.Clone(inst.RouteParts)
	return &cp
}

// Get returns a copy of the instance and refreshes its expiry.
func (m *MemoryStates) Get(ctx context.Context, handle string) (*Instance, bool, error) {
	m.mu.Lock()
	el, ok := m.items[handle]
	if !ok {
		m.mu.Unlock()
		return nil, false, nil
	}
	e, _ := el.Value.(*memEntry)
	if m.now().After(e.expires) {
		m.order.Remove(el)
		delete(m.items, handle)
		m.unlockAndReport(ctx, []memEnded{{e.inst, EndExpired}})
		return nil, false, nil
	}
	e.expires = m.now().Add(m.ttl)
	m.order.MoveToFront(el)
	cp := copyInstance(e.inst)
	m.mu.Unlock()
	return cp, true, nil
}

// Put stores a copy of the instance under its handle (a new random one
// when it has none), evicting the least recently used beyond the bound.
func (m *MemoryStates) Put(ctx context.Context, inst *Instance) (string, error) {
	handle := inst.Handle
	if handle == "" {
		m.mu.Lock()
		r := m.rand
		m.mu.Unlock()
		h, err := randomHex(r)
		if err != nil {
			return "", err
		}
		handle = h
	}
	cp := copyInstance(inst)
	cp.Handle = handle
	m.mu.Lock()
	now := m.now()
	e := &memEntry{inst: cp, expires: now.Add(m.ttl)}
	if el, ok := m.items[handle]; ok {
		el.Value = e
		m.order.MoveToFront(el)
	} else {
		m.items[handle] = m.order.PushFront(e)
	}
	var ended []memEnded
	for m.order.Len() > m.max {
		last := m.order.Back()
		le, _ := last.Value.(*memEntry)
		delete(m.items, le.inst.Handle)
		m.order.Remove(last)
		// The least recently used entry may have expired already; say so
		// rather than blame the bound.
		reason := EndEvicted
		if now.After(le.expires) {
			reason = EndExpired
		}
		ended = append(ended, memEnded{le.inst, reason})
	}
	m.unlockAndReport(ctx, ended)
	return handle, nil
}

// Delete removes an instance.
func (m *MemoryStates) Delete(_ context.Context, handle string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[handle]; ok {
		m.order.Remove(el)
		delete(m.items, handle)
	}
	return nil
}

// RevokeSession removes every instance bound to the hashed session.
func (m *MemoryStates) RevokeSession(ctx context.Context, session string) error {
	if session == "" {
		return nil
	}
	m.mu.Lock()
	var ended []memEnded
	for h, el := range m.items {
		if e, _ := el.Value.(*memEntry); e.inst.Session == session {
			m.order.Remove(el)
			delete(m.items, h)
			ended = append(ended, memEnded{e.inst, EndRevoked})
		}
	}
	m.unlockAndReport(ctx, ended)
	return nil
}

// Prune removes every instance expired at now and reports each as
// EndExpired. Without it an expired instance nobody looks up again stays
// in memory until the size bound pushes it out, and its end hook fires
// only then; the site layer calls it every PruneEvery.
func (m *MemoryStates) Prune(ctx context.Context, now time.Time) (int, error) {
	m.mu.Lock()
	var ended []memEnded
	for h, el := range m.items {
		if e, _ := el.Value.(*memEntry); now.After(e.expires) {
			m.order.Remove(el)
			delete(m.items, h)
			ended = append(ended, memEnded{e.inst, EndExpired})
		}
	}
	m.unlockAndReport(ctx, ended)
	return len(ended), nil
}

// WatchEnds installs fn; the memory store reports every reason.
func (m *MemoryStates) WatchEnds(fn EndFunc) []EndReason {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ends = fn
	return slices.Clone(allReasons)
}

// unlockAndReport releases m.mu (held by the caller) and then tells the
// watcher, so an end hook may call the store. The instances are already
// out of the map, so no other call can see or report them again.
func (m *MemoryStates) unlockAndReport(ctx context.Context, ended []memEnded) {
	fn := m.ends
	m.mu.Unlock()
	if fn == nil {
		return
	}
	for _, x := range ended {
		fn(ctx, x.inst, x.reason)
	}
}

// Each calls fn with a copy of every live instance, most recently used
// first, until fn returns false.
func (m *MemoryStates) Each(_ context.Context, fn func(*Instance) bool) error {
	m.mu.Lock()
	var all []*Instance
	now := m.now()
	for el := m.order.Front(); el != nil; el = el.Next() {
		if e, _ := el.Value.(*memEntry); !now.After(e.expires) {
			all = append(all, copyInstance(e.inst))
		}
	}
	m.mu.Unlock()
	for _, inst := range all {
		if !fn(inst) {
			break
		}
	}
	return nil
}

// Len reports how many instances are held.
func (m *MemoryStates) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.items)
}

// randomHex returns secretBytes of randomness as hex, from r (crypto/rand
// when nil).
func randomHex(r io.Reader) (string, error) {
	if r == nil {
		r = rand.Reader
	}
	b := make([]byte, secretBytes)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashSession is how a session id is kept and compared: never the id
// itself, so a store dump does not hand out sessions.
func hashSession(id string) string {
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// WithSession names the session a render belongs to:
// stateful components rendered under it are bound to it, and an action from
// another session finds them "not found". The site layer passes
// Options.Sessions' answer per response; engine-only hosts pass it
// themselves. Empty means no session.
func WithSession(id string) RenderOption {
	return func(o *renderOptions) { o.session = id }
}

// WithStaticState marks a static export's render: sealed state is written
// with no expiry.
func WithStaticState() RenderOption {
	return func(o *renderOptions) { o.staticState = true }
}

// buildStores registers Options.States as "default" and the named stores,
// giving byte and sealed stores the engine's codec when they have none.
func buildStores(opts *Options) (map[string]StateStore, error) {
	stores := map[string]StateStore{}
	if opts.States != nil {
		stores[DefaultStoreName] = opts.States
	}
	for _, name := range sortedKeys(opts.StateStores) {
		st := opts.StateStores[name]
		switch {
		case name == DefaultStoreName:
			return nil, fmt.Errorf("brick: Options.StateStores: %q is Options.States' name; pick another", name)
		case !validStoreName(name):
			return nil, fmt.Errorf("brick: Options.StateStores: %q is not a store name (letters, digits, - and _)", name)
		case st == nil:
			return nil, fmt.Errorf("brick: Options.StateStores: %q is nil", name)
		}
		stores[name] = st
	}
	for name, st := range stores {
		switch x := st.(type) {
		case *SealedStates:
			stores[name] = x.withCodec(opts.Codec)
		case *BytesStates:
			stores[name] = x.withCodec(opts.Codec)
		}
	}
	return stores, nil
}

// validStoreName keeps a name usable as the handle's prefix: no dot, which
// separates it from the handle.
func validStoreName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// storeNames lists the registered stores for messages.
func (e *Engine) storeNames() string {
	if len(e.stores) == 0 {
		return "none; set Options.States"
	}
	return strings.Join(sortedKeys(e.stores), ", ")
}

// checkStoreName is the compile check of brick: {store: name}: an unknown
// name is an error listing the names the engine has, never a silent
// fallback to another store.
func (e *Engine) checkStoreName(m *meta) error {
	if m == nil || m.store == "" {
		return nil
	}
	if _, ok := e.stores[m.store]; !ok {
		return fmt.Errorf("brick: store: %q is not a state store of this engine (have: %s)", m.store, e.storeNames())
	}
	return nil
}

// newInstance creates a stateful component's instance at render (or
// reuses the one an action is re-rendering). The store's Put, which makes
// the handle, runs in putInstance.
func (st *renderState) newInstance(callee *compiled, call *transpile.Call, props map[string]any) (*Instance, error) {
	if st.resume != nil {
		inst := st.resume
		st.resume = nil
		return inst, nil
	}
	name := callee.meta.store
	if name == "" {
		name = DefaultStoreName
	}
	if _, ok := st.e.stores[name]; !ok {
		if name == DefaultStoreName {
			return nil, fmt.Errorf("<%s> declares state:, which needs Options.States (brick.NewMemoryStates for one process, brick.NewSealedStates to survive restarts)", call.Name)
		}
		return nil, fmt.Errorf("<%s> uses state store %q, which this engine does not have (have: %s)", call.Name, name, st.e.storeNames())
	}
	token, err := randomHex(st.e.opts.Rand)
	if err != nil {
		return nil, err
	}
	state, _ := deepCopy(callee.meta.state).(map[string]any)
	pagePath := ""
	var route map[string]string
	var parts []string
	if st.page != nil {
		pagePath, route, parts = st.page.Path, maps.Clone(st.page.Route), slices.Clone(st.page.RouteParts)
	}
	return &Instance{
		Token: token, Store: name, Path: callee.path, Name: call.Name,
		Props: props, Ctx: st.storedCtx(), Locale: st.locale, State: state,
		Keys: sortedKeys(callee.meta.state), Session: hashSession(st.session),
		Issued: st.e.opts.Clock().UTC(), PagePath: pagePath, Route: route, RouteParts: parts,
	}, nil
}

// storedCtx is the .Ctx an instance keeps: every value from files
// (provide:, {{ provide }}), none from the request (Provide), which an
// action gets again from the live request. A file value
// that shadows a request value is kept.
func (st *renderState) storedCtx() map[string]any {
	out := map[string]any{}
	req := map[string]bool{}
	for _, f := range st.ctxStack {
		for k, v := range f.vals {
			out[k] = v
			req[k] = f.req[k]
		}
	}
	for k, r := range req {
		if r {
			delete(out, k)
		}
	}
	return out
}

// putInstance saves an instance in its store and records the handle.
// Values JSON cannot carry fail here, at render, naming the component —
// except in the memory store, which needs no encoding: there Dev warns, so
// switching stores later never surprises anyone.
func (e *Engine) putInstance(ctx context.Context, inst *Instance, noExpiry bool) error {
	store := e.stores[inst.Store]
	if store == nil {
		return fmt.Errorf("%w: state store %q is gone", ErrGone, inst.Store)
	}
	_, memory := store.(*MemoryStates)
	live := inst
	inst = e.withoutTemporary(inst)
	defer func() { live.Handle = inst.Handle }()
	if err := checkInstanceJSON(inst); err != nil {
		err = fmt.Errorf("<%s>: %w", inst.Name, err)
		if !memory {
			return err
		}
		if e.opts.Mode == Dev {
			e.warn(Diagnostic{File: inst.Path, Code: CodeRuntime, Message: err.Error() + " (fine in the memory store; a shared or sealed store would refuse it)"})
		}
	} else if e.opts.Mode == Dev {
		if b, err := e.stateCodec().Encode(inst); err == nil && len(b) > devStateWarnBytes {
			e.warn(Diagnostic{File: inst.Path, Code: CodeRuntime, Message: fmt.Sprintf("<%s> holds %d KiB of state; keep ids in state and load rows in the action, or give it brick: {store: <a server store>}", inst.Name, len(b)>>10)})
		}
	}
	handle, err := store.Put(WithStateScope(ctx, StateScope{Store: inst.Store, Session: inst.Session, NoExpiry: noExpiry}), inst)
	if err != nil {
		if memory || isEngineError(err) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	inst.Handle = handle
	return nil
}

// withoutTemporary is the instance as it is saved: brick: {temporary:}
// fields back at their declared value. The live instance
// keeps the value the action set, so this render shows it; the next action
// starts from the declared value. An instance without temporary fields is
// saved as is.
func (e *Engine) withoutTemporary(inst *Instance) *Instance {
	m, err := e.calleeMeta(inst.Path)
	if err != nil || len(m.temporary) == 0 {
		return inst
	}
	cp := *inst
	cp.State = maps.Clone(inst.State)
	for _, f := range m.temporary {
		cp.State[f] = deepCopy(m.state[f])
	}
	return &cp
}

// parseTemporary reads brick: {temporary: [field…]}.
func parseTemporary(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("brick: temporary: must list state fields ([rows])")
	}
	out := make([]string, 0, len(list))
	for _, x := range list {
		s, ok := x.(string)
		if !ok || s == "" {
			return nil, errors.New("brick: temporary: must list state fields ([rows])")
		}
		out = append(out, s)
	}
	return out, nil
}

// checkTemporary is the parse-time check of brick: {temporary:}: it needs
// state:, and each entry must be a declared top-level field, since the
// declared value is what the field is reset to.
func checkTemporary(m *meta) error {
	if len(m.temporary) == 0 {
		return nil
	}
	if m.state == nil {
		return errors.New("brick: temporary: needs state: — it resets state fields")
	}
	for _, f := range m.temporary {
		if _, ok := m.state[f]; !ok {
			return fmt.Errorf("brick: temporary: %s is not a top-level field of state: (declared: %s)", f, strings.Join(sortedKeys(m.state), ", "))
		}
	}
	return nil
}

// isEngineError reports a Put error that is the instance's fault (it
// cannot be encoded or is too large), not the store being down.
func isEngineError(err error) bool { return errors.Is(err, ErrStateValue) }

func (e *Engine) stateCodec() Codec {
	if e.opts.Codec != nil {
		return e.opts.Codec
	}
	return JSONCodec{}
}

// HasState reports whether any island in the result is a stateful
// component: its filled response carries a live handle and must not be
// shared by a cache.
func (r *Result) HasState() bool {
	for _, c := range r.Chunks {
		if c.Kind == ChunkIsland && c.Island != nil && c.Island.Stateful {
			return true
		}
	}
	return false
}

// RevokeSession ends every instance bound to the session id in every store
// that implements Revoker. It also ends the session's
// open server-push streams on every server sharing the push Broadcaster
// (Push.RevokeSession). Sealed state cannot be
// revoked this way: rotate the session id on sign-out, or use
// SealedOptions.Revoked.
func (e *Engine) RevokeSession(ctx context.Context, id string) error {
	h := hashSession(id)
	if h == "" {
		return nil
	}
	var errs []error
	for _, name := range sortedKeys(e.stores) {
		r, ok := e.stores[name].(Revoker)
		if !ok {
			continue
		}
		if err := r.RevokeSession(WithStateScope(ctx, StateScope{Store: name, Session: h}), h); err != nil && !errors.Is(err, errors.ErrUnsupported) {
			errs = append(errs, fmt.Errorf("store %s: %w", name, err))
		}
	}
	if e.opts.Push != nil {
		if err := e.opts.Push.RevokeSession(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("push: %w", err))
		}
	}
	return errors.Join(errs...)
}

// stateAttrs writes the root element's handle and token (and the instance
// id, unless the client attributes already do).
func stateAttrs(s *Scope) (template.HTMLAttr, error) {
	if s.inst == nil || s.inst.state == nil {
		return "", nil
	}
	inst := s.inst.state
	// A stateful root is wired only by the runtime. Recording it here, where
	// the root is actually written, also covers a component chosen by
	// brick:render at fill time, which no static walk of the page can see.
	if s.st != nil {
		s.st.needRuntime = true
	}
	id := ""
	if len(s.file.client) == 0 {
		id = fmt.Sprintf(`data-brick=%q `, s.ID)
	}
	// The handle is the store name, a dot and the store's handle (hex, or
	// base64url for a sealed blob); the token and id are hex. None of them
	// needs escaping in a quoted attribute.
	// data-brick-name and data-brick-on are what the runtime delivers
	// events by.
	on, err := listenerAttrs(s)
	if err != nil {
		return "", err
	}
	// data-brick-arrays names the list-valued paths, so the runtime binds a
	// checkbox group as a list even with one option.
	// It reads the instance's state, which has every declared key.
	// data-brick-query lets Back and Forward restore queried fields from
	// the URL in place.
	q, err := queryAttr(s.file.meta)
	if err != nil {
		return "", err
	}
	on += q
	if paths := arrayPaths(inst.State); len(paths) > 0 {
		on += ` data-brick-arrays="` + template.HTMLEscapeString(strings.Join(paths, " ")) + `"`
	}
	// data-brick-uploads tells the runtime each upload field's limits, for
	// a fast advisory error before a byte is sent, and whether the sink
	// presigns. The server checks every limit
	// again; this is never the gate.
	if len(s.file.meta.uploads) > 0 && s.st != nil {
		b, err := uploadsAttr(s.file.meta.uploads, s.st.e.opts.Uploads)
		if err != nil {
			return "", err
		}
		on += ` data-brick-uploads="` + template.HTMLEscapeString(b) + `"`
	}
	// data-brick-listen and its token are the topics the root refreshes on
	// and the sealed proof the page listed them.
	l, err := listenAttrs(s)
	if err != nil {
		return "", err
	}
	on += l
	// data-brick-errors is the render's .Errors, for brick.errors(el) in
	// client code. Written only when there are
	// errors, so a root without any keeps its bytes; encoding/json sorts
	// the keys, so the same errors write the same attribute.
	if len(s.Errors) > 0 {
		b, err := json.Marshal(s.Errors)
		if err != nil {
			return "", fmt.Errorf("brick: .Errors: %w", err)
		}
		on += ` data-brick-errors="` + template.HTMLEscapeString(string(b)) + `"`
	}
	return template.HTMLAttr(id + `data-brick-state=` + quoteGo(inst.Store+"."+inst.Handle) + ` data-brick-token=` + quoteGo(inst.Token) + on), nil
}

// quoteGo is fmt's %q for a string, without fmt: a sealed handle is a few
// KiB of base64url on every stateful root, and %q escapes it rune by rune.
// Printable ASCII other than " and \ is written as is, exactly as %q
// writes it; anything else takes strconv.Quote, which is what %q calls, so
// the bytes never differ from %q's.
func quoteGo(s string) string {
	for i := range len(s) {
		if c := s[i]; c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			return strconv.Quote(s)
		}
	}
	return `"` + s + `"`
}
