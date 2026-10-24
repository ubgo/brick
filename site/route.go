package site

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ubgo/brick"
)

// Dynamic route segments: a page file or directory named
// [slug] matches one URL segment, [...rest] one or more, [[...rest]] zero or
// more. A static page always beats a pattern; among patterns the order is
// a total one (patternCmp), and two patterns with the same shape — they
// match exactly the same URLs — are an index error naming both files.

// pathsKey is `brick: {paths: ns.fn}`: the script function listing a
// pattern page's route values, for static export and a closed world in
// serve.
const pathsKey = "paths"

// segKind is what one URL segment of a pattern is. The order is the
// specificity order: a literal beats a parameter at the same position.
type segKind int

const (
	segLit     segKind = iota // a literal segment, "posts"
	segOne                    // [name]: exactly one segment
	segRest                   // [...name]: one or more segments, last only
	segOptRest                // [[...name]]: zero or more segments, last only
)

// segment is one parsed segment: the literal text, or the parameter name.
type segment struct {
	kind segKind
	text string
}

// pattern is one dynamic page in one locale.
type pattern struct {
	// route is what Routes lists: URL is the pattern ("/posts/[slug]/"),
	// Params its parameter names in order.
	route Route
	segs  []segment
	// paths is the brick: {paths:} reference; empty means an open world.
	paths string
}

// Parameter segment spellings. A name is an identifier, so it reads the
// same in a template (.Page.Route.slug) and in Starlark.
var (
	segOneRE     = regexp.MustCompile(`^\[([A-Za-z_][A-Za-z0-9_]*)\]$`)
	segRestRE    = regexp.MustCompile(`^\[\.\.\.([A-Za-z_][A-Za-z0-9_]*)\]$`)
	segOptRestRE = regexp.MustCompile(`^\[\[\.\.\.([A-Za-z_][A-Za-z0-9_]*)\]\]$`)
)

// isPatternURL reports whether a page URL (from its file path) has any
// dynamic segment.
func isPatternURL(u string) bool { return strings.ContainsAny(u, "[]") }

// parsePattern splits a pattern URL into segments, rejecting a bracket that
// is not a whole parameter segment, a catch-all before the end, and a name
// used twice.
func parsePattern(u string) ([]segment, error) {
	trimmed := strings.Trim(u, "/")
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, "/")
	segs := make([]segment, 0, len(parts))
	seen := map[string]bool{}
	for i, p := range parts {
		var sg segment
		switch {
		case !strings.ContainsAny(p, "[]"):
			sg = segment{kind: segLit, text: p}
		case segOneRE.MatchString(p):
			sg = segment{kind: segOne, text: segOneRE.FindStringSubmatch(p)[1]}
		case segRestRE.MatchString(p):
			sg = segment{kind: segRest, text: segRestRE.FindStringSubmatch(p)[1]}
		case segOptRestRE.MatchString(p):
			sg = segment{kind: segOptRest, text: segOptRestRE.FindStringSubmatch(p)[1]}
		default:
			return nil, fmt.Errorf("%q: a dynamic segment is a whole name, [name], [...name] or [[...name]], with name an identifier", p)
		}
		if sg.kind >= segRest && i != len(parts)-1 {
			return nil, fmt.Errorf("%q: a catch-all must be the last segment", p)
		}
		if sg.kind != segLit {
			if seen[sg.text] {
				return nil, fmt.Errorf("the parameter %q appears twice", sg.text)
			}
			seen[sg.text] = true
		}
		segs = append(segs, sg)
	}
	return segs, nil
}

// params lists a pattern's parameter names in order.
func params(segs []segment) []string {
	var out []string
	for _, s := range segs {
		if s.kind != segLit {
			out = append(out, s.text)
		}
	}
	return out
}

// shape is the set of URLs a pattern matches, as a string: two patterns with
// one shape are a conflict whatever their parameter names.
func shape(segs []segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteByte('/')
		switch s.kind {
		case segLit:
			b.WriteString(s.text)
		case segOne:
			b.WriteString("[]")
		case segRest:
			b.WriteString("[...]")
		case segOptRest:
			b.WriteString("[[...]]")
		}
	}
	return b.String()
}

// patternCmp is the match order. Segment by segment,
// left to right, the more specific kind wins at the first position where
// two patterns differ: a literal, then [name], then [...name], then
// [[...name]]. So /docs/[...path] takes /docs/a/ before /[section]/[slug]
// does (a literal first), and /[section]/[slug] takes /x/y/ before
// /[[...all]] (one segment before a catch-all). Where the kinds agree, the
// shorter pattern comes first, and the URL breaks the remaining ties so the
// order is deterministic. Patterns that tie on kinds and length but differ
// in a literal match disjoint URLs, so their order does not matter.
func patternCmp(a, b *pattern) int {
	for i := range min(len(a.segs), len(b.segs)) {
		if c := cmp.Compare(a.segs[i].kind, b.segs[i].kind); c != 0 {
			return c
		}
	}
	if c := cmp.Compare(len(a.segs), len(b.segs)); c != 0 {
		return c
	}
	return strings.Compare(a.route.URL, b.route.URL)
}

// orderPatterns sorts patterns into match order and refuses two with one
// shape, naming both files.
func orderPatterns(ps []*pattern) error {
	byShape := map[string]*pattern{}
	for _, p := range ps {
		k := shape(p.segs)
		if prev, ok := byShape[k]; ok {
			return &brick.Error{Code: CodeRoute, Err: fmt.Errorf("site: %s and %s match the same URLs (%s and %s); rename one", prev.route.Path, p.route.Path, prev.route.URL, p.route.URL)}
		}
		byShape[k] = p
	}
	slices.SortStableFunc(ps, patternCmp)
	return nil
}

// validSegment is the gate every route value passes, from a request or from
// a paths: list: a route value is one URL segment of text, never a path.
// It refuses the empty segment, "." and "..", any slash or backslash, and
// control characters, so a value can never name a file outside the
// content or export directory, whatever a loader or Export does with it.
func validSegment(v string) bool {
	if v == "" || v == "." || v == ".." || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// match tries the pattern against a URL: directory-style, already split
// into segments. It returns the route values and the catch-all's parts.
func (p *pattern) match(segs []string) (map[string]string, []string, bool) {
	vals := map[string]string{}
	for i, sg := range p.segs {
		switch sg.kind {
		case segLit:
			if i >= len(segs) || segs[i] != sg.text {
				return nil, nil, false
			}
		case segOne:
			if i >= len(segs) || !validSegment(segs[i]) {
				return nil, nil, false
			}
			vals[sg.text] = segs[i]
		case segRest, segOptRest:
			rest := segs[min(i, len(segs)):]
			if len(rest) == 0 && sg.kind == segRest {
				return nil, nil, false
			}
			for _, r := range rest {
				if !validSegment(r) {
					return nil, nil, false
				}
			}
			vals[sg.text] = strings.Join(rest, "/")
			return vals, slices.Clone(rest), true
		}
	}
	if len(segs) != len(p.segs) {
		return nil, nil, false
	}
	return vals, nil, true
}

// splitURL splits a directory-style URL into segments; ok is false for a
// URL without the trailing slash every page URL has.
func splitURL(u string) ([]string, bool) {
	if !strings.HasPrefix(u, "/") || !strings.HasSuffix(u, "/") {
		return nil, false
	}
	t := strings.Trim(u, "/")
	if t == "" {
		return nil, true
	}
	return strings.Split(t, "/"), true
}

// splitFileURL splits a file URL, /tags/go/feed.xml, into segments; ok is
// false for a URL with a trailing slash or none at the start.
func splitFileURL(u string) ([]string, bool) {
	if !strings.HasPrefix(u, "/") || strings.HasSuffix(u, "/") {
		return nil, false
	}
	return strings.Split(strings.TrimPrefix(u, "/"), "/"), true
}

// routeAt is the Route that answers u: its static route or the matched
// pattern's, or the zero Route when no page has u. Serve and Export read
// an output page's format and prolog from it, which a pattern URL does
// not have in st.routes.
func (st *state) routeAt(u string) Route {
	if r, ok := st.routes[u]; ok {
		return r
	}
	if m, ok := st.match(u); ok {
		return m.route(u)
	}
	return Route{}
}

// dynamicMatch is a URL matched by a pattern.
type dynamicMatch struct {
	pat   *pattern
	vals  map[string]string
	parts []string
}

// match finds the first pattern, in match order, that takes u. A page
// URL ends in a slash and only an HTML pattern takes it; a file URL
// (/tags/go/feed.xml) only a dynamic output page takes (v22), so neither
// kind answers for the other's spelling.
func (st *state) match(u string) (dynamicMatch, bool) {
	segs, ok := splitURL(u)
	file := false
	if !ok {
		if segs, ok = splitFileURL(u); !ok {
			return dynamicMatch{}, false
		}
		file = true
	}
	for _, p := range st.patterns {
		if (p.route.Output != "") != file {
			continue
		}
		if vals, parts, ok := p.match(segs); ok {
			return dynamicMatch{pat: p, vals: vals, parts: parts}, true
		}
	}
	return dynamicMatch{}, false
}

// has reports whether u is a page: a static route or a pattern match.
func (st *state) has(u string) bool {
	if _, ok := st.routes[u]; ok {
		return true
	}
	_, ok := st.match(u)
	return ok
}

// hasEncodedSeparator reports a %2F or %5C in an escaped request path: a
// slash smuggled into one segment. Such a request never reaches a pattern,
// so a route value cannot carry a separator in from the URL.
func hasEncodedSeparator(escaped string) bool {
	l := strings.ToLower(escaped)
	return strings.Contains(l, "%2f") || strings.Contains(l, "%5c")
}

// route is the Route a matched URL renders: the pattern's page at u.
func (m dynamicMatch) route(u string) Route {
	r := m.pat.route
	r.URL = u
	r.Params = nil
	return r
}

// pathEntries reads a pattern's paths: list and turns every entry into a
// URL, checking each value with validSegment. The URLs come back in the
// list's order, without duplicates.
func (s *Site) pathEntries(ctx context.Context, st *state, p *pattern) ([]string, error) {
	opts := []brick.RenderOption{}
	if p.route.Locale != "" {
		opts = append(opts, brick.WithLocale(p.route.Locale))
	}
	if p.route.Markdown {
		src, err := s.markdownSource(p.route.Path)
		if err != nil {
			return nil, err
		}
		opts = append(opts, brick.WithSource(src))
	}
	entries, err := st.eng.RoutePaths(ctx, p.route.Path, p.paths, opts...)
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for i, e := range entries {
		u, err := p.build(e)
		if err != nil {
			return nil, fmt.Errorf("site: %s: brick: paths: entry %d: %w", p.route.Path, i, err)
		}
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out, nil
}

// build writes the URL one paths: entry names. Every parameter needs a
// value; a catch-all takes a "a/b" string or a list of segments; a key the
// pattern does not have is an error, since it is a typo more often than not.
func (p *pattern) build(entry map[string]any) (string, error) {
	names := map[string]bool{}
	var b strings.Builder
	for _, sg := range p.segs {
		if sg.kind == segLit {
			b.WriteString("/" + sg.text)
			continue
		}
		names[sg.text] = true
		raw, ok := entry[sg.text]
		if !ok && sg.kind != segOptRest {
			return "", fmt.Errorf("no value for %q", sg.text)
		}
		var parts []string
		switch v := raw.(type) {
		case nil:
		case string:
			if sg.kind == segOne {
				parts = []string{v}
			} else if v != "" {
				parts = strings.Split(v, "/")
			}
		case []any:
			if sg.kind == segOne {
				return "", fmt.Errorf("%q is one segment, not a list", sg.text)
			}
			for _, x := range v {
				s, ok := x.(string)
				if !ok {
					return "", fmt.Errorf("%q: segments must be strings, not %T", sg.text, x)
				}
				parts = append(parts, s)
			}
		default:
			return "", fmt.Errorf("%q must be a string, not %T", sg.text, raw)
		}
		if len(parts) == 0 && sg.kind != segOptRest {
			return "", fmt.Errorf("no value for %q", sg.text)
		}
		for _, s := range parts {
			if !validSegment(s) {
				return "", fmt.Errorf("%q: %q is not one URL segment (empty, \".\", \"..\", a slash or a control character)", sg.text, s)
			}
			b.WriteString("/" + s)
		}
	}
	for k := range entry {
		if !names[k] {
			return "", fmt.Errorf("%q is not a parameter of %s", k, p.route.URL)
		}
	}
	if p.route.Output != "" {
		return b.String(), nil // a file URL: /tags/go/feed.xml
	}
	return b.String() + "/", nil
}

// allowed is the closed world: a pattern with paths:
// answers only for a URL its list names, without running the page.
func (s *Site) allowed(ctx context.Context, st *state, m dynamicMatch, u string) (bool, error) {
	if m.pat.paths == "" {
		return true, nil
	}
	urls, err := s.pathEntries(ctx, st, m.pat)
	if err != nil {
		return false, err
	}
	return slices.Contains(urls, u), nil
}
