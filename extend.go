package brick

import (
	"context"
	"fmt"
	"io"
	"reflect"
)

// Scripting compiles script modules: `.star` files imported through `use:`
// and inline `<brick:script>` blocks. The render path never names
// a scripting language; contrib/script-starlark implements this.
//
// Invariant: a module is compiled from bytes brick read. A script never
// chooses a file while running (D7): every load() it makes is resolved
// through ScriptSource.Resolve before the module runs.
type Scripting interface {
	Compile(ctx context.Context, src ScriptSource) (ScriptModule, error)
}

// ScriptSource is one module to compile.
type ScriptSource struct {
	// Path identifies the module in errors: an FS path, or a page path with
	// a `#script` suffix for an inline block.
	Path string
	Src  []byte
	// Line is the file line where Src starts, so errors point at the file.
	Line int
	// Resolve returns a file named by a load() statement. The path must
	// start with ~/ and stay inside Options.FS (X7, X2); a data file comes
	// back parsed, a module as bytes.
	Resolve func(path string) (ScriptImport, error)
}

// ScriptImport is a file a module loaded.
type ScriptImport struct {
	Path string
	// Src is a module's source (for a .star file).
	Src []byte
	// Data is a parsed data file (for .json, .yaml or .toml); IsData is set.
	Data   any
	IsData bool
}

// ScriptModule is a compiled module. Call converts Go values in and out
// (D2) and must honour ctx cancellation and the step and time limits (D6).
type ScriptModule interface {
	// Functions lists the module's callable top-level functions.
	Functions() []string
	Call(ctx context.Context, fn string, args ...any) (any, error)
}

// Markdown converts markdown for the built-in <Markdown> component (E10).
// contrib/markdown-goldmark implements it.
type Markdown interface {
	Convert(src []byte, w io.Writer) error
}

// Hook interfaces. A hook implements one or more; New rejects a value that
// implements none, so a misspelled method never becomes a hook that
// silently never fires.
type (
	// BeforeRenderHook runs before a page renders; an error stops it.
	BeforeRenderHook interface {
		BeforeRender(ctx context.Context, page *Page) error
	}
	// AfterRenderHook runs after a page renders and may change its static
	// chunks (A9). Marker chunks must stay as they are.
	AfterRenderHook interface {
		AfterRender(ctx context.Context, page *Page, r *Result) error
	}
	// CompileHook observes each compiled file, once per snapshot.
	CompileHook interface {
		OnCompile(ctx context.Context, f *CompiledFile)
	}
	// ErrorHook is told of every error, including ones a boundary contained.
	ErrorHook interface {
		OnError(ctx context.Context, err error)
	}
	// InstanceEndHook (stateend.go) is told when a stateful instance ends.
)

// CompiledFile is what a CompileHook sees about a file.
type CompiledFile struct {
	Path string
	// Components are the tag names the file uses, in source order.
	Components []string
	Params     map[string]any
}

// hookSet is the hooks, split by interface, in registration order.
type hookSet struct {
	before  []BeforeRenderHook
	after   []AfterRenderHook
	compile []CompileHook
	onError []ErrorHook
	onEnd   []InstanceEndHook
}

func newHookSet(hooks []any) (hookSet, error) {
	var hs hookSet
	for _, h := range hooks {
		matched := false
		if x, ok := h.(BeforeRenderHook); ok {
			hs.before, matched = append(hs.before, x), true
		}
		if x, ok := h.(AfterRenderHook); ok {
			hs.after, matched = append(hs.after, x), true
		}
		if x, ok := h.(CompileHook); ok {
			hs.compile, matched = append(hs.compile, x), true
		}
		if x, ok := h.(ErrorHook); ok {
			hs.onError, matched = append(hs.onError, x), true
		}
		if x, ok := h.(InstanceEndHook); ok {
			hs.onEnd, matched = append(hs.onEnd, x), true
		}
		if !matched {
			return hookSet{}, fmt.Errorf("brick: Options.Hooks: %s implements no hook interface (BeforeRender, AfterRender, OnCompile, OnError, OnInstanceEnd)", reflect.TypeOf(h))
		}
	}
	return hs, nil
}

// reportError tells every OnError hook, recovering a panicking hook so a
// reporter can never fail a render.
func (e *Engine) reportError(ctx context.Context, err error) {
	for _, h := range e.hooks.onError {
		func() {
			defer func() { _ = recover() }()
			h.OnError(ctx, err)
		}()
	}
}
