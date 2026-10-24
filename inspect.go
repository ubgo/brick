package brick

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ubgo/brick/validate"
)

// InstanceView is a read-only copy of one stateful component as its store
// holds it now: what a host's own tests and tools inspect
// instead of scraping the rendered HTML. Every map and slice is a copy, so
// changing it changes nothing stored.
type InstanceView struct {
	// Component is the tag name, Path the component file, ID its instance
	// id in the page, Store the name of the store holding it.
	Component string
	Path      string
	ID        string
	Store     string
	// Version is 0 at render and +1 per applied action.
	Version int64
	// Props are the call site's props; State the current state.
	Props map[string]any
	State map[string]any
	// Touched are the field paths the user has sent or an action
	// validated, sorted.
	Touched []string
	// Errors are the rule errors of the touched fields, computed now the
	// way the next render computes .Errors. Errors an action returned are
	// shown by the render that followed it and never stored, so they are
	// not here.
	Errors map[string]string
}

// Get returns the value at a dotted state path (a key, or a list index),
// and whether it is there.
func (v *InstanceView) Get(path string) (any, bool) {
	parent, last := v.State, path
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		parent, last = nil, path[i+1:]
		switch x := validate.Lookup(v.State, path[:i]).(type) {
		case map[string]any:
			parent = x
		case []any:
			return lookupIndex(x, last)
		}
	}
	val, ok := parent[last]
	return val, ok
}

func lookupIndex(list []any, seg string) (any, bool) {
	n, err := strconv.Atoi(seg)
	if err != nil || n < 0 || n >= len(list) {
		return nil, false
	}
	return list[n], true
}

// Instance loads the instance behind handle (the page's data-brick-state
// value, "<store>.<key>") and returns a read-only view of it. It is a Go
// API for the host, which already holds the stores and their keys; it is
// never reachable over HTTP and widens nothing: the instance is read
// through its store exactly as an action reads it, so a sealed handle
// opens only under the session it was rendered for (pass WithSession),
// and an instance from another session is ErrGone, as for an action. The
// token is not asked for, because it guards the HTTP boundary against a
// forged request, and a caller holding the engine is not one. Nothing is
// written: no version bump, no touch, no migration saved (a store with a
// sliding expiry extends it on this read, as on any read).
func (e *Engine) Instance(ctx context.Context, handle string, opts ...RenderOption) (*InstanceView, error) {
	var ro renderOptions
	for _, o := range opts {
		o(&ro)
	}
	if ro.err != nil {
		return nil, ro.err
	}
	name, key, ok := strings.Cut(handle, ".")
	store := e.stores[name]
	if !ok || store == nil {
		return nil, ErrGone
	}
	hash := hashSession(ro.session)
	inst, ok, err := store.Get(WithStateScope(ctx, StateScope{Store: name, Session: hash}), key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if !ok || subtle.ConstantTimeCompare([]byte(inst.Session), []byte(hash)) != 1 {
		return nil, ErrGone
	}
	c, err := e.load(ctx, inst.Path, kindComponent, inst.Locale)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrGone
		}
		return nil, err
	}
	state, _ := deepCopy(inst.State).(map[string]any)
	if state == nil {
		state = map[string]any{}
	}
	props, _ := deepCopy(inst.Props).(map[string]any)
	view := &InstanceView{
		Component: inst.Name, Path: inst.Path, ID: inst.ID, Store: name, Version: inst.Version,
		Props: props, State: state, Touched: slices.Clone(inst.Touched), Errors: map[string]string{},
	}
	if len(c.meta.rules) > 0 && len(inst.Touched) > 0 {
		// The validator sees its own copy, so a rule cannot reach the view.
		st, _ := deepCopy(inst.State).(map[string]any)
		re, err := ruleErrors(ctx, e.opts.Validator, c.meta, st, inst.Touched, inst.Locale)
		if err != nil {
			return nil, err
		}
		view.Errors = re
	}
	return view, nil
}

// A lazy island renders on its own request, after the page and its Dev
// toolbar were sent, so the page's RenderStats cannot hold it. In Dev mode the island's answer carries its own counts instead, in
// a Server-Timing entry: the one response header a page's script can read
// for a fetch it did not make (PerformanceResourceTiming.serverTiming),
// so the toolbar learns what each island rendered without the runtime
// knowing the toolbar exists. Prod never sends it: the counts name the
// components a page is built from.
const (
	// ServerTimingHeader is the standard header the counts ride in.
	ServerTimingHeader = "Server-Timing"
	// IslandTimingName is the entry's metric name; the toolbar matches it.
	IslandTimingName = "brick-island"
)

// IslandTiming is the JSON a Dev island answer's Server-Timing entry
// carries (base64 in its desc, since a quoted-string cannot hold JSON's
// quotes unescaped): the island's component and what its render counted,
// the same fields RenderStats has.
type IslandTiming struct {
	Component   string      `json:"component"`
	Components  int         `json:"components"`
	Types       []TypeCount `json:"types"`
	LoaderCalls int         `json:"loaderCalls"`
	BatchCalls  int         `json:"batchCalls"`
	BatchItems  int         `json:"batchItems"`
}

// islandDev collects one lazy island's counts while IslandHandler renders
// it in Dev mode; nil in Prod, which counts nothing.
type islandDev struct {
	name  string
	stats RenderStats
}

// timing is the Server-Timing value for the island, took being the time
// the handler spent on it.
func (d *islandDev) timing(took time.Duration) string {
	t := IslandTiming{
		Component: d.name, Components: d.stats.Components, Types: d.stats.Types,
		LoaderCalls: d.stats.LoaderCalls, BatchCalls: d.stats.BatchCalls, BatchItems: d.stats.BatchItems,
	}
	if t.Types == nil {
		t.Types = []TypeCount{}
	}
	b, _ := json.Marshal(t) // plain fields only: Marshal cannot fail
	ms := float64(took.Microseconds()) / float64(time.Millisecond/time.Microsecond)
	return fmt.Sprintf(`%s;dur=%.2f;desc="%s"`, IslandTimingName, ms, base64.StdEncoding.EncodeToString(b))
}

// ParseIslandTiming reads the counts back from a Server-Timing value (a
// host's test, or a tool reading Dev answers); ok is false when the value
// holds no brick-island entry.
func ParseIslandTiming(header string) (IslandTiming, bool) {
	var t IslandTiming
	for entry := range strings.SplitSeq(header, ",") {
		params := strings.Split(strings.TrimSpace(entry), ";")
		if strings.TrimSpace(params[0]) != IslandTimingName {
			continue
		}
		for _, p := range params[1:] {
			v, ok := strings.CutPrefix(strings.TrimSpace(p), "desc=")
			if !ok {
				continue
			}
			b, err := base64.StdEncoding.DecodeString(strings.Trim(v, `"`))
			if err != nil || json.Unmarshal(b, &t) != nil {
				return IslandTiming{}, false
			}
			return t, true
		}
	}
	return t, false
}
