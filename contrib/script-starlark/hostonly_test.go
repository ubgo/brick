package scriptstarlark

import (
	"errors"
	"strings"
	"testing"

	"go.starlark.net/starlark"

	"github.com/ubgo/brick"
)

// A HostOnly name resolves, so the module compiles; using it (a call or an
// attribute) fails with brick.ErrHostOnly and says where; an undeclared
// name is still a compile error.
func TestHostOnly(t *testing.T) {
	t.Parallel()
	page := func(body string) string {
		return "---\nuse:\n  lib: ~/lib.star\n---\n" + body
	}
	fsys := files(
		"lib.star", "def stamp():\n    return now()\ndef rows():\n    return db.query(\"select 1\")\ndef pure():\n    return 7\n",
		"content/pure.html", page("{{ lib.pure }}"),
		"content/call.html", page("{{ lib.stamp }}"),
		"content/attr.html", page("{{ lib.rows }}"),
	)
	e := engine(t, Options{HostOnly: []string{"now", "db"}}, fsys)
	if got, err := render(t, e, "content/pure.html"); err != nil || got != "7" {
		t.Fatalf("pure: %q %v", got, err)
	}
	for _, p := range []string{"content/call.html", "content/attr.html"} {
		_, err := render(t, e, p)
		if !errors.Is(err, brick.ErrHostOnly) || !strings.Contains(err.Error(), "lib.star:") {
			t.Fatalf("%s: want ErrHostOnly with a position, got %v", p, err)
		}
	}
	// Only "now" declared: "db" is undefined, a compile error, not a stand-in.
	e2 := engine(t, Options{HostOnly: []string{"now"}}, fsys)
	if _, err := render(t, e2, "content/pure.html"); err == nil || !strings.Contains(err.Error(), "undefined: db") || errors.Is(err, brick.ErrHostOnly) {
		t.Fatalf("undeclared: %v", err)
	}
}

// A stand-in never hides a real global, and only names are accepted.
func TestHostOnlyRejects(t *testing.T) {
	t.Parallel()
	for name, opts := range map[string]Options{
		"clash with Globals": {Globals: starlark.StringDict{"now": starlark.None}, HostOnly: []string{"now"}},
		"clash with brick":   {HostOnly: []string{"brick"}},
		"clash with http":    {HTTPAllow: []string{"https://example.com"}, HostOnly: []string{"http"}},
		"not a name":         {HostOnly: []string{"db.query"}},
		"keyword":            {HostOnly: []string{"def"}},
		"empty":              {HostOnly: []string{""}},
	} {
		if _, err := New(opts); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// With the real global supplied, the same module renders the real value:
// HostOnly is only ever set by a program that is not the host.
func TestHostOnlyRealHostUnaffected(t *testing.T) {
	t.Parallel()
	now := starlark.NewBuiltin("now", func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
		return starlark.String("noon"), nil
	})
	e := engine(t, Options{Globals: starlark.StringDict{"now": now}}, files(
		"lib.star", "def stamp():\n    return now()\n",
		"content/a.html", "---\nuse:\n  lib: ~/lib.star\n---\n{{ lib.stamp }}",
	))
	if got, err := render(t, e, "content/a.html"); err != nil || got != "noon" {
		t.Fatalf("got %q %v", got, err)
	}
}
