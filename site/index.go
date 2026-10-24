package site

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/internal/frontmatter"
)

// Page is one entry of the page index, and of .Site.Pages.
type Page struct {
	// Path is the file, e.g. content/blog/hello.md.
	Path string
	// URL is the page's URL in the default locale (or in its own locale for
	// a page that exists only as a language variant).
	URL         string
	Title       string
	Description string
	Date        time.Time
	Draft       bool
	// Params is the page's frontmatter.
	Params map[string]any
	// Section is the first directory under the content dir ("blog"), or
	// empty for a top-level page.
	Section string
	// Locale is set for a page that exists only as a variant (about.fr.html
	// with no about.html); shared pages leave it empty.
	Locale string
	// Markdown is true for a .md page.
	Markdown bool
	// Output is the format of an output page (content/feed.xml.html →
	// "xml"), which renders the file at its exact URL (/feed.xml) with no
	// layout unless it names one; "" for an ordinary page (v14).
	Output string `json:",omitempty"`
	// layout says the frontmatter names a layout; an output page gets none
	// otherwise.
	layout bool
	// noSitemap leaves the page out of the built-in sitemap: brick:
	// {sitemap: false}.
	noSitemap bool
	// prolog is an output page's leading <?xml …?> instructions, written
	// verbatim before its rendered bytes (liftProlog).
	prolog string
	// perRequest is brick: {per_request: true} (v22): rendered for every
	// request, never cached, skipped by Export.
	perRequest bool
	// variants holds the language-variant files of a shared page
	// (content/about.fr.html for content/about.html), so route can name
	// the file a locale's URL actually renders.
	variants map[string]bool
}

// Route is one URL and what renders it.
type Route struct {
	URL    string
	Path   string
	Locale string
	// Markdown is true when Path is a .md page.
	Markdown bool
	// Params names a dynamic page's parameters, in URL order; URL is then
	// the pattern, e.g. /posts/[slug]/. Empty for a static page.
	Params []string `json:",omitempty"`
	// Output is an output page's format ("xml", "txt"): the URL is a file
	// name, the response's Content-Type is the format's, and no HTML is
	// injected into it. Empty for an HTML page, so it is also the
	// output-page flag.
	Output string `json:",omitempty"`
	// Dynamic is true for a pattern route (Patterns): URL holds [param]
	// segments and Params names them.
	Dynamic bool `json:",omitempty"`
	// Title, Description, Date, Draft and Section are the page's, as in
	// .Site.Pages, so a host builds a menu, a sitemap or an admin list
	// from Routes without a second lookup.
	Title       string    `json:",omitempty"`
	Description string    `json:",omitempty"`
	Date        time.Time `json:",omitzero"`
	Draft       bool      `json:",omitempty"`
	Section     string    `json:",omitempty"`
	// Meta is every top-level frontmatter key except the engine's brick:
	// map. Unlike .Params at render time it still holds the engine's own
	// top-level keys (layout, use, load, props, state, ...), so a host
	// that wants only author keys must filter those out. Route names, menu entries and other host
	// metadata are ordinary keys here; the
	// engine adds no route keys of its own. Routes and Patterns return a
	// copy, so changing it never changes the index.
	Meta map[string]any `json:",omitempty"`
	// page is the index Page's Path this route belongs to: Path names the
	// file rendered (a language variant when the locale has one), page
	// the shared page whose flags (draft, sitemap) and dates apply.
	page string
	// noLayout renders the route with WithLayout("none"): an output page
	// whose frontmatter names no layout.
	noLayout bool
	// prolog is the output page's lifted XML prolog (liftProlog).
	prolog string
	// PerRequest is the page's brick: {per_request: true} (v22): the route
	// is rendered for every request with that request's options and never
	// stored in the page cache (render), and Export skips it. A host that
	// caches pages itself must skip these too.
	PerRequest bool `json:",omitempty"`
}

// Files and keys the index treats specially.
const (
	extHTML = ".html"
	extMD   = ".md"
	// indexStem is the page that stands for its directory.
	indexStem = "index"
	// notFoundStem is the page served for unknown URLs.
	notFoundStem = "404"
	// collectionFile declares a section's frontmatter schema.
	collectionFile = "_collection.yaml"
	// collectionFields is the schema's key in collectionFile.
	collectionFields = "fields"
	// engineKey and urlKey spell the URL override `brick: {url: …}`.
	engineKey = "brick"
	urlKey    = "url"
	// sitemapKey is `brick: {sitemap: false}`, a page's opt-out from the
	// built-in sitemap.
	sitemapKey = "sitemap"
	// perRequestKey is `brick: {per_request: true}` (v22): a page rendered
	// per visitor instead of from the page cache.
	perRequestKey = "per_request"
	// layoutKey is the frontmatter's layout key (A13), read to know whether
	// an output page asked for a layout.
	layoutKey = "layout"
)

// Diagnostic codes the site layer adds to brick's.
const (
	CodeCollection brick.Code = "collection" // frontmatter breaks its section's schema
	CodeSkipped    brick.Code = "skipped"    // a file that could not be a page
	CodeRoute      brick.Code = "route"      // a URL that two pages claim
)

// dynamicPage is a page whose file path has a [param] segment: it is not
// in the page index (it has no one URL) and is routed by pattern.
type dynamicPage struct {
	page  Page
	paths string
}

// index walks the content directory and builds the page list, and the
// list of dynamic pages beside it.
func index(o Options) ([]Page, []dynamicPage, []brick.Diagnostic, error) {
	var files []string
	imported := map[string]bool{}
	docs := map[string]frontmatter.Doc{}
	var diags []brick.Diagnostic
	err := fs.WalkDir(o.FS, o.ContentDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if p != o.ContentDir && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		switch path.Ext(p) {
		case extHTML:
		case extMD:
			if o.Engine.Markdown == nil {
				diags = append(diags, brick.Diagnostic{File: p, Severity: brick.SeverityWarning, Code: CodeSkipped, Message: "a .md page needs brick.Options.Markdown (for example contrib/markdown-goldmark); skipped"})
				return nil
			}
		default:
			return nil
		}
		src, err := fs.ReadFile(o.FS, p)
		if err != nil {
			return err
		}
		doc, err := frontmatter.Parse(src)
		if err != nil {
			// Keep the page: rendering it reports the error where it is.
			diags = append(diags, brick.Diagnostic{File: p, Severity: brick.SeverityError, Code: brick.CodeFrontmatter, Message: err.Error()})
		}
		docs[p] = doc
		files = append(files, p)
		if use, ok := doc.Params["use"].(map[string]any); ok {
			for _, v := range use {
				if s, ok := v.(string); ok && strings.HasSuffix(s, extHTML) && strings.HasPrefix(s, "~/") {
					imported[strings.TrimPrefix(s, "~/")] = true
				}
			}
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, diags, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	var dynamic []dynamicPage
	shared := map[string]bool{}
	for _, f := range files {
		if stem, loc := variantOf(f); loc == "" {
			shared[stem] = true
		}
	}
	var pages []Page
	variantFiles := map[string]map[string]bool{}
	for _, f := range files {
		if stem, loc := variantOf(f); loc != "" && shared[stem] {
			if variantFiles[stem] == nil {
				variantFiles[stem] = map[string]bool{}
			}
			variantFiles[stem][f] = true
		}
	}
	for _, f := range files {
		if imported[f] {
			continue // a component that sits beside its pages (A12)
		}
		stem, loc := variantOf(f)
		if loc != "" && shared[stem] {
			continue // a variant of a shared page, chosen at render (C16)
		}
		if stem == path.Join(o.ContentDir, notFoundStem) {
			continue // rendered for unknown URLs; not a page of the site
		}
		if stem == path.Join(o.ContentDir, errorStem) {
			continue // rendered for a loader's brick.error; not a page of the site
		}
		doc := docs[f]
		pg := Page{Path: f, Params: userParams(doc.Params), Locale: loc, Markdown: path.Ext(f) == extMD, variants: variantFiles[stem]}
		pg.Title, _ = doc.Params["title"].(string)
		pg.Description, _ = doc.Params["description"].(string)
		pg.Date, _ = doc.Params["date"].(time.Time)
		pg.Draft, _ = doc.Params["draft"].(bool)
		if pg.Draft && !o.Drafts {
			continue
		}
		_, pg.layout = doc.Params[layoutKey]
		if eng, ok := doc.Params[engineKey].(map[string]any); ok {
			if v, ok := eng[sitemapKey]; ok {
				b, isBool := v.(bool)
				if !isBool {
					return nil, nil, nil, fmt.Errorf("site: %s: brick: sitemap: must be true or false, got %v", f, v)
				}
				pg.noSitemap = !b
			}
			if v, ok := eng[perRequestKey]; ok {
				b, isBool := v.(bool)
				if !isBool {
					return nil, nil, nil, fmt.Errorf("site: %s: brick: per_request: must be true or false, got %v", f, v)
				}
				pg.perRequest = b
			}
		}
		dyn := isPatternURL(strings.TrimPrefix(stem, o.ContentDir+"/"))
		out, err := outputOf(f, dyn)
		if err != nil {
			return nil, nil, nil, err
		}
		pg.Output = out
		if out != "" {
			pg.prolog, _ = liftProlog(doc.Body)
		}
		rel := strings.TrimPrefix(f, o.ContentDir+"/")
		if dir, _, ok := strings.Cut(rel, "/"); ok {
			pg.Section = dir
		}
		if dyn {
			dp, err := dynamicOf(o.ContentDir, stem, pg, doc.Params)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("site: %s: %w", f, err)
			}
			dynamic = append(dynamic, dp)
			continue
		}
		u, err := urlFor(o.ContentDir, stem, doc.Params)
		if err == nil && pg.Output != "" {
			u, err = outputURL(o.ContentDir, stem, pg.Output, doc.Params)
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("site: %s: %w", f, err)
		}
		pg.URL = u
		pages = append(pages, pg)
	}
	diags = append(diags, collections(o, pages)...)
	slices.SortStableFunc(pages, func(a, b Page) int {
		if c := b.Date.Compare(a.Date); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return pages, dynamic, diags, nil
}

// dynamicOf reads a dynamic page: its URL is its file path, never a
// brick: {url:} override, and brick: {paths:} names its value list.
func dynamicOf(contentDir, stem string, pg Page, params map[string]any) (dynamicPage, error) {
	dp := dynamicPage{page: pg}
	if eng, ok := params[engineKey].(map[string]any); ok {
		if _, ok := eng[urlKey]; ok {
			return dp, errors.New("a dynamic page takes its URL from its file name; brick: {url:} cannot be set on it")
		}
		if raw, ok := eng[pathsKey]; ok {
			s, ok := raw.(string)
			if !ok || s == "" {
				return dp, fmt.Errorf("brick: paths: must name a script function (posts.slugs), got %v", raw)
			}
			dp.paths = s
		}
	}
	u, err := urlFor(contentDir, stem, nil)
	if pg.Output != "" {
		// A file URL, no trailing slash: /tags/[tag]/feed.xml (v22).
		u, err = outputURL(contentDir, stem, pg.Output, nil)
	}
	if err != nil {
		return dp, err
	}
	if _, err := parsePattern(u); err != nil {
		return dp, err
	}
	dp.page.URL = u
	return dp, nil
}

// userParams drops engine keys so .Params means the author's data, as in
// the engine.
func userParams(p map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range p {
		if k != engineKey {
			out[k] = v
		}
	}
	return out
}

// variantOf splits `content/about.fr.html` into (`content/about`, `fr`);
// a shared file returns its stem and "".
func variantOf(p string) (stem, locale string) {
	stem = strings.TrimSuffix(p, path.Ext(p))
	dot := strings.LastIndexByte(stem, '.')
	if dot < 0 || strings.Contains(stem[dot:], "/") {
		return stem, ""
	}
	if isOutputExt(stem[dot+1:]) {
		return stem, "" // feed.xml: an output page, never the locale "xml"
	}
	tag, ok := brick.NormalizeLocale(stem[dot+1:])
	if !ok {
		return stem, ""
	}
	return stem[:dot], tag
}

// urlFor is the path mirror, or the `brick: {url: …}` override. URLs are
// directory-style with a trailing slash, so a static export needs no
// rewrite rules.
func urlFor(contentDir, stem string, params map[string]any) (string, error) {
	if eng, ok := params[engineKey].(map[string]any); ok {
		if raw, ok := eng[urlKey]; ok {
			s, ok := raw.(string)
			if !ok || !strings.HasPrefix(s, "/") || strings.Contains(s, "..") || strings.ContainsAny(s, "?#\\ ") {
				return "", fmt.Errorf("brick.url must be a path starting with /, got %v", raw)
			}
			return withSlash(s), nil
		}
	}
	rel := strings.TrimPrefix(stem, contentDir+"/")
	if rel == indexStem {
		return "/", nil
	}
	rel = strings.TrimSuffix(rel, "/"+indexStem)
	return "/" + rel + "/", nil
}

// outputURL is an output page's URL: its file name without .html, at its
// path (content/blog/feed.xml.html → /blog/feed.xml), or a brick: {url:}
// override that keeps the format's extension, so the file served and the
// file exported are the same kind.
func outputURL(contentDir, stem, ext string, params map[string]any) (string, error) {
	if eng, ok := params[engineKey].(map[string]any); ok {
		if raw, ok := eng[urlKey]; ok {
			u, _ := raw.(string)
			if !strings.EqualFold(path.Ext(u), "."+ext) {
				return "", fmt.Errorf("brick.url of a .%s output page must end in .%s, got %v", ext, ext, raw)
			}
			return u, nil
		}
	}
	return "/" + strings.TrimPrefix(stem, contentDir+"/"), nil
}

func withSlash(u string) string {
	if strings.HasSuffix(u, "/") || path.Ext(u) != "" {
		return u
	}
	return u + "/"
}

// localeURL puts a non-default locale's prefix on a URL.
func localeURL(u, locale string, locales []string) string {
	if locale == "" || len(locales) == 0 || locale == locales[0] {
		return u
	}
	return "/" + locale + u
}

// route gives every page its URLs: one per locale for a shared page, one
// for a variant-only page. Two pages on one URL is an error naming both.
// Dynamic pages become patterns the same way, one per locale, in match
// order.
func route(pages []Page, dynamic []dynamicPage, locales []string) (map[string]Route, []string, []*pattern, error) {
	routes := map[string]Route{}
	add := func(r Route) error {
		if prev, ok := routes[r.URL]; ok {
			// A *brick.Error so callers can match the collision by Code
			// (CodeRoute) instead of parsing the message.
			return &brick.Error{Code: CodeRoute, Err: fmt.Errorf("site: %s and %s both have the URL %s; set brick: {url: …} on one of them", prev.Path, r.Path, r.URL)}
		}
		routes[r.URL] = r
		return nil
	}
	for _, pg := range pages {
		targets := []string{pg.Locale}
		if pg.Locale == "" && len(locales) > 0 {
			targets = locales
		}
		for _, loc := range targets {
			r := Route{URL: localeURL(pg.URL, loc, locales), Path: variantPath(pg, loc), page: pg.Path, Locale: loc, Markdown: pg.Markdown, Output: pg.Output, noLayout: pg.Output != "" && !pg.layout, prolog: pg.prolog, PerRequest: pg.perRequest}
			if err := add(withPageMeta(r, pg)); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	var pats []*pattern
	for _, dp := range dynamic {
		pg := dp.page
		targets := []string{pg.Locale}
		if pg.Locale == "" && len(locales) > 0 {
			targets = locales
		}
		for _, loc := range targets {
			u := localeURL(pg.URL, loc, locales)
			segs, err := parsePattern(u)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("site: %s: %w", pg.Path, err)
			}
			r := withPageMeta(Route{URL: u, Path: variantPath(pg, loc), page: pg.Path, Locale: loc, Markdown: pg.Markdown, Params: params(segs), Dynamic: true, Output: pg.Output, noLayout: pg.Output != "" && !pg.layout, prolog: pg.prolog, PerRequest: pg.perRequest}, pg)
			pats = append(pats, &pattern{route: r, segs: segs, paths: dp.paths})
		}
	}
	if err := orderPatterns(pats); err != nil {
		return nil, nil, nil, err
	}
	return routes, sortedKeys(routes), pats, nil
}

// variantPath is the file a page renders for a locale: the exact tag's
// variant (about.pt-BR.html), then the language's (about.pt.html), else
// the shared file, the same order the engine picks at render (C16), so
// Route.Path names what the URL really serves.
func variantPath(pg Page, locale string) string {
	if locale == "" || len(pg.variants) == 0 {
		return pg.Path
	}
	ext := path.Ext(pg.Path)
	stem := strings.TrimSuffix(pg.Path, ext)
	for tag := locale; tag != ""; {
		if cand := stem + "." + tag + ext; pg.variants[cand] {
			return cand
		}
		i := strings.LastIndexByte(tag, '-')
		if i < 0 {
			break
		}
		tag = tag[:i]
	}
	return pg.Path
}

// withPageMeta copies the page's listing fields and frontmatter onto its
// route (G17).
func withPageMeta(r Route, pg Page) Route {
	r.Title, r.Description, r.Date, r.Draft, r.Section = pg.Title, pg.Description, pg.Date, pg.Draft, pg.Section
	r.Meta = pg.Params
	return r
}

// cloneRoute is the copy Routes and Patterns hand out: the slices and the
// frontmatter are the caller's to change.
func cloneRoute(r Route) Route {
	r.Params = slices.Clone(r.Params)
	if r.Meta != nil {
		r.Meta, _ = cloneValue(r.Meta).(map[string]any)
	}
	return r
}

// cloneValue deep-copies a YAML value: maps and lists are rebuilt, scalars
// (strings, numbers, times) are values already.
func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = cloneValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cloneValue(e)
		}
		return out
	}
	return v
}

// listed is the index as templates see it: the pages a reader visits, so
// output pages (feed.xml, robots.txt) are left out and a feed ranging over
// .Site.Pages never lists itself.
func listed(pages []Page) []Page {
	out := make([]Page, 0, len(pages))
	for _, pg := range pages {
		if pg.Output == "" {
			out = append(out, pg)
		}
	}
	return out
}

// collections checks each section's pages against its _collection.yaml.
func collections(o Options, pages []Page) []brick.Diagnostic {
	var out []brick.Diagnostic
	schemas := map[string]*brick.Schema{}
	for _, pg := range pages {
		// The section's own index page lists the collection; it is not a
		// member of it.
		if pg.Output != "" || pg.Section == "" || strings.TrimSuffix(pg.Path, path.Ext(pg.Path)) == path.Join(o.ContentDir, pg.Section, indexStem) {
			continue
		}
		sch, seen := schemas[pg.Section]
		if !seen {
			p := path.Join(o.ContentDir, pg.Section, collectionFile)
			if b, err := fs.ReadFile(o.FS, p); err == nil {
				doc, err := frontmatter.Parse(append([]byte("---\n"), append(b, []byte("\n---\n")...)...))
				if err == nil {
					sch, err = brick.ParseSchema(doc.Params[collectionFields])
				}
				if err != nil {
					out = append(out, brick.Diagnostic{File: p, Severity: brick.SeverityError, Code: CodeCollection, Message: err.Error()})
				}
			}
			schemas[pg.Section] = sch
		}
		if sch == nil {
			continue
		}
		for _, msg := range sch.Validate(pg.Params) {
			out = append(out, brick.Diagnostic{File: pg.Path, Line: 1, Severity: brick.SeverityError, Code: CodeCollection, Message: fmt.Sprintf("frontmatter breaks the %s collection: %s", pg.Section, msg)})
		}
	}
	return out
}

// Small filesystem helpers.

func exists(fsys fs.FS, p string) bool {
	st, err := fs.Stat(fsys, p)
	return err == nil && !st.IsDir()
}

func isDir(fsys fs.FS, p string) bool {
	st, err := fs.Stat(fsys, p)
	return err == nil && st.IsDir()
}

// under reports whether p is dir or inside it.
func under(p, dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
