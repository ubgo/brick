package scriptstarlark

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ubgo/brick"
)

// countingScripting counts the calls each script function receives, so a
// test can prove a batch loader ran once.
type countingScripting struct {
	inner brick.Scripting
	mu    *sync.Mutex
	calls map[string]int
}

type countingModule struct {
	brick.ScriptModule
	s countingScripting
}

func (s countingScripting) Compile(ctx context.Context, src brick.ScriptSource) (brick.ScriptModule, error) {
	m, err := s.inner.Compile(ctx, src)
	if err != nil {
		return nil, err
	}
	return countingModule{ScriptModule: m, s: s}, nil
}

func (m countingModule) Call(ctx context.Context, fn string, args ...any) (any, error) {
	m.s.mu.Lock()
	m.s.calls[fn]++
	m.s.mu.Unlock()
	return m.ScriptModule.Call(ctx, fn, args...)
}

// TestBatchLoaderStarlark proves N→1 on a real page: five UserCard tags in
// a range, written in Starlark, make one load_many call that receives the
// three distinct inputs and answers with a list in input order.
func TestBatchLoaderStarlark(t *testing.T) {
	t.Parallel()
	inner, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	sc := countingScripting{inner: inner, mu: &sync.Mutex{}, calls: map[string]int{}}
	e, err := brick.New(brick.Options{Scripting: sc, FS: files(
		"lib/users.star", "USERS = {\"1\": \"Ada\", \"2\": \"Grace\", \"3\": \"Linus\"}\ndef cards(inputs):\n    return [{\"name\": USERS[i[\"props\"][\"id\"]], \"n\": len(inputs)} for i in inputs]\n",
		"components/UserCard.html", "---\nuse:\n  users: ~/lib/users.star\nbrick:\n  load_many: users.cards\nprops:\n  id: {type: string, required: true}\n---\n<li>{{ .Props.name }}/{{ .Props.n }}</li>",
		"content/team.html", "---\nids: [\"2\", \"1\", \"2\", \"3\", \"1\"]\n---\n<ul>{{ range .Params.ids }}<UserCard id=\"{{ . }}\" />{{ end }}</ul>",
	)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := render(t, e, "content/team.html")
	if err != nil {
		t.Fatal(err)
	}
	want := "<ul><li>Grace/3</li><li>Ada/3</li><li>Grace/3</li><li>Linus/3</li><li>Ada/3</li></ul>"
	if strings.TrimSpace(got) != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if sc.calls["cards"] != 1 {
		t.Fatalf("load_many ran %d times, want 1", sc.calls["cards"])
	}
}
