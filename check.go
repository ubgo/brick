package brick

import (
	"cmp"
	"context"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/ubgo/brick/internal/transpile"
)

// Report is what CompileAll found.
type Report struct {
	// Diagnostics are every error and warning, sorted by file and line.
	Diagnostics []Diagnostic
	// Pages and Components are the files compiled in each role.
	Pages      []string
	Components []string
}

// Err returns the errors as an ErrorList, or nil when there are none.
func (r *Report) Err() error {
	var list ErrorList
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityError {
			list = append(list, &Error{File: d.File, Line: d.Line, Col: d.Col, Code: d.Code, Severity: d.Severity, Err: errors.New(d.Message)})
		}
	}
	if len(list) == 0 {
		return nil
	}
	return list
}

// CompileOption tunes CompileAll.
type CompileOption func(*compileOptions)

type compileOptions struct {
	isPage func(string) bool
}

// PageFilter decides which files under the directory are pages (A12). The
// default: every .html file that no other file imports with use:.
func PageFilter(isPage func(path string) bool) CompileOption {
	return func(o *compileOptions) { o.isPage = isPage }
}

// CompileAll compiles every page under dir, every component in the roots and
// every layout, and returns every problem at once — the engine behind a
// `brick check` gate. Each file is also dry-run once, so html/template's
// lazy escaper errors surface now rather than at a render (T10).
func (e *Engine) CompileAll(ctx context.Context, dir string, opts ...CompileOption) (*Report, error) {
	var co compileOptions
	for _, o := range opts {
		o(&co)
	}
	if e.opts.FS == nil {
		return nil, errNoFS
	}
	if err := checkDir(dir); err != nil {
		return nil, err
	}
	dir = fsDir(dir)
	var htmlFiles []string
	imported := map[string]bool{}
	err := fs.WalkDir(e.opts.FS, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, htmlExt) {
			return nil
		}
		htmlFiles = append(htmlFiles, p)
		if m, err := e.calleeMeta(p); err == nil {
			for _, u := range m.use {
				if u.Kind == useComponent {
					imported[u.Path] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	r := &Report{}
	seen := map[string]bool{}
	check := func(p string, kind fileKind) {
		if seen[p] {
			return
		}
		seen[p] = true
		c, err := e.compileFresh(ctx, p, kind)
		if err != nil {
			r.Diagnostics = append(r.Diagnostics, diagOf(err, p))
			return
		}
		if kind == kindComponent {
			r.Components = append(r.Components, p)
		} else if kind == kindPage {
			r.Pages = append(r.Pages, p)
		}
		r.Diagnostics = append(r.Diagnostics, e.dryRun(c)...)
		// Shape warnings (G15) are check-time only: nothing at render
		// reads a fields:/of: declaration.
		r.Diagnostics = append(r.Diagnostics, e.checkShapes(c)...)
		// Balance warnings read the same source the compile just read, so
		// a file that compiled always scans.
		if src, err := e.readFile(p); err == nil {
			r.Diagnostics = append(r.Diagnostics, e.htmlBalance(p, src)...)
		}
	}
	for _, p := range htmlFiles {
		isPage := !imported[p] && !isLocaleVariant(p)
		if co.isPage != nil {
			isPage = co.isPage(p)
		}
		if isPage {
			check(p, kindPage)
		} else {
			check(p, kindComponent)
		}
	}
	// Both walks' callbacks return nil for every entry (an unreadable one
	// is skipped, as a missing root is), so WalkDir has no error to report.
	for _, root := range e.opts.Roots {
		_ = fs.WalkDir(e.opts.FS, fsDir(root), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, htmlExt) {
				if _, _, ok := splitStem(strings.TrimSuffix(path.Base(p), htmlExt)); ok {
					check(p, kindComponent)
				}
			}
			return nil
		})
	}
	_ = fs.WalkDir(e.opts.FS, fsDir(e.opts.LayoutDir), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, htmlExt) {
			check(p, kindLayout)
		}
		return nil
	})
	r.Diagnostics = append(r.Diagnostics, e.Warnings()...)
	slices.SortStableFunc(r.Diagnostics, func(a, b Diagnostic) int {
		return cmp.Or(strings.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Col, b.Col))
	})
	r.Diagnostics = slices.Compact(r.Diagnostics)
	return r, nil
}

// isLocaleVariant reports whether a file is a language variant (C16), which
// is checked as the role of its shared file, never as its own page.
func isLocaleVariant(p string) bool {
	base := strings.TrimSuffix(path.Base(p), htmlExt)
	_, suffix, ok := strings.Cut(base, ".")
	if !ok {
		return false
	}
	_, valid := normalizeLocale(suffix)
	return valid
}

// compileFresh compiles a file without caching it: CompileAll streams, and
// keeps only diagnostics (E8).
func (e *Engine) compileFresh(ctx context.Context, p string, kind fileKind) (*compiled, error) {
	src, err := e.readFile(p)
	if err != nil {
		return nil, &Error{File: p, Code: CodeImport, Err: err}
	}
	return e.compile(ctx, p, src, kind)
}

// dryRun executes each template of a file once against a discard writer
// with an inert scope, reporting only html/template escaper errors: a valid
// template dry-runs silently (T10, proved by the spike).
func (e *Engine) dryRun(c *compiled) []Diagnostic {
	st := &renderState{e: e, ctx: context.Background(), out: &builder{}, dry: true, tags: map[string]bool{}, assets: map[string]bool{}, memo: map[string]any{}, seen: map[string]int{}, keys: map[string]bool{}, files: map[string]bool{}}
	st.page = &Page{Path: c.path, Params: map[string]any{}}
	st.pushCtx(nil)
	root := st.newScope(c, map[string]any{}, map[string]any{})
	var out []Diagnostic
	names := append([]string{c.path}, c.out.Defines...)
	for _, name := range names {
		if c.set.Lookup(name) == nil {
			continue
		}
		var data any = bodyScope{Root: root}
		if name == c.path || slices.ContainsFunc(c.out.Locals, func(l transpile.Local) bool { return l.Define == name }) {
			data = root
		}
		err := c.set.ExecuteTemplate(io.Discard, name, data)
		var te *template.Error
		if errors.As(err, &te) {
			d := diagOf(c.mapError(err, CodeSyntax), c.path)
			out = append(out, d)
		}
	}
	return out
}

// diagOf turns an error into a Diagnostic.
func diagOf(err error, file string) Diagnostic {
	var be *Error
	if errors.As(err, &be) {
		f := be.File
		if f == "" {
			f = file
		}
		return Diagnostic{File: f, Line: be.Line, Col: be.Col, Severity: SeverityError, Code: be.Code, Message: errMessage(be)}
	}
	return Diagnostic{File: file, Severity: SeverityError, Code: CodeRuntime, Message: err.Error()}
}

// DiagnosticOf turns err into a Diagnostic whose Message is bare (no
// "file:line:col:" prefix, which String adds), so a caller that merges
// diagnostics from several passes can compare them structurally. file
// stands in for an error with no location of its own.
func DiagnosticOf(err error, file string) Diagnostic { return diagOf(err, file) }

// errMessage is an Error's message without its location prefix.
func errMessage(e *Error) string {
	msg := ""
	if e.Component != "" {
		msg = "<" + e.Component + ">: "
	}
	if e.Err != nil {
		msg += e.Err.Error()
	}
	return msg
}
