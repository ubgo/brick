package brick

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
)

// RenderOption is a per-render input (A1, C16, X5).
type RenderOption func(*renderOptions)

type renderOptions struct {
	// pageMarks is WithPageMarks: the page's own output is bracketed by
	// PageStartMark and PageEndMark (fragment navigation).
	pageMarks bool
	src       []byte
	hasSrc    bool
	locale    string
	nonce     string
	layout    string
	hasLayout bool
	provide   map[string]any
	// session is the WithSession id; staticState marks an export's render
	//.
	session     string
	staticState bool
	// query are the request's URL parameters (WithQuery);
	// queryOwners, made by Fill, records which component claimed each
	// parameter across the islands of one response, for the Dev warning.
	query       url.Values
	queryOwners map[string]string
	// route and routeParts are WithRoute's values.
	route      map[string]string
	routeParts []string
	// url and translations are WithURL's and WithTranslations' values.
	url          string
	translations []Translation
	// headings is WithHeadings' value.
	headings []Heading
	// stats is WithStats' target: Fill adds each island's counts to it.
	stats *RenderStats
	// formResume is WithFormResume's instance.
	formResume *FormResume
	// lazyInline is WithLazyInline; views is WithViewLog's log.
	lazyInline bool
	views      *ViewLog
	err        error
}

// WithSource supplies the page's bytes from elsewhere (a CMS, a database);
// the path passed to Render stays its identity for errors and hooks (A1).
// Such a page is compiled for that render and not cached (E8).
func WithSource(src []byte) RenderOption {
	return func(o *renderOptions) { o.src, o.hasSrc = src, true }
}

// WithLocale picks language variants of files (C16) and seeds `locale` in
// the root .Ctx. An invalid tag falls back to DefaultLocale with a warning.
func WithLocale(tag string) RenderOption {
	return func(o *renderOptions) { o.locale = tag }
}

// WithNonce sets the response's CSP nonce, written on every tag the engine
// emits and printed by {{ nonce }}. It must be base64 or base64url (X5).
func WithNonce(n string) RenderOption {
	return func(o *renderOptions) {
		if !nonceRE.MatchString(n) {
			o.err = errBadNonce
			return
		}
		o.nonce = n
	}
}

// PageStartMark and PageEndMark bracket the page's own output inside its
// innermost layout when the render asked for WithPageMarks. The site layer
// uses them for fragment navigation: a navigation whose layout already
// matches gets only this region. HTML comments, so a page that is shown
// as is renders the same.
const (
	PageStartMark = "<!--brick:page-->"
	PageEndMark   = "<!--/brick:page-->"
)

// WithPageMarks brackets the page's output with PageStartMark and
// PageEndMark. Without a layout there is nothing to strip, so no marks are
// written.
func WithPageMarks() RenderOption {
	return func(o *renderOptions) { o.pageMarks = true }
}

// WithLayout overrides the page's layout; "none" renders a fragment (C3).
func WithLayout(name string) RenderOption {
	return func(o *renderOptions) { o.layout, o.hasLayout = name, true }
}

// WithHeadings sets .Page.Headings: the page's headings, for a layout's
// table of contents. The site layer passes them for a markdown page.
func WithHeadings(h []Heading) RenderOption {
	return func(o *renderOptions) { o.headings = h }
}

// Provide seeds the root .Ctx, below any layout in precedence (C2).
func Provide(key string, v any) RenderOption {
	return func(o *renderOptions) {
		if o.provide == nil {
			o.provide = map[string]any{}
		}
		o.provide[key] = v
	}
}

// Render renders a page and returns final bytes. It is exactly RenderChunks
// then Result.Bytes (A7). In Dev mode a failed render returns the page with
// an inline error block AND the error; in Prod, nil and the error (E1).
func (e *Engine) Render(ctx context.Context, path string, props any, opts ...RenderOption) ([]byte, error) {
	r, err := e.RenderChunks(ctx, path, props, opts...)
	if r == nil {
		return nil, err
	}
	out, ferr := e.Fill(ctx, r, opts...)
	if err == nil {
		err = ferr
	}
	if out == nil && err != nil && e.opts.Mode == Dev {
		// A Dev-mode page with an error block still returns its bytes.
		out, _ = r.withoutIslands().Bytes(opts...)
	}
	return out, err
}

// RenderChunks renders a page to a cacheable Result (A7).
func (e *Engine) RenderChunks(ctx context.Context, pagePath string, props any, opts ...RenderOption) (*Result, error) {
	var ro renderOptions
	for _, o := range opts {
		o(&ro)
	}
	if ro.err != nil {
		return nil, ro.err
	}
	p, err := pagePathOf(pagePath)
	if err != nil {
		return nil, &Error{File: pagePath, Code: CodePath, Err: err}
	}
	var warnings []Diagnostic
	locale := e.opts.DefaultLocale
	if ro.locale != "" {
		if tag, ok := normalizeLocale(ro.locale); ok {
			locale = tag
		} else {
			warnings = append(warnings, Diagnostic{File: p, Severity: SeverityWarning, Code: CodeI18n, Message: fmt.Sprintf("WithLocale(%q) is not a language tag; using %q", ro.locale, locale)})
		}
	}

	var page *compiled
	if ro.hasSrc {
		page, err = e.compile(ctx, p, ro.src, kindPage)
	} else {
		page, err = e.load(ctx, p, kindPage, locale)
	}
	if err != nil {
		return e.failed(ctx, nil, asError(err, CodeSyntax, p))
	}
	return e.run(ctx, page, props, locale, ro, warnings, nil)
}

// pagePathOf accepts a page path with or without `~/`.
func pagePathOf(p string) (string, error) {
	if strings.HasPrefix(p, rootPrefix) {
		return resolvePath(p)
	}
	return resolvePath(rootPrefix + p)
}

// run renders a compiled page, its loader and its layout chain. body, when
// set, replaces the page's own template (used by RenderComponent).
func (e *Engine) run(ctx context.Context, page *compiled, props any, locale string, ro renderOptions, warnings []Diagnostic, _ func() error) (*Result, error) {
	if props == nil {
		// Nothing required: a page rendered without props reads .Props.x as
		// empty, the same rule as a missing frontmatter key.
		props = map[string]any{}
	}
	// The page's own loader headers land in Result.Headers, never in a
	// sink the caller put on ctx for Fill (that one is per response).
	headers := http.Header{}
	ctx = context.WithValue(ctx, headerSinkKey{}, &headerSink{h: headers})
	st := &renderState{e: e, ctx: ctx, out: &builder{limit: e.opts.MaxOutputBytes}, locale: locale, tags: map[string]bool{}, assets: map[string]bool{}, memo: map[string]any{}, warnings: warnings, seen: map[string]int{}, keys: map[string]bool{}, files: map[string]bool{}, session: ro.session, staticState: ro.staticState, lazyInline: ro.lazyInline, views: ro.views}
	st.liveOut = st.out
	params := cloneMap(page.meta.params)
	translations := ro.translations
	if translations == nil {
		translations = []Translation{}
	}
	headings := ro.headings
	if headings == nil {
		headings = []Heading{}
	}
	st.page = &Page{Path: page.path, Params: params, Props: props, Locale: locale, Route: ro.route, RouteParts: ro.routeParts, URL: ro.url, Translations: translations, Headings: headings}
	if v, ok := params["title"]; ok && v != nil {
		st.page.Title = fmt.Sprint(v)
	}

	root := map[string]any{}
	if locale != "" {
		root[ctxLocale] = locale
	}
	for k, v := range ro.provide {
		root[k] = v
	}
	st.pushCtx(root)
	st.markRequest(ro.provide)

	// A page's loader runs before its layout executes, so a layout sees a
	// loaded .Title (D4); frontmatter written in the file wins.
	if page.meta.load != "" {
		loaded, err := st.runLoader(page, nil)
		if err != nil {
			return e.failed(ctx, st, st.failure)
		}
		for _, k := range sortedKeys(loaded) {
			if _, ok := params[k]; !ok {
				params[k] = loaded[k]
			} else {
				st.warn(loaderShadowed(page.path, k))
			}
		}
		if v, ok := params["title"]; ok && v != nil {
			st.page.Title = fmt.Sprint(v)
		}
	}
	for _, tag := range page.meta.cacheTags {
		st.tags[tag] = true
	}

	for _, h := range e.hooks.before {
		if err := callHook(func() error { return h.BeforeRender(ctx, st.page) }); err != nil {
			return e.failed(ctx, st, &Error{File: page.path, Code: CodeHook, Err: fmt.Errorf("%T: %w", h, err)})
		}
	}

	chain, err := e.layoutChain(ctx, page, ro, locale)
	if err != nil {
		return e.failed(ctx, st, asError(err, CodeLayout, page.path))
	}

	// A page's or layout's own <brick:style> and <brick:client> ship like a
	// component's: one-off CSS for a landing page belongs in that page.
	for _, f := range append([]*compiled{page}, chain...) {
		if len(f.client) > 0 || len(f.styles) > 0 {
			st.assets[f.path] = true
		}
		st.needRuntime = st.needRuntime || f.methodLinks
	}

	pageScope := st.newScope(page, params, props)
	pageScope.inst = &instance{name: page.path, nodes: map[string]func(any) error{}}
	run := func() error {
		st.pushCtx(page.meta.provide)
		defer st.popCtx()
		return st.exec(page, page.path, pageScope)
	}
	for i, lay := range chain {
		inner, l := run, lay
		if i == 0 && ro.pageMarks {
			// The innermost layout's children are the page itself.
			page := inner
			inner = func() error {
				if _, err := io.WriteString(st.out, PageStartMark); err != nil {
					return err
				}
				if err := page(); err != nil {
					return err
				}
				_, err := io.WriteString(st.out, PageEndMark)
				return err
			}
		}
		run = func() error {
			// In a layout .Params and the promoted keys are the page's; the
			// layout's own frontmatter is its .Props (C3).
			scope := st.newScope(l, params, cloneMap(l.meta.params))
			if l.meta.load != "" {
				loaded, err := st.runLoader(l, nil)
				if err != nil {
					return err
				}
				lp := cloneMap(l.meta.params)
				for _, k := range sortedKeys(loaded) {
					if _, ok := lp[k]; !ok {
						lp[k] = loaded[k]
					} else {
						st.warn(loaderShadowed(l.path, k))
					}
				}
				scope.Props = lp
			}
			scope.inst = &instance{name: l.path, hasChildren: true, nodes: map[string]func(any) error{}, children: func() error {
				st.depth++
				st.pushCtx(nil)
				defer func() {
					st.popCtx()
					st.depth--
				}()
				return inner()
			}}
			for _, tag := range l.meta.cacheTags {
				st.tags[tag] = true
			}
			st.pushCtx(l.meta.provide)
			defer st.popCtx()
			return st.exec(l, l.path, scope)
		}
	}
	err = run()
	if err == nil {
		// Batch-loaded components render now, one load_many per level
		//.
		err = st.drain()
	}
	if err != nil {
		cause := st.failure
		if cause == nil {
			cause = asError(err, CodeRuntime, page.path)
		}
		return e.failed(ctx, st, cause)
	}
	r, err := e.finish(ctx, st, page)
	if r != nil && len(headers) > 0 {
		r.Headers = headers
	}
	return r, err
}

// finish builds the Result and runs AfterRender hooks.
func (e *Engine) finish(ctx context.Context, st *renderState, page *compiled) (*Result, error) {
	r := st.result()
	if e.opts.Mode == Dev {
		// X6: html/template replaced an unsafe URL; say so instead of
		// shipping a silent #ZgotmplZ.
		for _, c := range r.Chunks {
			if bytes.Contains(c.Bytes, []byte("ZgotmplZ")) {
				r.Warnings = append(r.Warnings, Diagnostic{File: page.path, Severity: SeverityWarning, Code: CodeZgotmplZ, Message: "html/template replaced an unsafe value with #ZgotmplZ (for example a javascript: URL); the value came from data"})
				break
			}
		}
	}
	if len(r.assets) > 0 && !slices.Contains(markerKinds(r.Chunks), ChunkAssets) {
		r.Warnings = append(r.Warnings, Diagnostic{File: page.path, Severity: SeverityWarning, Code: CodeAssets, Message: "a component on this page has <brick:client> or <brick:style>, but no {{ assets }} was rendered, so its JS and CSS never load"})
	}
	for _, h := range e.hooks.after {
		before := markerKinds(r.Chunks)
		if err := callHook(func() error { return h.AfterRender(ctx, st.page, r) }); err != nil {
			return e.failed(ctx, st, &Error{File: page.path, Code: CodeHook, Err: fmt.Errorf("%T: %w", h, err)})
		}
		if !slices.Equal(before, markerKinds(r.Chunks)) {
			return e.failed(ctx, st, &Error{File: page.path, Code: CodeHook, Err: fmt.Errorf("%T changed marker chunks; AfterRender may change static chunks only (A9)", h)})
		}
	}
	return r, nil
}

// markerKinds lists the non-bytes chunks, in order.
func markerKinds(cs []Chunk) []ChunkKind {
	var out []ChunkKind
	for _, c := range cs {
		if c.Kind != ChunkBytes {
			out = append(out, c.Kind)
		}
	}
	return out
}

// result assembles a Result from the render state.
func (st *renderState) result() *Result {
	st.linkGlobals()
	tags := sortedKeys(st.tags)
	r := &Result{Chunks: st.out.result(), Types: slices.Clone(st.types), Tags: tags, Errors: slices.Clone(st.errors), Warnings: slices.Clone(st.warnings), Files: sortedKeys(st.files), page: st.page, locale: st.locale}
	r.Stats = st.stats.stats(r.Chunks)
	for _, p := range sortedKeys(st.assets) {
		r.assets = append(r.assets, st.e.assetsFor(p)...)
	}
	if st.e.opts.Navigation || st.needRuntime || slices.ContainsFunc(r.assets, func(a asset) bool { return !a.CSS }) {
		r.assets = append(slices.Clone(st.e.runtimeAssets()), r.assets...)
	}
	return r
}

// linkGlobals adds, for every library any of whose files this render read
// (a used component, even one with no CSS of its own),
// the library's global components (manifest `global:`): their CSS and JS
// are linked and their files recorded as dependencies, but their templates
// are never rendered. A global that fails to compile is skipped; the error
// surfaces wherever the component is used directly.
func (st *renderState) linkGlobals() {
	for _, p := range st.e.withGlobals(sortedKeys(st.files)) {
		if st.assets[p] || st.files[p] {
			continue
		}
		c, err := st.e.load(st.ctx, p, kindComponent, st.locale)
		if err != nil {
			continue
		}
		st.files[p] = true
		if len(c.client) > 0 || len(c.styles) > 0 {
			st.assets[p] = true
		}
	}
}

// failed reports a failed render: Dev returns the output so far with an
// inline error block AND the error; Prod returns only the error (E1).
func (e *Engine) failed(ctx context.Context, st *renderState, err *Error) (*Result, error) {
	if err == nil {
		err = &Error{Code: CodeRuntime, Err: errors.New("render failed")}
	}
	if !IsAnswer(err) {
		// A loader's answer (not found, redirect, error status) is not a
		// fault, so OnError hooks never see it.
		e.reportError(ctx, err)
	}
	if e.opts.Mode != Dev {
		return nil, err
	}
	r := &Result{}
	if st != nil {
		r = st.result()
	}
	r.Chunks = append(r.Chunks, Chunk{Kind: ChunkBytes, Bytes: []byte(errorBlock(tailOf(r.Chunks), err))})
	return r, err
}

// tailBytes is how much trailing output decides an error block's context.
const tailBytes = 4096

// tailOf returns the last bytes of rendered output.
func tailOf(cs []Chunk) []byte {
	var out []byte
	for i := len(cs) - 1; i >= 0 && len(out) < tailBytes; i-- {
		if cs[i].Kind == ChunkBytes {
			out = append(slices.Clone(cs[i].Bytes), out...)
		}
	}
	return out
}

// rawTextOpenRE finds the raw-text elements whose content is not HTML.
var rawTextOpenRE = regexp.MustCompile(`(?i)<(/?)(script|style|textarea|title)\b`)

// inTextContext reports whether output ending in tail is in HTML body text:
// not inside a tag and not inside a raw-text element (E7).
func inTextContext(tail []byte) bool {
	if bytes.LastIndexByte(tail, '<') > bytes.LastIndexByte(tail, '>') {
		return false
	}
	open := false
	for _, m := range rawTextOpenRE.FindAllSubmatch(tail, -1) {
		open = len(m[1]) == 0
	}
	return !open
}

// errorBlock is the Dev-mode inline error display (E7): a visible block in
// HTML body text; elsewhere — inside a tag or a script — an HTML comment,
// which cannot break the surrounding markup. The error is returned too.
func errorBlock(tail []byte, err error) string {
	if !inTextContext(tail) {
		return "<!-- brick error: " + strings.ReplaceAll(err.Error(), "--", "- -") + " -->"
	}
	return `<pre class="brick-error" style="background:#fff1f0;color:#a8071a;border:1px solid #ffa39e;padding:12px;white-space:pre-wrap;font:13px/1.5 monospace">` + template.HTMLEscapeString(err.Error()) + `</pre>`
}

// errorID is a short, stable reference for an error shown in Prod (X4).
func errorID(err error) string {
	sum := sha256.Sum256([]byte(err.Error()))
	return hex.EncodeToString(sum[:])[:hashLen]
}

// callHook runs a hook, turning a panic into an error with code panic (E1).
func callHook(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &Error{Code: CodePanic, Err: fmt.Errorf("panic: %v", r)}
		}
	}()
	return fn()
}

// layoutChain resolves a page's layouts, innermost first (C3). Precedence:
// render option → frontmatter → layouts/default.html. The implicit default
// never applies to a layout, and a cycle is an error showing the chain.
func (e *Engine) layoutChain(ctx context.Context, page *compiled, ro renderOptions, locale string) ([]*compiled, error) {
	name, explicit := page.meta.layout, page.meta.hasLay
	if ro.hasLayout {
		name, explicit = ro.layout, true
	}
	if name == layoutNone {
		return nil, nil
	}
	if !explicit {
		if e.opts.FS == nil {
			return nil, nil
		}
		def := path.Join(fsDir(e.opts.LayoutDir), defaultLayoutName+htmlExt)
		if ok, _ := e.exists(def); !ok {
			return nil, nil
		}
		name = defaultLayoutName
	}
	var chain []*compiled
	seen := []string{page.path}
	for name != "" && name != layoutNone {
		p, err := e.layoutPath(name)
		if err != nil {
			return nil, &Error{File: seen[len(seen)-1], Line: 1, Code: CodeLayout, Err: err}
		}
		if slices.Contains(seen, p) {
			return nil, &Error{File: page.path, Code: CodeLayout, Err: fmt.Errorf("layout cycle: %s", strings.Join(append(seen, p), " → "))}
		}
		seen = append(seen, p)
		l, err := e.load(ctx, p, kindLayout, locale)
		if err != nil {
			return nil, err
		}
		chain = append(chain, l)
		name = l.meta.layout
	}
	return chain, nil
}

// layoutPath maps a layout name or ~/ path to a file.
func (e *Engine) layoutPath(name string) (string, error) {
	var p string
	if strings.HasPrefix(name, rootPrefix) {
		fp, err := resolvePath(name)
		if err != nil {
			return "", fmt.Errorf("layout: %w", err)
		}
		p = fp
	} else {
		fp, err := resolvePath(rootPrefix + path.Join(fsDir(e.opts.LayoutDir), name) + htmlExt)
		if err != nil || strings.Contains(name, "..") {
			return "", fmt.Errorf("layout: %q is not a layout name", name)
		}
		p = fp
	}
	if ok, err := e.exists(p); err != nil || !ok {
		return "", fmt.Errorf("layout %q: %s does not exist", name, displayPath(p))
	}
	return p, nil
}

// RenderComponent renders one component alone, for tests and previews
// (A10). children is brick markup for the component's body — trusted code,
// like any file — and props are passed as expressions, so they arrive with
// their Go types. No layout applies.
func (e *Engine) RenderComponent(ctx context.Context, name string, props map[string]any, children string, opts ...RenderOption) ([]byte, error) {
	if !isComponentStem(name) {
		return nil, &Error{Code: CodeRender, Err: fmt.Errorf("RenderComponent: %q is not a component name", name)}
	}
	var b strings.Builder
	b.WriteString("<" + name)
	for _, k := range sortedKeys(props) {
		if !validAttrName(k) || strings.ContainsAny(k, "`") {
			return nil, &Error{Code: CodeProp, Err: fmt.Errorf("RenderComponent: %q is not a prop name", k)}
		}
		fmt.Fprintf(&b, " :%s=\"index .Props `%s`\"", k, k)
	}
	if children == "" {
		b.WriteString(" />")
	} else {
		b.WriteString(">" + children + "</" + name + ">")
	}
	opts = append(slices.Clone(opts), WithSource([]byte(b.String())), WithLayout(layoutNone))
	return e.Render(ctx, "_component/"+name+htmlExt, props, opts...)
}

// Assets returns the component modules and stylesheets as an fs.FS, for a
// server to mount at Options.AssetBase (A8).
func (e *Engine) Assets() fs.FS {
	return assetFS{e: e}
}

// loaderShadowed is the warning for a key both the file's frontmatter and
// its load: function produced. Frontmatter wins (D4), so the loader's value
// is dropped; saying so keeps precedence from being silent.
func loaderShadowed(file, key string) Diagnostic {
	return Diagnostic{File: file, Code: CodeShadow, Message: fmt.Sprintf("frontmatter %q shadows the key of the same name from this file's load: function; the frontmatter value is used", key)}
}
