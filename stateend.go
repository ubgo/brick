package brick

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// EndReason says why a stateful component's instance ended. It is a closed set: a store reports only the reasons it can
// actually see, and says which through EndWatcher.
type EndReason string

const (
	// EndExpired: the TTL passed with no use. Reported when the store
	// notices — a memory or SQL store at its next sweep (Prune) or lookup —
	// so the hook may run a sweep interval after the deadline.
	EndExpired EndReason = "expired"
	// EndEvicted: a size bound pushed it out while still live (the memory
	// store's least-recently-used limit).
	EndEvicted EndReason = "evicted"
	// EndRevoked: Engine.RevokeSession ended it with its session.
	EndRevoked EndReason = "revoked"
)

// InstanceEnd is what an InstanceEndHook is told. It describes an instance
// that no longer exists, so it carries no request: it fires from a store's
// sweep, an eviction or a revoke, never from a page or an action.
type InstanceEnd struct {
	// Store is the registered store name ("default", "redis", …).
	Store  string
	Reason EndReason
	// At is when the store saw the end, by the engine's Clock; for an
	// expiry that is after the deadline, by up to one sweep interval.
	At time.Time
	// Instance is a copy of the instance as last saved: Name and Path name
	// the component, ID its instance id, Handle the store key, Session the
	// hashed session, State its last state, Pending its unconfirmed
	// uploads (so a hook can delete orphaned objects).
	Instance *Instance
}

// InstanceEndHook runs when an instance ends, for the stores that can see
// it (EndWatcher). It runs on the goroutine that noticed the end — a
// prune timer, a Put that evicted, a RevokeSession call — after the store
// released its locks, so it may use the store; a slow hook slows that
// caller. A panic is recovered.
type InstanceEndHook interface {
	OnInstanceEnd(ctx context.Context, end InstanceEnd)
}

// EndFunc is what a store calls for each instance it saw end.
type EndFunc func(ctx context.Context, inst *Instance, reason EndReason)

// EndWatcher is the upgrade of a StateStore that can see its instances
// end. WatchEnds installs fn (nil removes it) and returns the reasons this
// store reports, so a store that cannot see expiry (Redis deletes the key
// itself) says so instead of pretending, and a sealed store, which never
// sees an instance end, simply does not implement it. The last call wins:
// a site rebuild makes a new Engine over the same store, and only the
// current one should hear the ends. A store shared by two live engines
// reports to the one made last.
type EndWatcher interface {
	WatchEnds(fn EndFunc) []EndReason
}

// BytesEndFunc is EndFunc at the bytes level: the key and the stored
// bytes as the store held them (sealed and encoded), which Bytes decodes.
type BytesEndFunc func(ctx context.Context, key string, data []byte, reason EndReason)

// BytesEndWatcher is EndWatcher for a BytesStore; Bytes turns it into an
// EndWatcher. The adapter must report a value only when its own delete
// removed it, so two servers sweeping one database report each end once.
type BytesEndWatcher interface {
	WatchEndsBytes(fn BytesEndFunc) []EndReason
}

// watchEnds connects the engine's end hooks to every store that can
// report ends. It runs at the end of New, so a failed New never steals a
// store from the engine that is still serving. Without an end hook it does
// nothing, leaving a shared store's watcher to whoever set it.
func (e *Engine) watchEnds() {
	if len(e.hooks.onEnd) == 0 {
		return
	}
	for _, name := range sortedKeys(e.stores) {
		w, ok := e.stores[name].(EndWatcher)
		var reasons []EndReason
		if ok {
			reasons = w.WatchEnds(e.endFunc(name))
		}
		if len(reasons) == 0 {
			e.opts.Logger.Warn("brick: state store reports no instance ends; OnInstanceEnd never fires for it", "store", name)
			continue
		}
		if missing := missingReasons(reasons); len(missing) > 0 {
			e.opts.Logger.Info("brick: state store cannot report every instance end", "store", name, "reports", joinReasons(reasons), "never", joinReasons(missing))
		}
	}
}

// allReasons is every EndReason, for telling a host which ones a store
// will never report.
var allReasons = []EndReason{EndExpired, EndEvicted, EndRevoked}

func missingReasons(have []EndReason) []EndReason {
	var out []EndReason
	for _, r := range allReasons {
		if !slices.Contains(have, r) {
			out = append(out, r)
		}
	}
	return out
}

func joinReasons(rs []EndReason) string {
	s := make([]string, len(rs))
	for i, r := range rs {
		s[i] = string(r)
	}
	return strings.Join(s, ",")
}

// endFunc is the EndFunc the engine gives store name: it stamps the store
// and time and tells every end hook, each in isolation so one panicking
// hook neither stops the others nor fails the store's sweep or revoke.
func (e *Engine) endFunc(name string) EndFunc {
	return func(ctx context.Context, inst *Instance, reason EndReason) {
		if inst == nil {
			return
		}
		at := e.opts.Clock().UTC()
		for _, h := range e.hooks.onEnd {
			// Each hook gets its own copy, so one cannot change what the next sees.
			end := InstanceEnd{Store: name, Reason: reason, At: at, Instance: copyInstance(inst)}
			func() {
				defer func() {
					if p := recover(); p != nil {
						e.opts.Logger.Error("brick: OnInstanceEnd panicked", "store", name, "component", inst.Name, "panic", fmt.Sprint(p))
					}
				}()
				h.OnInstanceEnd(ctx, end)
			}()
		}
	}
}
