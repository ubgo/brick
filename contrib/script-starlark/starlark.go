// Package scriptstarlark runs brick's scripts in Starlark.
//
// It implements brick.Scripting. A script is sealed: it can compute, and it
// reaches the outside world only through capabilities the host grants in Go
// — Globals, and the standard `db` and `http` capabilities, which exist only
// when Options.DB or Options.HTTPAllow is set. The `brick` module
// (brick.dispatch, brick.flash, brick.redirect, …) is always predeclared and
// records an action's effects; called while a page renders, it is an error
// .
//
//	eng, err := brick.New(brick.Options{
//	    FS:        fsys,
//	    Scripting: scriptstarlark.New(scriptstarlark.Options{DB: readOnlyDB}),
//	})
//
// Invariants:
//   - scripts never choose a file while running: load() takes a literal path
//     that brick resolves before the module runs;
//   - the dialect is pinned: no set, no while, no top-level control flow, no
//     global reassignment, no recursion;
//   - every call has a step limit and a wall-clock limit, and the render's
//     ctx cancels it;
//   - SQL passed to db.query is fixed text, checked from the syntax tree.
package scriptstarlark

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/ubgo/brick"
)

// Defaults (D6, D8, D9). Zero in Options means these, never "unlimited".
const (
	DefaultSteps        = 1_000_000
	DefaultTimeout      = 2 * time.Second
	DefaultDBMaxRows    = 10_000
	DefaultHTTPMaxBytes = 5 << 20
	// maxDepth caps value conversion in both directions (D2).
	maxDepth = 64
	// dataSymbol is the name a loaded data file binds: load("~/d.yaml", "data").
	dataSymbol = "data"
)

// Options configures the Starlark engine.
type Options struct {
	// Steps is the per-call execution step limit.
	Steps uint64
	// Timeout is the per-call wall-clock limit; the ctx deadline wins when
	// sooner.
	Timeout time.Duration
	// Globals are extra capabilities: the one door between scripts and the
	// outside world. Nothing is granted by default.
	Globals starlark.StringDict
	// DB turns on the read-only `db` capability. Pass a connection whose
	// database role is read-only: that is the real guarantee (D8).
	DB *sql.DB
	// DBMaxRows caps a query's rows; more is an error, never a truncation.
	DBMaxRows int
	// DBExec additionally grants db.exec for writes. Off by default, so a
	// theme that can INSERT says so in Go rather than by accident.
	DBExec bool
	// HTTPAllow turns on `http`, restricted to these origins, each written
	// "https://host" or "https://host:port" (D9). No wildcards.
	HTTPAllow []string
	// HTTPMaxBytes caps a response body.
	HTTPMaxBytes int64
	// HTTPClient is used for requests; its redirect policy is replaced so
	// every redirect is re-checked against the allowlist.
	HTTPClient *http.Client
	// Logger receives print() output. Nil discards.
	Logger *slog.Logger
	// HostOnly are globals another program supplies: the site's own host,
	// when this engine runs inside a tool that is not it (brick check,
	// brick lsp; brick.yaml's host: globals). Each name resolves, so scripts
	// naming it compile, and any use of it fails with brick.ErrHostOnly. A
	// name that is also a real global (Globals, db, http, brick) is an
	// error: a stand-in never hides the real thing.
	HostOnly []string
}

// Engine is a brick.Scripting backed by Starlark.
type Engine struct {
	opts    Options
	globals starlark.StringDict
	allow   []origin
}

// New returns a Starlark scripting engine.
func New(opts Options) (*Engine, error) {
	if opts.Steps == 0 {
		opts.Steps = DefaultSteps
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.DBMaxRows <= 0 {
		opts.DBMaxRows = DefaultDBMaxRows
	}
	if opts.HTTPMaxBytes <= 0 {
		opts.HTTPMaxBytes = DefaultHTTPMaxBytes
	}
	e := &Engine{opts: opts, globals: starlark.StringDict{}}
	for k, v := range opts.Globals {
		e.globals[k] = v
	}
	if _, clash := e.globals[brickModule]; clash {
		return nil, errors.New("scriptstarlark: Globals already has brick, the predeclared effects module (brick.dispatch, brick.flash, …)")
	}
	e.globals[brickModule] = newBrickModule()
	if opts.DB != nil {
		if _, clash := e.globals["db"]; clash {
			return nil, errors.New("scriptstarlark: Globals already has db, and Options.DB would replace it")
		}
		e.globals["db"] = newDB(e)
	}
	for _, name := range opts.HostOnly {
		if !isIdent(name) {
			return nil, fmt.Errorf("scriptstarlark: HostOnly %q is not a Starlark name", name)
		}
		if _, clash := e.globals[name]; clash || (name == "http" && len(opts.HTTPAllow) > 0) {
			return nil, fmt.Errorf("scriptstarlark: HostOnly %q is already a real global, so the stand-in would hide it", name)
		}
		e.globals[name] = hostOnly(name)
	}
	if len(opts.HTTPAllow) > 0 {
		allow, err := parseAllow(opts.HTTPAllow)
		if err != nil {
			return nil, err
		}
		e.allow = allow
		if _, clash := e.globals["http"]; clash {
			return nil, errors.New("scriptstarlark: Globals already has http, and Options.HTTPAllow would replace it")
		}
		e.globals["http"] = newHTTP(e)
	}
	e.globals.Freeze()
	return e, nil
}

// dialect pins every optional Starlark feature off (D6): no set, no while,
// no top-level control flow, no global reassignment, and the recursion check
// stays on.
func dialect() *syntax.FileOptions { return &syntax.FileOptions{} }

// Compile compiles a module and every module it loads (D7).
func (e *Engine) Compile(ctx context.Context, src brick.ScriptSource) (brick.ScriptModule, error) {
	l := &loader{e: e, resolve: src.Resolve, done: map[string]*loaded{}}
	globals, err := l.load(ctx, src.Path, src.Src, src.Line, nil)
	if err != nil {
		return nil, err
	}
	return &module{e: e, path: src.Path, globals: globals}, nil
}

// loader compiles one module graph, each module once (keyed by content).
type loader struct {
	e       *Engine
	resolve func(string) (brick.ScriptImport, error)
	done    map[string]*loaded
}

type loaded struct {
	globals starlark.StringDict
	err     error
	busy    bool
}

func (l *loader) load(ctx context.Context, name string, src []byte, line int, stack []string) (starlark.StringDict, error) {
	key := string(src)
	if d, ok := l.done[key]; ok {
		if d.busy {
			return nil, fmt.Errorf("load cycle: %s", strings.Join(append(stack, name), " → "))
		}
		return d.globals, d.err
	}
	d := &loaded{busy: true}
	l.done[key] = d
	defer func() { d.busy = false }()

	// Pad with newlines so Starlark's own positions match the file (an
	// inline <brick:script> starts partway down its page).
	padded := strings.Repeat("\n", max(line-1, 0)) + string(src)
	f, err := dialect().Parse(name, padded, 0)
	if err != nil {
		d.err = err
		return nil, err
	}
	if err := checkSQL(f); err != nil {
		d.err = err
		return nil, err
	}
	prog, err := starlark.FileProgram(f, func(n string) bool { return l.e.predeclared(n) })
	if err != nil {
		d.err = err
		return nil, err
	}
	thread, done := l.e.thread(ctx, name)
	defer done()
	thread.Load = func(_ *starlark.Thread, module string) (starlark.StringDict, error) {
		if l.resolve == nil {
			return nil, errors.New("load() is not available here")
		}
		imp, err := l.resolve(module)
		if err != nil {
			return nil, err
		}
		if imp.IsData {
			v, err := toStarlark(imp.Data, 0, nil)
			if err != nil {
				return nil, fmt.Errorf("load(%q): %w", module, err)
			}
			v.Freeze()
			return starlark.StringDict{dataSymbol: v}, nil
		}
		return l.load(ctx, imp.Path, imp.Src, 1, append(stack, name))
	}
	globals, err := prog.Init(thread, l.e.globals)
	if err != nil {
		d.err = err
		return nil, err
	}
	globals.Freeze()
	d.globals = globals
	return globals, nil
}

// predeclared reports the names a module may reference without defining.
func (e *Engine) predeclared(name string) bool {
	_, ok := e.globals[name]
	return ok
}

// thread returns a thread with the step limit, the timeout and cancellation
// wired to ctx; done releases it.
func (e *Engine) thread(ctx context.Context, name string) (*starlark.Thread, func()) {
	ctx, cancel := context.WithTimeout(ctx, e.opts.Timeout)
	t := &starlark.Thread{Name: name, Print: func(_ *starlark.Thread, msg string) {
		if e.opts.Logger != nil {
			e.opts.Logger.Info(msg, "script", name)
		}
	}}
	t.SetMaxExecutionSteps(e.opts.Steps)
	t.SetLocal(ctxKey, ctx)
	stop := context.AfterFunc(ctx, func() { t.Cancel(ctx.Err().Error()) })
	return t, func() {
		stop()
		cancel()
	}
}

// ctxKey stores the call's context on the thread, for capabilities.
const ctxKey = "brick.ctx"

// threadCtx returns the context a capability call runs under.
func threadCtx(t *starlark.Thread) context.Context {
	if c, ok := t.Local(ctxKey).(context.Context); ok {
		return c
	}
	return context.Background()
}

// module is a compiled module.
type module struct {
	e       *Engine
	path    string
	globals starlark.StringDict
}

// Functions lists the module's top-level functions, sorted.
func (m *module) Functions() []string {
	var out []string
	for name, v := range m.globals {
		if _, ok := v.(*starlark.Function); ok && !strings.HasPrefix(name, "_") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Call runs one function with converted arguments (D2) under the limits.
func (m *module) Call(ctx context.Context, fn string, args ...any) (any, error) {
	v, ok := m.globals[fn]
	if !ok {
		return nil, fmt.Errorf("%s has no function %q", m.path, fn)
	}
	sargs := make(starlark.Tuple, len(args))
	for i, a := range args {
		sv, err := toStarlark(a, 0, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: argument %d: %w", fn, i+1, err)
		}
		sargs[i] = sv
	}
	thread, done := m.e.thread(ctx, m.path)
	defer done()
	res, err := starlark.Call(thread, v, sargs, nil)
	if err != nil {
		// The render's own context ending is cancellation, which nothing
		// contains (E2). The per-call time limit is a limit like the step
		// count: an ordinary script error a boundary may contain (E4).
		if cerr := ctx.Err(); cerr != nil {
			return nil, fmt.Errorf("%w: %v", cerr, err)
		}
		if threadCtx(thread).Err() != nil {
			return nil, fmt.Errorf("%s: over the time limit (%s per call)", fn, m.e.opts.Timeout)
		}
		if ans := answerOf(err); ans != nil {
			// brick.not_found / redirect / error: the backtrace is noise,
			// the typed answer is what the host acts on.
			return nil, fmt.Errorf("%s: %w", fn, ans)
		}
		if errors.Is(err, brick.ErrHostOnly) {
			// A host-only stand-in was used: keep the sentinel so a check
			// can tell "not runnable here" from a broken script.
			var ee *starlark.EvalError
			if errors.As(err, &ee) {
				return nil, fmt.Errorf("%s: %w", scriptPos(ee), ee.Unwrap())
			}
			return nil, err
		}
		var ee *starlark.EvalError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("%s", strings.TrimSpace(ee.Backtrace()))
		}
		return nil, err
	}
	out, err := fromStarlark(res, 0)
	if err != nil {
		return nil, fmt.Errorf("%s returned %w", fn, err)
	}
	return out, nil
}

// checkSQL enforces D8 from the syntax tree: the SQL given to db.query and
// db.exec is a string literal or a module-level constant, so building SQL
// from strings does not compile. db.query_dynamic is the named escape.
func checkSQL(f *syntax.File) error {
	consts := map[string]bool{}
	for _, st := range f.Stmts {
		if a, ok := st.(*syntax.AssignStmt); ok && a.Op == syntax.EQ {
			id, isID := a.LHS.(*syntax.Ident)
			lit, isLit := a.RHS.(*syntax.Literal)
			if isID && isLit && lit.Token == syntax.STRING {
				consts[id.Name] = true
			}
		}
	}
	var err error
	syntax.Walk(f, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok || err != nil {
			return err == nil
		}
		dot, ok := call.Fn.(*syntax.DotExpr)
		if !ok {
			return true
		}
		recv, ok := dot.X.(*syntax.Ident)
		if !ok || recv.Name != "db" || !slices.Contains([]string{"query", "exec"}, dot.Name.Name) {
			return true
		}
		pos, _ := call.Span()
		if len(call.Args) == 0 {
			err = fmt.Errorf("%s: db.%s needs a SQL string", pos, dot.Name.Name)
			return false
		}
		switch a := call.Args[0].(type) {
		case *syntax.Literal:
			if a.Token == syntax.STRING {
				return true
			}
		case *syntax.Ident:
			if consts[a.Name] {
				return true
			}
		}
		err = fmt.Errorf("%s: db.%s takes fixed SQL — a string literal or a module-level constant, with ? for values; for SQL built at run time use db.query_dynamic (D8)", pos, dot.Name.Name)
		return false
	})
	return err
}

// answerOf returns the loader answer err carries (brick.ErrNotFound,
// *brick.Redirect, *brick.HTTPError), or nil for any other failure.
func answerOf(err error) error {
	var rd *brick.Redirect
	var he *brick.HTTPError
	switch {
	case errors.As(err, &rd):
		return rd
	case errors.As(err, &he):
		return he
	case errors.Is(err, brick.ErrNotFound):
		return brick.ErrNotFound
	}
	return nil
}
