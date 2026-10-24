package brick

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// batchUsers is a counting batch loader: one call per load_many, the ids
// of every call recorded, and a user dict per input.
type batchUsers struct {
	mu    sync.Mutex
	calls [][]string
}

func (b *batchUsers) cards(a ...any) (any, error) {
	list, _ := a[0].([]any)
	var ids []string
	out := make([]any, len(list))
	for i, in := range list {
		props, _ := in.(map[string]any)["props"].(map[string]any)
		id := fmt.Sprint(props["id"])
		ids = append(ids, id)
		out[i] = map[string]any{"name": "user" + id}
	}
	b.mu.Lock()
	b.calls = append(b.calls, ids)
	b.mu.Unlock()
	return out, nil
}

func batchEngine(t *testing.T, b *batchUsers, files ...string) *Engine {
	t.Helper()
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"users": {"cards": b.cards},
	}}
	base := []string{
		"lib/users.star", "users",
		"components/UserCard.html", "---\nuse:\n  users: ~/lib/users.star\nbrick:\n  load_many: users.cards\nprops:\n  id: {type: string, required: true}\n---\n<b data-id=\"{{ .ID }}\">{{ .Props.name }}</b>",
	}
	return newEngine(t, Options{Scripting: sc, EmitIDs: true, FS: theme(append(base, files...)...)})
}

// TestBatchLoaderOneCall pins the batch contract: N calls of a batch-loaded component in
// a range make one load_many call with every distinct input, in call order,
// and the output is what N single loads would have given.
func TestBatchLoaderOneCall(t *testing.T) {
	t.Parallel()
	b := &batchUsers{}
	e := batchEngine(t, b,
		"content/list.html", "---\nids: [\"3\", \"1\", \"3\", \"2\"]\n---\n<ul>{{ range .Params.ids }}<li><UserCard id=\"{{ . }}\" /></li>{{ end }}</ul>",
	)
	got := render(t, e, "content/list.html", nil)
	if len(b.calls) != 1 {
		t.Fatalf("load_many ran %d times (%v), want 1", len(b.calls), b.calls)
	}
	if strings.Join(b.calls[0], ",") != "3,1,2" {
		t.Fatalf("batch inputs %v, want the distinct ids in call order 3,1,2", b.calls[0])
	}
	names := regexp.MustCompile(`>(user\d)<`).FindAllStringSubmatch(got, -1)
	var order []string
	for _, n := range names {
		order = append(order, n[1])
	}
	if strings.Join(order, ",") != "user3,user1,user3,user2" {
		t.Fatalf("rendered %v in %q", order, got)
	}
	if !strings.HasPrefix(got, "<ul><li><b ") || !strings.HasSuffix(got, "</b></li></ul>") {
		t.Fatalf("placeholders not spliced in place: %q", got)
	}
}

// TestBatchLoaderDeterministic pins E6 for deferred calls: the same render
// twice gives the same bytes, and every instance keeps the id it would
// have had without batching (its call-site occurrence, C10).
func TestBatchLoaderDeterministic(t *testing.T) {
	t.Parallel()
	page := "<p>{{ range .Params.ids }}<UserCard id=\"{{ . }}\" />{{ end }}</p><Plain />"
	b := &batchUsers{}
	e := batchEngine(t, b,
		"components/Plain.html", "<i data-id=\"{{ .ID }}\">plain</i>",
		"content/p.html", "---\nids: [\"1\", \"2\", \"1\"]\n---\n"+page,
	)
	first := render(t, e, "content/p.html", nil)
	second := render(t, e, "content/p.html", nil)
	if first != second {
		t.Fatalf("not deterministic:\n%s\n%s", first, second)
	}
	// The same page with a plain loader (load:) renders without deferral;
	// its ids must match the batched render's exactly.
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"users": {"one": func(a ...any) (any, error) {
			props, _ := a[0].(map[string]any)["props"].(map[string]any)
			return map[string]any{"name": "user" + fmt.Sprint(props["id"])}, nil
		}},
	}}
	plain := newEngine(t, Options{Scripting: sc, EmitIDs: true, FS: theme(
		"lib/users.star", "users",
		"components/UserCard.html", "---\nuse:\n  users: ~/lib/users.star\nload: users.one\nprops:\n  id: {type: string, required: true}\n---\n<b data-id=\"{{ .ID }}\">{{ .Props.name }}</b>",
		"components/Plain.html", "<i data-id=\"{{ .ID }}\">plain</i>",
		"content/p.html", "---\nids: [\"1\", \"2\", \"1\"]\n---\n"+page,
	)})
	if want := render(t, plain, "content/p.html", nil); first != want {
		t.Fatalf("batched render differs from the unbatched one:\nbatched %s\nplain   %s", first, want)
	}
	ids := regexp.MustCompile(`data-id="(b[0-9a-f]+)"`).FindAllStringSubmatch(first, -1)
	seen := map[string]bool{}
	for _, m := range ids {
		if seen[m[1]] {
			t.Fatalf("duplicate id %s in %q", m[1], first)
		}
		seen[m[1]] = true
	}
	if len(ids) != 4 {
		t.Fatalf("want 4 ids, got %d in %q", len(ids), first)
	}
}

// TestBatchLoaderLevels pins nesting: a batch-loaded component inside
// another one's output is batched with its own level, so two levels cost
// two calls, not 1+N.
func TestBatchLoaderLevels(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var teamCalls, userCalls int
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"data": {
			"teams": func(a ...any) (any, error) {
				mu.Lock()
				teamCalls++
				mu.Unlock()
				list := a[0].([]any)
				out := make([]any, len(list))
				for i, in := range list {
					id := fmt.Sprint(in.(map[string]any)["props"].(map[string]any)["id"])
					out[i] = map[string]any{"members": []any{id + "a", id + "b"}}
				}
				return out, nil
			},
			"users": func(a ...any) (any, error) {
				mu.Lock()
				userCalls++
				mu.Unlock()
				list := a[0].([]any)
				out := make([]any, len(list))
				for i, in := range list {
					out[i] = map[string]any{"name": strings.ToUpper(fmt.Sprint(in.(map[string]any)["props"].(map[string]any)["id"]))}
				}
				return out, nil
			},
		},
	}}
	e := newEngine(t, Options{Scripting: sc, FS: theme(
		"lib/data.star", "data",
		"components/Team.html", "---\nuse: {data: ~/lib/data.star}\nbrick: {load_many: data.teams}\nprops: {id: {type: string}}\n---\n[{{ range .Props.members }}<Member id=\"{{ . }}\" />{{ end }}]",
		"components/Member.html", "---\nuse: {data: ~/lib/data.star}\nbrick: {load_many: data.users}\nprops: {id: {type: string}}\n---\n{{ .Props.name }};",
		"content/t.html", "---\nteams: [\"x\", \"y\", \"z\"]\n---\n{{ range .Params.teams }}<Team id=\"{{ . }}\" />{{ end }}",
	)})
	got := render(t, e, "content/t.html", nil)
	if got != "[XA;XB;][YA;YB;][ZA;ZB;]" {
		t.Fatalf("got %q", got)
	}
	if teamCalls != 1 || userCalls != 1 {
		t.Fatalf("teams ran %d, users ran %d; want 1 each", teamCalls, userCalls)
	}
}

// TestBatchLoaderUnbatchedPaths pins the fallback: where a call cannot
// wait (inside a boundary), load_many still answers it, as a one-element
// batch, so the output never depends on whether batching happened.
func TestBatchLoaderUnbatchedPaths(t *testing.T) {
	t.Parallel()
	b := &batchUsers{}
	e := batchEngine(t, b,
		"content/b.html", "<brick:boundary><UserCard id=\"7\" /><UserCard id=\"8\" /></brick:boundary>|<UserCard id=\"7\" />",
	)
	got := render(t, e, "content/b.html", nil)
	if !strings.Contains(got, ">user7<") || !strings.Contains(got, ">user8<") {
		t.Fatalf("got %q", got)
	}
	// Two single calls inside the boundary; the call outside reuses the
	// memoised 7 (D5), so it neither defers nor calls again.
	if len(b.calls) != 2 {
		t.Fatalf("calls %v, want two one-element batches", b.calls)
	}
}

// TestBatchLoaderErrors pins the contract checks: a wrong answer shape, a
// page with load_many, and a file with both loaders all fail clearly.
func TestBatchLoaderErrors(t *testing.T) {
	t.Parallel()
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"bad": {
			"short": func(a ...any) (any, error) { return []any{}, nil },
			"one":   func(a ...any) (any, error) { return map[string]any{}, nil },
		},
	}}
	e := newEngine(t, Options{Scripting: sc, FS: theme(
		"lib/bad.star", "bad",
		"components/Short.html", "---\nuse: {bad: ~/lib/bad.star}\nbrick: {load_many: bad.short}\n---\nx",
		"content/short.html", "<Short /><Short />",
		"content/pagemany.html", "---\nuse: {bad: ~/lib/bad.star}\nbrick: {load_many: bad.short}\n---\nx",
		"content/both.html", "---\nuse: {bad: ~/lib/bad.star}\nload: bad.one\nbrick: {load_many: bad.short}\n---\nx",
		"components/Nofn.html", "---\nuse: {bad: ~/lib/bad.star}\nbrick: {load_many: bad.nope}\n---\nx",
		"content/nofn.html", "<Nofn />",
	)})
	if err := renderErr(t, e, "content/short.html"); err.Code != CodeScript || !strings.Contains(err.Error(), "list of 1 dicts") {
		t.Fatalf("short: %v", err)
	}
	if err := renderErr(t, e, "content/pagemany.html"); err.Code != CodeScript || !strings.Contains(err.Error(), "only a component") {
		t.Fatalf("pagemany: %v", err)
	}
	if err := renderErr(t, e, "content/both.html"); !strings.Contains(err.Error(), "one loader") {
		t.Fatalf("both: %v", err)
	}
	if err := renderErr(t, e, "content/nofn.html"); err.Code != CodeScript || !strings.Contains(err.Error(), "load_many") {
		t.Fatalf("nofn: %v", err)
	}
}

// TestBatchLoaderCanceled pins E2 for a batch: a canceled render is
// reported as canceled, not as a script failure.
func TestBatchLoaderCanceled(t *testing.T) {
	t.Parallel()
	e := batchEngine(t, &batchUsers{}, "content/c.html", "---\nids: [\"1\", \"2\"]\n---\n{{ range .Params.ids }}<UserCard id=\"{{ . }}\" />{{ end }}")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.Render(ctx, "content/c.html", map[string]any{})
	var be *Error
	if !errors.As(err, &be) || be.Code != CodeCanceled {
		t.Fatalf("want a canceled error, got %v", err)
	}
}

// TestBatchLoaderInIsland pins the Fill path: batch-loaded components inside
// an island batch when the island renders, and the island's output matches
// an inline render's.
func TestBatchLoaderInIsland(t *testing.T) {
	t.Parallel()
	b := &batchUsers{}
	e := batchEngine(t, b,
		"components/Team.html", "---\nbrick: {island: true}\nprops: {ids: {type: list}}\n---\n<div>{{ range .Props.ids }}<UserCard id=\"{{ . }}\" />{{ end }}</div>",
		"content/i.html", "<Team :ids=\"list `4` `5` `6`\" />",
	)
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/i.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 0 {
		t.Fatalf("the page render loaded island data: %v", b.calls)
	}
	out, err := e.Fill(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 1 || strings.Join(b.calls[0], ",") != "4,5,6" {
		t.Fatalf("calls %v, want one batch 4,5,6", b.calls)
	}
	for _, n := range []string{">user4<", ">user5<", ">user6<"} {
		if !strings.Contains(string(out), n) {
			t.Fatalf("missing %s in %q", n, out)
		}
	}
}
