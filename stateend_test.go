package brick

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// endRecorder is an InstanceEndHook that keeps what it was told.
type endRecorder struct {
	mu   sync.Mutex
	ends []InstanceEnd
}

func (r *endRecorder) OnInstanceEnd(_ context.Context, e InstanceEnd) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, e)
}

func (r *endRecorder) got() []InstanceEnd {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.ends)
}

// memClock is a settable clock for MemoryStates.
type memClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *memClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *memClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newMemClock() *memClock { return &memClock{t: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)} }

type memEnd struct {
	handle string
	reason EndReason
}

// watchMem installs a watcher on m that records (handle, reason) and, to
// prove reports run after the lock is released, calls the store back.
func watchMem(m *MemoryStates) func() []memEnd {
	var mu sync.Mutex
	var seen []memEnd
	m.WatchEnds(func(_ context.Context, inst *Instance, r EndReason) {
		_ = m.Len() // deadlocks if the store reports under its lock
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, memEnd{inst.Handle, r})
	})
	return func() []memEnd {
		mu.Lock()
		defer mu.Unlock()
		out := slices.Clone(seen)
		slices.SortFunc(out, func(a, b memEnd) int { return strings.Compare(a.handle, b.handle) })
		return out
	}
}

// The memory store sees every way an instance ends and reports each with
// the right reason.
func TestMemoryStatesEnds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	put := func(t *testing.T, m *MemoryStates, handle, session string) {
		t.Helper()
		if _, err := m.Put(ctx, &Instance{Handle: handle, Name: "C", Session: session}); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("Reasons", func(t *testing.T) {
		if got := NewMemoryStates(0, 0).WatchEnds(nil); !slices.Equal(got, allReasons) {
			t.Fatalf("memory reports every reason: %v", got)
		}
	})
	t.Run("ExpiredAtGet", func(t *testing.T) {
		clock := newMemClock()
		m := NewMemoryStates(0, time.Minute)
		m.SetClock(clock.now)
		seen := watchMem(m)
		put(t, m, "a", "")
		clock.add(2 * time.Minute)
		if _, ok, _ := m.Get(ctx, "a"); ok {
			t.Fatal("expired must be gone")
		}
		if got := seen(); !slices.Equal(got, []memEnd{{"a", EndExpired}}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("ExpiredAtPrune", func(t *testing.T) {
		clock := newMemClock()
		m := NewMemoryStates(0, time.Minute)
		m.SetClock(clock.now)
		seen := watchMem(m)
		put(t, m, "a", "")
		put(t, m, "b", "")
		clock.add(2 * time.Minute)
		put(t, m, "fresh", "")
		n, err := m.Prune(ctx, clock.now())
		if err != nil || n != 2 || m.Len() != 1 {
			t.Fatalf("Prune = %d, %v; left %d", n, err, m.Len())
		}
		if got := seen(); !slices.Equal(got, []memEnd{{"a", EndExpired}, {"b", EndExpired}}) {
			t.Fatalf("got %v", got)
		}
		if n, _ := m.Prune(ctx, clock.now()); n != 0 || len(seen()) != 2 {
			t.Fatal("a second Prune must find and report nothing")
		}
	})
	t.Run("EvictedByTheBound", func(t *testing.T) {
		clock := newMemClock()
		m := NewMemoryStates(2, time.Minute)
		m.SetClock(clock.now)
		seen := watchMem(m)
		put(t, m, "a", "")
		put(t, m, "b", "")
		put(t, m, "c", "")
		if got := seen(); !slices.Equal(got, []memEnd{{"a", EndEvicted}}) {
			t.Fatalf("a live instance pushed out is evicted: %v", got)
		}
		clock.add(2 * time.Minute)
		put(t, m, "d", "")
		if got := seen(); !slices.Equal(got, []memEnd{{"a", EndEvicted}, {"b", EndExpired}}) {
			t.Fatalf("an already expired instance pushed out is expired: %v", got)
		}
	})
	t.Run("RevokedThatSessionOnly", func(t *testing.T) {
		m := NewMemoryStates(0, 0)
		seen := watchMem(m)
		put(t, m, "a1", "sa")
		put(t, m, "a2", "sa")
		put(t, m, "b1", "sb")
		if err := m.RevokeSession(ctx, "sa"); err != nil {
			t.Fatal(err)
		}
		if got := seen(); !slices.Equal(got, []memEnd{{"a1", EndRevoked}, {"a2", EndRevoked}}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("DeleteAndNoWatcherReportNothing", func(t *testing.T) {
		m := NewMemoryStates(0, 0)
		seen := watchMem(m)
		put(t, m, "a", "")
		if err := m.Delete(ctx, "a"); err != nil || len(seen()) != 0 {
			t.Fatalf("a host's own Delete is not an end report: %v", seen())
		}
		m.WatchEnds(nil)
		put(t, m, "b", "s")
		_ = m.RevokeSession(ctx, "s")
		if len(seen()) != 0 {
			t.Fatal("a removed watcher must hear nothing")
		}
	})
}

// The engine wires its end hooks to its stores: a real render, a real
// expiry, and the hook gets the component's name, instance id, store and
// reason, stamped with the engine's clock.
func TestInstanceEndHook(t *testing.T) {
	t.Parallel()
	clock := newMemClock()
	mem := NewMemoryStates(0, time.Minute)
	mem.SetClock(clock.now)
	rec := &endRecorder{}
	e := v5Engine(t, Options{States: mem, Hooks: []any{rec}, Clock: clock.now})
	_, handle, _ := fillPage(t, e)
	clock.add(2 * time.Minute)
	if n, err := mem.Prune(context.Background(), clock.now()); n != 1 || err != nil {
		t.Fatalf("Prune = %d, %v", n, err)
	}
	got := rec.got()
	if len(got) != 1 {
		t.Fatalf("one end, got %+v", got)
	}
	end := got[0]
	key := strings.TrimPrefix(handle, DefaultStoreName+".")
	if end.Store != DefaultStoreName || end.Reason != EndExpired || !end.At.Equal(clock.now()) ||
		end.Instance == nil || end.Instance.Name != "Counter" || end.Instance.ID == "" || end.Instance.Handle != key {
		t.Fatalf("end = %+v (instance %+v), handle %s", end, end.Instance, handle)
	}
}

// A revoke through the engine reaches the hook too.
func TestInstanceEndHookRevoked(t *testing.T) {
	t.Parallel()
	mem := NewMemoryStates(0, 0)
	rec := &endRecorder{}
	e := v5Engine(t, Options{States: mem, Hooks: []any{rec}})
	fillPage(t, e, WithSession("s1"))
	fillPage(t, e, WithSession("s2"))
	if err := e.RevokeSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	got := rec.got()
	if len(got) != 1 || got[0].Reason != EndRevoked || got[0].Instance.Session != hashSession("s1") {
		t.Fatalf("got %+v", got)
	}
}

type panicEndHook struct{}

func (panicEndHook) OnInstanceEnd(context.Context, InstanceEnd) { panic("boom") }

// A panicking hook is contained: the next hook still hears the end and the
// store's sweep still succeeds.
func TestInstanceEndHookPanicIsContained(t *testing.T) {
	t.Parallel()
	clock := newMemClock()
	mem := NewMemoryStates(0, time.Minute)
	mem.SetClock(clock.now)
	rec := &endRecorder{}
	var logs bytes.Buffer
	e := v5Engine(t, Options{States: mem, Hooks: []any{panicEndHook{}, rec}, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	fillPage(t, e)
	clock.add(2 * time.Minute)
	if n, err := mem.Prune(context.Background(), clock.now()); n != 1 || err != nil {
		t.Fatalf("Prune = %d, %v", n, err)
	}
	if len(rec.got()) != 1 || !strings.Contains(logs.String(), "OnInstanceEnd panicked") {
		t.Fatalf("hooks after a panic still run, and the panic is logged: %d %s", len(rec.got()), logs.String())
	}
}

// Each hook gets its own copy of the instance.
type mutatingEndHook struct{}

func (mutatingEndHook) OnInstanceEnd(_ context.Context, e InstanceEnd) { e.Instance.Name = "changed" }

func TestInstanceEndHooksGetCopies(t *testing.T) {
	t.Parallel()
	mem := NewMemoryStates(0, 0)
	rec := &endRecorder{}
	e := v5Engine(t, Options{States: mem, Hooks: []any{mutatingEndHook{}, rec}})
	fillPage(t, e, WithSession("s"))
	_ = e.RevokeSession(context.Background(), "s")
	if got := rec.got(); len(got) != 1 || got[0].Instance.Name != "Counter" {
		t.Fatalf("one hook must not change what the next sees: %+v", got)
	}
}

// countingWatcher is a store that counts WatchEnds calls.
type countingWatcher struct {
	*MemoryStates
	calls int
}

func (c *countingWatcher) WatchEnds(fn EndFunc) []EndReason {
	c.calls++
	return c.MemoryStates.WatchEnds(fn)
}

// An engine without an end hook leaves the stores' watchers alone: a store
// shared with another engine keeps reporting to it.
func TestNoEndHookTouchesNoWatcher(t *testing.T) {
	t.Parallel()
	st := &countingWatcher{MemoryStates: NewMemoryStates(0, 0)}
	v5Engine(t, Options{States: st})
	if st.calls != 0 {
		t.Fatalf("WatchEnds called %d times without a hook", st.calls)
	}
	v5Engine(t, Options{States: st, Hooks: []any{&endRecorder{}}})
	if st.calls != 1 {
		t.Fatalf("WatchEnds called %d times with a hook", st.calls)
	}
}

// A store that cannot see ends is not asked to pretend: the engine logs
// that the hook never fires for it. Sealed state lives in the page.
func TestEndHookOnBlindStoreIsLogged(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	v5Engine(t, Options{States: sealedStore(t, sealedOpts{}), Hooks: []any{&endRecorder{}}, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if !strings.Contains(logs.String(), "reports no instance ends") {
		t.Fatalf("a sealed store must be named as blind: %s", logs.String())
	}
	if _, ok := any(&SealedStates{}).(EndWatcher); ok {
		t.Fatal("SealedStates must not claim to watch ends")
	}
	// Bytes over a store that cannot report says so too.
	if got := Bytes(nil, BytesOptions{}).WatchEnds(func(context.Context, *Instance, EndReason) {}); got != nil {
		t.Fatalf("Bytes over a blind store reports %v", got)
	}
}

// newHookSet accepts a value that is only an InstanceEndHook.
func TestHookSetAcceptsEndHook(t *testing.T) {
	t.Parallel()
	hs, err := newHookSet([]any{&endRecorder{}})
	if err != nil || len(hs.onEnd) != 1 {
		t.Fatalf("%v %d", err, len(hs.onEnd))
	}
	if _, err := newHookSet([]any{struct{}{}}); err == nil || !strings.Contains(err.Error(), "OnInstanceEnd") {
		t.Fatalf("the error lists OnInstanceEnd: %v", err)
	}
}
