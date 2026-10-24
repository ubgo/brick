package brick

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

// statsEngine has one load: component (Name), one load_many component
// (UserCard), an island (Team) and a page that renders them all.
func statsEngine(t *testing.T, b *batchUsers, page string) *Engine {
	t.Helper()
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"users": {
			"cards": b.cards,
			"one": func(a ...any) (any, error) {
				props, _ := a[0].(map[string]any)["props"].(map[string]any)
				return map[string]any{"name": "n" + fmt.Sprint(props["id"])}, nil
			},
		},
	}}
	return newEngine(t, Options{Scripting: sc, FS: theme(
		"lib/users.star", "users",
		"components/UserCard.html", "---\nuse:\n  users: ~/lib/users.star\nbrick:\n  load_many: users.cards\nprops:\n  id: {type: string, required: true}\n---\n<b>{{ .Props.name }}</b>",
		"components/Name.html", "---\nuse:\n  users: ~/lib/users.star\nload: users.one\nprops:\n  id: {type: string, required: true}\n---\n<i>{{ .Props.name }}</i>",
		"components/Team.html", "---\nbrick: {island: true}\n---\n<div><Name id=\"9\" /></div>",
		"content/p.html", page,
	)})
}

// Result.Stats counts what the render did: calls per component (most
// first, ties by name), islands, load: calls without memoised repeats, and
// one load_many call for a level of batched calls.
func TestRenderStats(t *testing.T) {
	t.Parallel()
	b := &batchUsers{}
	e := statsEngine(t, b, `<UserCard id="1" /><UserCard id="2" /><UserCard id="1" /><Name id="7" /><Name id="7" /><Name id="8" /><Team /><Team />`)
	r, err := e.RenderChunks(context.Background(), "content/p.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := RenderStats{
		Components: 8,
		Types:      []TypeCount{{"Name", 3}, {"UserCard", 3}, {"Team", 2}},
		Islands:    2,
		// Name id=7 twice is one call (D5); 7 and 8 are two.
		LoaderCalls: 2,
		// Three UserCards, two distinct ids: one batch of two.
		BatchCalls: 1,
		BatchItems: 2,
	}
	if !reflect.DeepEqual(r.Stats, want) {
		t.Fatalf("Stats =\n%+v\nwant\n%+v", r.Stats, want)
	}
	if got := r.Stats.Top(2); !reflect.DeepEqual(got, want.Types[:2]) {
		t.Errorf("Top(2) = %+v", got)
	}
	if got := r.Stats.Top(10); len(got) != 3 {
		t.Errorf("Top(10) = %+v", got)
	}
	// Same input, same stats (E6).
	again, err := e.RenderChunks(context.Background(), "content/p.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Stats, r.Stats) {
		t.Errorf("second render Stats %+v, first %+v", again.Stats, r.Stats)
	}
}

// A page with no components counts nothing.
func TestRenderStatsEmpty(t *testing.T) {
	t.Parallel()
	e := statsEngine(t, &batchUsers{}, `<h1>hi</h1>`)
	r, err := e.RenderChunks(context.Background(), "content/p.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Stats, RenderStats{}) {
		t.Fatalf("Stats = %+v", r.Stats)
	}
}

// WithStats adds what Fill renders: each island's components and loader
// calls, its root counted once (the page counted it when it captured the
// island). The Result's own Stats stay as they were.
func TestRenderStatsWithFill(t *testing.T) {
	t.Parallel()
	e := statsEngine(t, &batchUsers{}, `<Name id="7" /><Team /><Team />`)
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/p.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	page := RenderStats{Components: 3, Types: []TypeCount{{"Team", 2}, {"Name", 1}}, Islands: 2, LoaderCalls: 1}
	if !reflect.DeepEqual(r.Stats, page) {
		t.Fatalf("page Stats = %+v", r.Stats)
	}
	got := r.Stats
	if _, err := e.Fill(ctx, r, WithStats(&got)); err != nil {
		t.Fatal(err)
	}
	// Each island renders Name id=9 with its own memo: two more calls.
	want := RenderStats{Components: 5, Types: []TypeCount{{"Name", 3}, {"Team", 2}}, Islands: 2, LoaderCalls: 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filled Stats =\n%+v\nwant\n%+v", got, want)
	}
	if !reflect.DeepEqual(r.Stats, page) {
		t.Fatalf("Fill changed the Result's Stats: %+v", r.Stats)
	}
}

// Islands are counted from the chunks, a brick:lazy one also as lazy.
func TestRenderStatsLazyIslands(t *testing.T) {
	t.Parallel()
	var c renderCounts
	got := c.stats([]Chunk{
		{Kind: ChunkBytes, Bytes: []byte("x")},
		{Kind: ChunkIsland, Island: &Island{Name: "A"}},
		{Kind: ChunkIsland, Island: &Island{Name: "B", Lazy: LazyVisible}},
	})
	if got.Islands != 2 || got.LazyIslands != 1 {
		t.Fatalf("Islands %d, LazyIslands %d", got.Islands, got.LazyIslands)
	}
}
