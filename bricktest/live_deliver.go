// Delivering an event to a component: resolving its target and parsing the call an attribute names.

package bricktest

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ubgo/brick"
)

// Deliver applies an action's dispatched events as the runtime does
// : for each event, every on: listener except the
// source (only the source for to "self", only that component for a name),
// then the source's call-site listener on the stateful root above it, in
// document order; each runs as an ordinary action. Events those actions
// dispatch are delivered in turn, with the runtime's guards: at most 4
// hops, no event twice in one chain, at most 50 listeners per event. A
// result with a redirect or navigate wakes no listener. It returns every
// listener action's result, in the order they ran.
func (l *LivePage) Deliver(res *brick.ActionResult) []*brick.ActionResult {
	l.t.Helper()
	return l.deliver(res, nil)
}

func (l *LivePage) deliver(res *brick.ActionResult, chain []string) []*brick.ActionResult {
	l.t.Helper()
	if res == nil || slices.ContainsFunc(res.Effects, func(e brick.Effect) bool {
		return e.Kind == brick.EffectRedirect || e.Kind == brick.EffectNavigate
	}) {
		return nil
	}
	src := l.source[res]
	var out []*brick.ActionResult
	for _, ef := range res.Effects {
		if ef.Kind != brick.EffectDispatch {
			continue
		}
		if len(chain) >= maxEventHops || slices.Contains(chain, ef.Name) {
			continue
		}
		next := append(slices.Clone(chain), ef.Name)
		targets := l.targets(src, ef)
		if len(targets) > maxEventFanOut {
			targets = targets[:maxEventFanOut]
		}
		data := ef.Data
		if data == nil {
			data = map[string]any{}
		}
		for _, tg := range targets {
			name, args, err := parseCall(tg.call)
			if err != nil {
				l.t.Fatalf("bricktest: listener %q on %s: %v", tg.call, tg.id, err)
			}
			if name != "$refresh" {
				args = append(args, data)
			} else {
				args = nil
			}
			r := &LiveRoot{live: l, id: tg.id}
			lres := r.Call(name, args...)
			out = append(out, lres)
			out = append(out, l.deliver(lres, next)...)
		}
	}
	return out
}

// target is one listener a delivery runs.
type target struct{ id, call string }

// targets lists the listeners for one event, deduplicated by root.
func (l *LivePage) targets(src string, ef brick.Effect) []target {
	els := scan(l.html)
	var out []target
	seen := map[string]bool{}
	add := func(id, call string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, target{id, call})
		}
	}
	for _, el := range els {
		if !el.stateful() {
			continue
		}
		id := el.attrs["data-brick"]
		switch {
		case ef.To == brick.DispatchSelf && id != src:
			continue
		case ef.To != brick.DispatchSelf && id == src:
			continue
		case ef.To != "" && ef.To != brick.DispatchSelf && el.attrs["data-brick-name"] != ef.To:
			continue
		}
		if call, ok := jsonMap(el.attrs["data-brick-on"])[ef.Name]; ok {
			add(id, call)
		}
	}
	if ef.To == brick.DispatchSelf || src == "" {
		return out
	}
	for i, el := range els {
		if !el.stateful() || el.attrs["data-brick"] != src {
			continue
		}
		call, ok := jsonMap(el.attrs["data-brick-up"])[ef.Name]
		if !ok {
			break
		}
		for p := els[i].parent; p >= 0; p = els[p].parent {
			if els[p].stateful() {
				add(els[p].attrs["data-brick"], call)
				break
			}
		}
		break
	}
	return out
}

func jsonMap(s string) map[string]string {
	var m map[string]string
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

// callRE is the runtime's action-call grammar: a name, then optional JSON
// arguments.
var callRE = regexp.MustCompile(`(?s)^\s*(\$?[\w.]+)\s*(?:\((.*)\))?\s*$`)

func parseCall(call string) (string, []any, error) {
	m := callRE.FindStringSubmatch(call)
	if m == nil {
		return "", nil, fmt.Errorf("not an action call")
	}
	var args []any
	if a := strings.TrimSpace(m[2]); a != "" {
		dec := json.NewDecoder(strings.NewReader("[" + a + "]"))
		dec.UseNumber()
		if err := dec.Decode(&args); err != nil {
			return "", nil, err
		}
		for i, v := range args {
			args[i] = numbers(v)
		}
	}
	return m[1], args, nil
}

// numbers turns JSON numbers into int64 or float64, as ActionHandler does.
func numbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		for k, e := range x {
			x[k] = numbers(e)
		}
	case []any:
		for i, e := range x {
			x[i] = numbers(e)
		}
	}
	return v
}
