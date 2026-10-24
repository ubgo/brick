package site

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/internal/frontmatter"
)

// cacheKey identifies a cached render. The locale is part of the URL
// already, but is kept explicit so a future per-request locale cannot
// collide.
type cacheKey struct {
	url    string
	locale string
	// marks is a render WithPageMarks, for serve (fragment navigation);
	// Render and Export never are, so their bytes stay what they were.
	marks bool
}

// ErrNotFound is returned by Render for a URL no page has.
var ErrNotFound = errors.New("site: no page at this URL")

// Render returns the page at url, from the cache when possible.
// Only a Result is cached, never bytes: pass WithNonce to Result.Bytes per
// response. A failed render is never cached.
//
// A URL no static page has may match a dynamic page; a
// pattern with brick: {paths:} answers ErrNotFound for a value its list
// does not name, without running the page. A loader's brick.ErrNotFound
// comes back as the error, and the miss is not cached.
func (s *Site) Render(ctx context.Context, url string) (*brick.Result, error) {
	res, _, err := s.render(ctx, s.current(), url, true)
	return res, err
}

// PerRequestCacheControl is the Cache-Control header of a brick:
// {per_request: true} page (v22) whose loaders set none: the page was
// rendered for one visitor, so no shared cache may keep it, and the
// browser asks again before reusing it. A page loader's own
// brick.header("Cache-Control", …) wins.
const PerRequestCacheControl = "private, no-cache"

// render is Render with a given build; closed is false when the caller
// already knows the URL is on its pattern's paths: list (Export). hit says
// the Result came from the cache (the Dev toolbar shows it).
func (s *Site) render(ctx context.Context, st *state, url string, closed bool) (*brick.Result, bool, error) {
	return s.renderFor(ctx, nil, st, url, closed)
}

// renderRequest is render for a request serve answers. A per-request page
// (brick: {per_request: true}, v22) is rendered with Options.Request's
// options, so its own loaders and body see the visitor (.Ctx), as only
// islands do on a cached page, and its loaders run on every request;
// every other page is the shared cached one, and the request reaches only
// its islands (Fill).
func (s *Site) renderRequest(r *http.Request, st *state, url string) (*brick.Result, bool, error) {
	return s.renderFor(r.Context(), r, st, url, true)
}

// renderFor is render with the request a per-request page is rendered for;
// req is nil outside serve (Render, Check), where such a page renders as
// an anonymous visitor would see it and is still never cached.
func (s *Site) renderFor(ctx context.Context, req *http.Request, st *state, url string, closed bool) (*brick.Result, bool, error) {
	r, static := st.routes[url]
	var m dynamicMatch
	if !static {
		var ok bool
		if m, ok = st.match(url); !ok {
			return nil, false, ErrNotFound
		}
		r = m.route(url)
	}
	key := cacheKey{url: url, locale: r.Locale, marks: req != nil && r.Output == ""}
	var res *brick.Result
	hit := false
	if !r.PerRequest {
		s.cacheMu.Lock()
		res, hit = s.cache[key]
		s.cacheMu.Unlock()
	}
	if hit {
		// Only a URL that rendered is cached, so a hit passed the closed
		// world when it was rendered.
		return res, true, nil
	}
	var extra []brick.RenderOption
	if !static {
		if closed {
			in, err := s.allowed(ctx, st, m, url)
			if err != nil {
				return nil, false, err
			}
			if !in {
				return nil, false, ErrNotFound
			}
		}
		extra = append(extra, brick.WithRoute(m.vals, m.parts))
	}
	if key.marks {
		// Served HTML pages mark where the page sits in its layout, so a
		// navigation whose layout matches gets only that region (v22).
		extra = append(extra, brick.WithPageMarks())
	}
	if r.PerRequest && req != nil && s.opts.Request != nil {
		extra = append(extra, s.opts.Request(req)...)
	}
	res, err := s.renderRoute(ctx, st, r, extra...)
	if r.PerRequest {
		if res != nil {
			perRequestHeaders(res)
		}
		if brick.IsAnswer(err) {
			// A not-found from its loader is per visitor too (the loader
			// saw who asked), so it is marked like a redirect or a status.
			err = visitorAnswer{err}
		}
		return res, false, err
	}
	if err != nil {
		return res, false, err
	}
	s.cacheMu.Lock()
	// Only store into the build it was rendered for: a rebuild in between
	// would otherwise let a stale page back in.
	if s.current() == st {
		s.cache[key] = res
	}
	s.cacheMu.Unlock()
	return res, false, nil
}

// perRequestHeaders gives a per-request page's Result its Cache-Control,
// unless a page or layout loader set one. The Result was made for this
// request alone, never cached, so changing its headers reaches no other
// response.
func perRequestHeaders(res *brick.Result) {
	if res.Headers.Get("Cache-Control") != "" {
		return
	}
	if res.Headers == nil {
		res.Headers = http.Header{}
	}
	res.Headers.Set("Cache-Control", PerRequestCacheControl)
}

// RenderNotFound renders content/404.html, if the project has one.
func (s *Site) RenderNotFound(ctx context.Context) (*brick.Result, bool, error) {
	st := s.current()
	for _, ext := range []string{extHTML, extMD} {
		p := path.Join(s.opts.ContentDir, notFoundStem+ext)
		if exists(s.opts.FS, p) {
			res, err := s.renderRoute(ctx, st, Route{URL: "/404/", Path: p, Markdown: ext == extMD})
			return res, true, err
		}
	}
	return nil, false, nil
}

// renderRoute renders one route with a given build; extra carries a
// dynamic route's values.
func (s *Site) renderRoute(ctx context.Context, st *state, r Route, extra ...brick.RenderOption) (*brick.Result, error) {
	opts := slices.Clone(extra)
	if r.Locale != "" {
		opts = append(opts, brick.WithLocale(r.Locale))
	}
	sr, static := st.routes[r.URL]
	static = static && sr.Path == r.Path
	opts = append(opts, brick.WithURL(r.URL), brick.WithTranslations(st.translationsOf(r, static)))
	if r.noLayout {
		opts = append(opts, brick.WithLayout("none"))
	}
	if r.prolog != "" {
		src, err := fs.ReadFile(s.opts.FS, r.Path)
		if err != nil {
			return nil, err
		}
		if doc, err := frontmatter.Parse(src); err == nil {
			_, rest := liftProlog(doc.Body)
			head := src[:len(src)-len(doc.Body)]
			opts = append(opts, brick.WithSource(append(append([]byte(nil), head...), rest...)))
		}
	}
	if r.Markdown {
		src, headings, err := s.markdownPage(r.Path)
		if err != nil {
			return nil, err
		}
		opts = append(opts, brick.WithSource(src), brick.WithHeadings(headings))
	}
	return st.eng.RenderChunks(ctx, r.Path, nil, opts...)
}

// markdownSource turns a .md page into a brick page: the same frontmatter,
// and the converted HTML as the body with every `{{` written as a literal,
// so markdown text is never template code.
func (s *Site) markdownSource(p string) ([]byte, error) {
	src, _, err := s.markdownPage(p)
	return src, err
}

// markdownPage is markdownSource plus the page's headings, for
// .Page.Headings (v14): reported by a backend that implements
// brick.MarkdownHeadings, empty for one that does not, never guessed by
// re-parsing the HTML.
func (s *Site) markdownPage(p string) ([]byte, []brick.Heading, error) {
	src, err := fs.ReadFile(s.opts.FS, p)
	if err != nil {
		return nil, nil, err
	}
	doc, err := frontmatter.Parse(src)
	if err != nil {
		return src, nil, nil // the engine reports the frontmatter error with its position
	}
	var html bytes.Buffer
	var headings []brick.Heading
	if mh, ok := s.opts.Engine.Markdown.(brick.MarkdownHeadings); ok {
		headings, err = mh.ConvertHeadings(doc.Body, &html)
	} else {
		err = s.opts.Engine.Markdown.Convert(doc.Body, &html)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("site: %s: %w", p, err)
	}
	head := src[:len(src)-len(doc.Body)]
	body := strings.ReplaceAll(html.String(), "{{", `{{ "{{" }}`)
	return append(append([]byte(nil), head...), body...), headings, nil
}

// evictFiles drops the cached pages that read any of paths and returns
// their URLs.
func (s *Site) evictFiles(paths []string) []string {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	var out []string
	for k, res := range s.cache {
		for _, p := range paths {
			if slices.Contains(res.Files, p) {
				delete(s.cache, k)
				out = append(out, k.url)
				break
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out) // a URL may have a marked and an unmarked entry
}

// InvalidateTags drops the cached pages whose render recorded any of tags
// (cache: {tags: …} or {{ cacheTag }}) — the hook for data that changes
// outside the project, such as a database behind a loader. It returns the
// dropped URLs and tells live-reloading browsers.
func (s *Site) InvalidateTags(tags ...string) []string {
	s.cacheMu.Lock()
	var out []string
	for k, res := range s.cache {
		for _, t := range tags {
			if slices.Contains(res.Tags, t) {
				delete(s.cache, k)
				out = append(out, k.url)
				break
			}
		}
	}
	s.cacheMu.Unlock()
	slices.Sort(out)
	out = slices.Compact(out) // a URL may have a marked and an unmarked entry
	if len(out) > 0 {
		s.live.publish(out)
	}
	return out
}

// clear drops every cached page.
func (s *Site) clear() {
	s.cacheMu.Lock()
	s.cache = map[cacheKey]*brick.Result{}
	s.cacheMu.Unlock()
}

// Check compiles the whole theme (brick's CompileAll), adds the index's
// diagnostics, and renders every route, so a page that fails only at render
// — a bad expression, a missing layout — is reported too.
func (s *Site) Check(ctx context.Context) ([]brick.Diagnostic, error) {
	st := s.current()
	rep, err := st.eng.CompileAll(ctx, s.opts.ContentDir)
	if err != nil {
		return nil, err
	}
	diags := append(slices.Clone(rep.Diagnostics), st.diags...)
	seen := map[string]bool{}
	for _, d := range diags {
		seen[diagKey(d)] = true
	}
	for _, u := range st.order {
		r := st.routes[u]
		if _, err := s.renderRoute(ctx, st, r); err != nil {
			if brick.IsAnswer(err) {
				continue // a loader's redirect or error status is an answer, not a broken page
			}
			d := brick.DiagnosticOf(err, r.Path)
			if errors.Is(err, brick.ErrHostOnly) {
				// The page uses a capability only the site's own program has
				// (brick.yaml host:): not broken, just not runnable here.
				d.Severity, d.Code = brick.SeverityWarning, brick.CodeHostOnly
				d.Message = "not rendered by this check: " + d.Message
			}
			if !seen[diagKey(d)] {
				seen[diagKey(d)] = true
				diags = append(diags, d)
			}
		}
	}
	slices.SortStableFunc(diags, func(a, b brick.Diagnostic) int {
		if c := strings.Compare(a.File, b.File); c != 0 {
			return c
		}
		return a.Line - b.Line
	})
	return diags, nil
}

// diagKey identifies a diagnostic by its structure (position, code and bare
// message), so the compile pass and the render pass agree on "the same error".
func diagKey(d brick.Diagnostic) string {
	return fmt.Sprintf("%s|%d|%d|%s|%s", d.File, d.Line, d.Col, d.Code, d.Message)
}

// isNotFound reports a URL with no page, or a loader's brick.ErrNotFound
// : both answer with the 404 page.
func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, brick.ErrNotFound)
}

// statusOf maps a render error to an HTTP status.
func statusOf(err error) int {
	if isNotFound(err) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}
