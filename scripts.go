package brick

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// errNoScripting explains a script with no Scripting engine configured.
var errNoScripting = errors.New("this file uses a script, but Options.Scripting is not set (see contrib/script-starlark)")

// scriptEntry compiles one module once per snapshot; every file that
// imports it shares it (D7).
type scriptEntry struct {
	once sync.Once
	mod  ScriptModule
	err  error
	// deps are the module and every file its load() statements reached.
	deps []string
}

// recordingResolver wraps the confined resolver and records each file it
// hands a script, so a page that uses the script depends on them too.
func (e *Engine) recordingResolver(file string, deps *[]string) func(string) (ScriptImport, error) {
	var mu sync.Mutex
	return func(p string) (ScriptImport, error) {
		imp, err := e.resolveScriptImport(file, p)
		if err == nil {
			mu.Lock()
			*deps = append(*deps, imp.Path)
			mu.Unlock()
		}
		return imp, err
	}
}

// loadScript compiles a `.star` module imported through `use:`.
func (e *Engine) loadScript(ctx context.Context, p string) (ScriptModule, []string, error) {
	if e.opts.Scripting == nil {
		return nil, nil, errNoScripting
	}
	e.mu.Lock()
	if e.scripts == nil {
		e.scripts = map[string]*scriptEntry{}
	}
	ent, ok := e.scripts[p]
	if !ok {
		ent = &scriptEntry{}
		e.scripts[p] = ent
	}
	e.mu.Unlock()
	ent.once.Do(func() {
		src, err := e.readFile(p)
		if err != nil {
			ent.err = err
			return
		}
		ent.deps = []string{p}
		ent.mod, ent.err = e.opts.Scripting.Compile(ctx, ScriptSource{Path: p, Src: src, Line: 1, Resolve: e.recordingResolver(p, &ent.deps)})
	})
	return ent.mod, ent.deps, ent.err
}

// compileInline compiles a file's <brick:script> block.
func (e *Engine) compileInline(ctx context.Context, file string, src []byte, line int, deps *[]string) (ScriptModule, error) {
	if e.opts.Scripting == nil {
		return nil, &Error{File: file, Line: line, Code: CodeScript, Err: errNoScripting}
	}
	mod, err := e.opts.Scripting.Compile(ctx, ScriptSource{Path: file + "#script", Src: src, Line: line, Resolve: e.recordingResolver(file, deps)})
	if err != nil {
		return nil, &Error{File: file, Line: line, Code: CodeScript, Err: err}
	}
	return mod, nil
}

// resolveScriptImport is the confined resolver behind load() (D7, X7): a
// literal ~/ path inside Options.FS, of an allowed type.
func (e *Engine) resolveScriptImport(file, p string) (ScriptImport, error) {
	fp, err := e.resolveFrom(file, p)
	if err != nil {
		return ScriptImport{}, err
	}
	ext := strings.ToLower(pathExt(fp))
	if _, custom := e.opts.DataFormats[ext]; custom {
		ext = extJSON // parsed as data below, through the host's parser
	}
	switch ext {
	case extStar:
		src, err := e.readFile(fp)
		if err != nil {
			return ScriptImport{}, err
		}
		return ScriptImport{Path: fp, Src: src}, nil
	case extJSON, extYAML, extYML, extTOML:
		v, err := e.loadData(fp)
		if err != nil {
			return ScriptImport{}, err
		}
		return ScriptImport{Path: fp, Data: v, IsData: true}, nil
	}
	return ScriptImport{}, fmt.Errorf("load(%q): only .star modules and .json/.yaml data can be loaded", p)
}

// scriptCaller is the template function behind a rewritten script call
// (T7): it takes `$` first, to reach the render's context and limits.
func scriptCaller(file, ns, fn string, mod ScriptModule) func(root any, args ...any) (any, error) {
	label := fn
	if ns != "" {
		label = ns + "." + fn
	}
	return func(root any, args ...any) (any, error) {
		s, err := scopeOf(root)
		if err != nil {
			return nil, err
		}
		if s.st.dry {
			return nil, nil
		}
		v, err := mod.Call(s.st.ctx, fn, args...)
		if err != nil {
			code := CodeScript
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				code = CodeCanceled
			}
			return nil, s.st.fail(&Error{File: file, Code: code, Err: fmt.Errorf("%s: %w", label, err)})
		}
		return v, nil
	}
}

// loaderTarget splits the file's loader — `load:`, or `brick: {load_many:}`
// for a batch loader — into its module and function.
func loaderTarget(c *compiled) (ScriptModule, string, error) {
	name, label := c.meta.load, keyLoad
	if c.meta.loadMany != "" {
		name, label = c.meta.loadMany, keyBrick+": "+brickLoadMany
	}
	ns, fn, qualified := strings.Cut(name, ".")
	if !qualified {
		ns, fn = "", name
	}
	mod, ok := c.scripts[ns]
	if !ok || mod == nil {
		if ns == "" {
			return nil, "", fmt.Errorf("%s: %s: this file has no <brick:script> defining it", label, fn)
		}
		return nil, "", fmt.Errorf("%s: %s: %q is not a use: script import", label, name, ns)
	}
	for _, f := range mod.Functions() {
		if f == fn {
			return mod, fn, nil
		}
	}
	return nil, "", fmt.Errorf("%s: the module has no function %q", label, fn)
}

// checkLoader fails compilation when load: or load_many names nothing
// .
func (e *Engine) checkLoader(c *compiled) error {
	if _, _, err := loaderTarget(c); err != nil {
		return &Error{File: c.path, Line: 1, Code: CodeScript, Err: err}
	}
	return nil
}

// runLoader calls a file's loader once per distinct input per render (D5).
// Its input is {props, page, ctx}; ctx holds only the file's inject: keys,
// so the dependency stays visible. A batch loader (load_many) that the
// render did not get to batch — inside a boundary, a slot or an island's
// root — is called here with a one-element list, so its answer never
// depends on whether batching happened.
func (st *renderState) runLoader(c *compiled, given []propValue) (map[string]any, error) {
	mod, fn, err := loaderTarget(c)
	if err != nil {
		return nil, st.fail(&Error{File: c.path, Code: CodeScript, Err: err})
	}
	input, key, err := st.loaderInput(c, fn, given)
	if err != nil {
		return nil, err
	}
	if v, ok := st.memo[key]; ok {
		m, _ := v.(map[string]any)
		return m, nil
	}
	if c.meta.loadMany != "" {
		if err := st.runBatch(c, mod, fn, []map[string]any{input}, []string{key}); err != nil {
			return nil, err
		}
		m, _ := st.memo[key].(map[string]any)
		return m, nil
	}
	st.stats.loader()
	out, err := mod.Call(st.ctx, fn, input)
	if err != nil {
		return nil, st.fail(&Error{File: c.path, Code: scriptErrCode(err), Err: fmt.Errorf("load: %s: %w", fn, err)})
	}
	m, ok := out.(map[string]any)
	if !ok {
		return nil, st.fail(&Error{File: c.path, Code: CodeScript, Err: fmt.Errorf("load: %s returned %T; a loader must return a dict", fn, out)})
	}
	st.memo[key] = m
	return m, nil
}

// runBatch calls a batch loader once with every input and memoises each
// answer under its input's key, so the components that asked find it there
// . The answer is a list in input order: inputs are dicts,
// which no script language can use as map keys, so position is the only
// pairing every implementation can honour.
func (st *renderState) runBatch(c *compiled, mod ScriptModule, fn string, inputs []map[string]any, keys []string) error {
	list := make([]any, len(inputs))
	for i, in := range inputs {
		list[i] = in
	}
	st.stats.batch(len(inputs))
	out, err := mod.Call(st.ctx, fn, list)
	if err != nil {
		return st.fail(&Error{File: c.path, Code: scriptErrCode(err), Err: fmt.Errorf("load_many: %s: %w", fn, err)})
	}
	got, ok := out.([]any)
	if !ok || len(got) != len(inputs) {
		return st.fail(&Error{File: c.path, Code: CodeScript, Err: fmt.Errorf("load_many: %s returned %s; a batch loader must return a list of %d dicts, one per input, in input order", fn, describeBatch(out), len(inputs))})
	}
	for i, v := range got {
		m, ok := v.(map[string]any)
		if !ok {
			return st.fail(&Error{File: c.path, Code: CodeScript, Err: fmt.Errorf("load_many: %s: item %d is %T; each item must be a dict", fn, i, v)})
		}
		st.memo[keys[i]] = m
	}
	return nil
}

// describeBatch names a wrong batch answer: its length when it is a list.
func describeBatch(v any) string {
	if l, ok := v.([]any); ok {
		return fmt.Sprintf("a list of %d", len(l))
	}
	return fmt.Sprintf("%T", v)
}

// scriptErrCode keeps a canceled render reported as canceled (E2), not as
// a script failure.
func scriptErrCode(err error) Code {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return CodeCanceled
	}
	return CodeScript
}

// loaderInput builds a loader's input and its memo key.
func (st *renderState) loaderInput(c *compiled, fn string, given []propValue) (map[string]any, string, error) {
	// The loader sees props as the component will: declared ones coerced,
	// defaults filled (D4).
	props := map[string]any{}
	for _, g := range given {
		v := g.value
		if d, ok := c.meta.props[g.name]; ok {
			cv, err := checkProp(d, v)
			if err != nil {
				return nil, "", st.fail(&Error{File: c.path, Code: CodeProp, Err: fmt.Errorf("prop %q: %w", g.name, err)})
			}
			v = cv
		}
		props[g.name] = v
	}
	for _, name := range c.meta.order {
		if d := c.meta.props[name]; d.HasDefault {
			if _, ok := props[name]; !ok {
				props[name] = d.Default
			}
		}
	}
	ctxVals := map[string]any{}
	all := (&Scope{st: st}).Ctx()
	for k, d := range c.meta.inject {
		if v, ok := all[k]; ok {
			ctxVals[k] = v
		} else if d.HasDefault {
			ctxVals[k] = d.Default
		}
	}
	input := map[string]any{"props": props, "page": loaderPage(st.page), "ctx": ctxVals}
	key, err := memoKey(c.hash, fn, input)
	if err != nil {
		return nil, "", st.fail(&Error{File: c.path, Code: CodeScript, Err: fmt.Errorf("load: %s: input cannot be converted: %w", fn, err)})
	}
	return input, key, nil
}

// memoKey is the loader memo key: module hash + function + canonical input
// (D5). encoding/json sorts map keys, which makes the encoding canonical.
func memoKey(hash, fn string, input map[string]any) (string, error) {
	var b bytes.Buffer
	b.WriteString(hash + "\x00" + fn + "\x00")
	if err := json.NewEncoder(&b).Encode(input); err != nil {
		return "", err
	}
	return b.String(), nil
}
