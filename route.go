package brick

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ErrNotFound is what a loader returns (Starlark: brick.not_found()) when
// the record a dynamic route names does not exist. The render
// fails with it, unwrappable with errors.Is, and the host answers with its
// 404 page instead of an error page; the site layer also never caches the
// miss. It is not reported to OnError hooks: a missing record is an answer,
// not a fault.
var ErrNotFound = errors.New("brick: not found")

// ErrHostOnly is what a stand-in for a host capability returns when it is
// used: a script global or an upload sink that the site's own Go program
// supplies, declared in brick.yaml's host: section so the brick CLI can
// compile the site without it. A program that is not the host (brick check,
// brick lsp) cannot run the real thing, so using the stand-in fails with
// this, unwrappable with errors.Is, and site.Site.Check reports the page as
// a warning (CodeHostOnly) rather than an error: the page is not broken, it
// is only not runnable here.
var ErrHostOnly = errors.New("only the site's host program supplies this, so it does not run here")

// routeKey is the key under which a loader's `page` input carries the
// route values: input["page"]["route"]["slug"]. It shadows a
// frontmatter param of the same name, because the URL is the more specific
// source of the two.
const routeKey = "route"

// WithRoute gives the page the values a dynamic route matched:
// .Page.Route by name, and .Page.RouteParts for a catch-all's segments.
// The site layer sets it; a Go host with its own router may pass its own
// values the same way. The values are untrusted text, escaped like any data.
func WithRoute(params map[string]string, parts []string) RenderOption {
	return func(o *renderOptions) {
		o.route = maps.Clone(params)
		o.routeParts = append([]string(nil), parts...)
	}
}

// WithURL tells the page the URL it is rendered at (.Page.URL). The site
// layer sets it; a Go host with its own router passes its own. The value
// is untrusted text, escaped like any data.
func WithURL(u string) RenderOption {
	return func(o *renderOptions) { o.url = u }
}

// WithTranslations gives the page its other-language variants
// (.Page.Translations). The site layer pairs them from its routes; a Go
// host with its own router passes its own, in the order it wants them
// listed. The list is copied, so the caller may reuse its slice.
func WithTranslations(ts []Translation) RenderOption {
	return func(o *renderOptions) { o.translations = slices.Clone(ts) }
}

// loaderPage is a loader's `page` input: the page's frontmatter, plus the
// route values under routeKey when the page was matched by a pattern.
func loaderPage(p *Page) map[string]any {
	if p.Route == nil {
		return p.Params
	}
	out := maps.Clone(p.Params)
	if out == nil {
		out = map[string]any{}
	}
	route := make(map[string]any, len(p.Route))
	for k, v := range p.Route {
		route[k] = v
	}
	out[routeKey] = route
	return out
}

// RoutePaths calls the script function a dynamic page names in
// `brick: {paths: ns.fn}` and returns its entries: one map of route values
// per page to render. The site layer uses it for static export
// and for the closed world in serve; ref is that frontmatter value. opts
// matter only for WithSource (a markdown page) and WithLocale.
func (e *Engine) RoutePaths(ctx context.Context, pagePath, ref string, opts ...RenderOption) ([]map[string]any, error) {
	var ro renderOptions
	for _, o := range opts {
		o(&ro)
	}
	p, err := pagePathOf(pagePath)
	if err != nil {
		return nil, &Error{File: pagePath, Code: CodePath, Err: err}
	}
	var c *compiled
	if ro.hasSrc {
		c, err = e.compile(ctx, p, ro.src, kindPage)
	} else {
		c, err = e.load(ctx, p, kindPage, ro.locale)
	}
	if err != nil {
		return nil, asError(err, CodeSyntax, p)
	}
	ns, fn, qualified := strings.Cut(ref, ".")
	if !qualified {
		ns, fn = "", ref
	}
	mod, ok := c.scripts[ns]
	if !ok || mod == nil || !slices.Contains(mod.Functions(), fn) {
		return nil, &Error{File: p, Line: 1, Code: CodeScript, Err: fmt.Errorf("brick: paths: %q names no function of a use: script import or of this file's <brick:script>", ref)}
	}
	out, err := mod.Call(ctx, fn)
	if err != nil {
		return nil, &Error{File: p, Code: CodeScript, Err: fmt.Errorf("brick: paths: %s: %w", ref, err)}
	}
	list, ok := out.([]any)
	if !ok {
		return nil, &Error{File: p, Code: CodeScript, Err: fmt.Errorf("brick: paths: %s returned %T; it must return a list of dicts", ref, out)}
	}
	entries := make([]map[string]any, 0, len(list))
	for i, v := range list {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, &Error{File: p, Code: CodeScript, Err: fmt.Errorf("brick: paths: %s: entry %d is %T, not a dict of route values", ref, i, v)}
		}
		entries = append(entries, m)
	}
	return entries, nil
}
