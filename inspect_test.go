package brick

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Instance is a read-only view: changing it changes nothing stored, and
// reading it bumps no version.
func TestInstanceReadOnly(t *testing.T) {
	e := v5Engine(t, Options{States: NewMemoryStates(0, 0)})
	_, handle, _ := fillPage(t, e)
	ctx := context.Background()
	v, err := e.Instance(ctx, handle)
	if err != nil {
		t.Fatal(err)
	}
	if v.Component != "Counter" || v.Path != "components/Counter.html" || v.Store != "default" || v.Version != 0 {
		t.Fatalf("view %+v", v)
	}
	if got, ok := v.Get("count"); !ok || got != int64(0) {
		t.Fatalf("count = %#v %v", got, ok)
	}
	if fmt.Sprint(v.Props["step"]) != "2" || len(v.Errors) != 0 {
		t.Fatalf("props %v errors %v", v.Props, v.Errors)
	}
	v.State["count"] = 99
	v.Props["step"] = 99
	again, err := e.Instance(ctx, handle)
	if err != nil {
		t.Fatal(err)
	}
	if again.State["count"] != int64(0) || fmt.Sprint(again.Props["step"]) != "2" || again.Version != 0 {
		t.Fatalf("the view reached the store: %+v", again)
	}
}

// Instance widens nothing: a sealed handle opens only under the session it
// was rendered for, and a bad or missing handle is ErrGone.
func TestInstanceSessionBound(t *testing.T) {
	e := v5Engine(t, Options{States: sealedStore(t, sealedOpts{})})
	_, handle, _ := fillPage(t, e, WithSession("alice"))
	ctx := context.Background()
	if v, err := e.Instance(ctx, handle, WithSession("alice")); err != nil || v.Component != "Counter" {
		t.Fatalf("own session: %v %v", v, err)
	}
	for name, h := range map[string]string{"no store": "nope", "unknown store": "zz." + handle, "forged": handle + "x"} {
		if _, err := e.Instance(ctx, h, WithSession("alice")); !errors.Is(err, ErrGone) {
			t.Errorf("%s: err = %v, want ErrGone", name, err)
		}
	}
	for _, opts := range [][]RenderOption{{WithSession("bob")}, nil} {
		if _, err := e.Instance(ctx, handle, opts...); !errors.Is(err, ErrGone) {
			t.Errorf("another session %v: err = %v, want ErrGone", opts, err)
		}
	}

	// The memory store checks the session too.
	m := v5Engine(t, Options{States: NewMemoryStates(0, 0)})
	_, mh, _ := fillPage(t, m, WithSession("alice"))
	if _, err := m.Instance(ctx, mh, WithSession("bob")); !errors.Is(err, ErrGone) {
		t.Fatalf("memory store, another session: %v", err)
	}
}

func TestInstanceGetPaths(t *testing.T) {
	v := &InstanceView{State: map[string]any{"a": map[string]any{"b": []any{"x", map[string]any{"c": nil}}}}}
	for path, want := range map[string]bool{"a": true, "a.b": true, "a.b.0": true, "a.b.1.c": true, "a.b.2": false, "a.b.-1": false, "a.b.x": false, "a.z": false, "z.y": false, "a.b.0.q": false} {
		if _, ok := v.Get(path); ok != want {
			t.Errorf("Get(%q) ok = %v, want %v", path, ok, want)
		}
	}
}

// TestIslandTimingRoundTrip: the Server-Timing value a Dev island answer
// carries reads back to the same counts, beside other servers' entries.
func TestIslandTimingRoundTrip(t *testing.T) {
	d := &islandDev{name: "Panel", stats: RenderStats{Components: 3, Types: []TypeCount{{Name: "Card", Count: 2}}, LoaderCalls: 1, BatchCalls: 1, BatchItems: 4}}
	v := d.timing(1500 * time.Microsecond)
	if !strings.HasPrefix(v, `brick-island;dur=1.50;desc="`) {
		t.Fatalf("value = %q", v)
	}
	got, ok := ParseIslandTiming("db;dur=2, " + v + `, cache;desc="hit"`)
	if !ok || got.Component != "Panel" || got.Components != 3 || got.LoaderCalls != 1 || got.BatchCalls != 1 || got.BatchItems != 4 || len(got.Types) != 1 || got.Types[0] != (TypeCount{Name: "Card", Count: 2}) {
		t.Fatalf("parsed = %+v, %v", got, ok)
	}
	if _, ok := ParseIslandTiming(`db;dur=2`); ok {
		t.Error("a value without the entry parsed")
	}
	if empty := (&islandDev{name: "X"}).timing(0); !strings.Contains(empty, `desc="`) {
		t.Errorf("an island that rendered nothing: %q", empty)
	} else if got, ok := ParseIslandTiming(empty); !ok || got.Types == nil {
		t.Errorf("empty types should be [] not null: %+v", got)
	}
}
