package scriptstarlark

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/ubgo/brick"
)

// hostOnly is the stand-in for a global the site's host supplies
// (Options.HostOnly). It resolves, so the script compiles; calling it or
// reading any attribute (db.query) fails with brick.ErrHostOnly, never with
// a made-up value the script would go on to use.
type hostOnly string

var (
	_ starlark.Callable = hostOnly("")
	_ starlark.HasAttrs = hostOnly("")
)

func (h hostOnly) String() string        { return "<host-only " + string(h) + ">" }
func (h hostOnly) Type() string          { return "host_only" }
func (h hostOnly) Freeze()               {}
func (h hostOnly) Truth() starlark.Bool  { return starlark.True }
func (h hostOnly) Hash() (uint32, error) { return 0, h.err() }
func (h hostOnly) Name() string          { return string(h) }
func (h hostOnly) AttrNames() []string   { return nil }

// Attr fails for every name: the stand-in has no shape to imitate.
func (h hostOnly) Attr(string) (starlark.Value, error) { return nil, h.err() }

// CallInternal fails: the real function lives in the host.
func (h hostOnly) CallInternal(*starlark.Thread, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return nil, h.err()
}

func (h hostOnly) err() error {
	return fmt.Errorf("%s (declared in brick.yaml host: globals): %w", string(h), brick.ErrHostOnly)
}

// isIdent reports a valid Starlark identifier that is not a keyword.
func isIdent(s string) bool {
	f, err := dialect().Parse("", s, 0)
	if err != nil || len(f.Stmts) != 1 {
		return false
	}
	e, ok := f.Stmts[0].(*syntax.ExprStmt)
	if !ok {
		return false
	}
	_, ok = e.X.(*syntax.Ident)
	return ok
}

// scriptPos is the innermost script position of an error, "file:line:col",
// skipping builtin frames, so a one-line message still says where.
func scriptPos(ee *starlark.EvalError) string {
	for i := len(ee.CallStack) - 1; i >= 0; i-- {
		if p := ee.CallStack[i].Pos; p.IsValid() && p.Filename() != "<builtin>" {
			return p.String()
		}
	}
	return ee.Msg
}
