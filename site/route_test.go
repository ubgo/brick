package site

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/watch"
)

// postsScripting is a `posts` module over a fixed record set: one(input)
// is the loader (brick.ErrNotFound for an unknown slug), slugs() the
// paths: list. It counts loader calls so a test can see the cache and the
// closed world.
type postsScripting struct {
	posts map[string]string
	slugs []any
	calls *atomic.Int32
}

func (p postsScripting) Compile(context.Context, brick.ScriptSource) (brick.ScriptModule, error) {
	return p, nil
}

func (p postsScripting) Functions() []string { return []string{"one", "slugs"} }

func (p postsScripting) Call(_ context.Context, fn string, args ...any) (any, error) {
	if fn == "slugs" {
		return p.slugs, nil
	}
	p.calls.Add(1)
	in, _ := args[0].(map[string]any)
	pg, _ := in["page"].(map[string]any)
	route, _ := pg["route"].(map[string]any)
	slug, _ := route["slug"].(string)
	title, ok := p.posts[slug]
	if !ok {
		return nil, brick.ErrNotFound
	}
	return map[string]any{"title": title}, nil
}

func newPosts() postsScripting {
	return postsScripting{
		posts: map[string]string{"hello": "Hello", "second": "Second"},
		slugs: []any{map[string]any{"slug": "hello"}, map[string]any{"slug": "second"}},
		calls: &atomic.Int32{},
	}
}

// postsSite is a blog whose post page is content/posts/[slug].html.
func postsSite(paths bool) fstest.MapFS {
	front := "---\nuse: {posts: ~/scripts/posts.star}\nload: posts.one\n---\n"
	if paths {
		front = "---\nuse: {posts: ~/scripts/posts.star}\nload: posts.one\nbrick: {paths: posts.slugs}\n---\n"
	}
	return project(
		"layouts/default.html", "<html>{{ children }}</html>",
		"scripts/posts.star", "# fake",
		"content/index.html", "home",
		"content/404.html", "missing page",
		"content/posts/[slug].html", front+"<h1>{{ .Params.title }}</h1>{{ .Page.Route.slug }}",
	)
}

// Matching order: a static page beats every pattern; then,
// segment by segment, a literal before [name] before [...name] before
// [[...name]].
func TestDynamicRouteOrder(t *testing.T) {
	t.Parallel()
	show := "{{ .Page.Path }}|{{ range $k, $v := .Page.Route }}{{ $k }}={{ $v }};{{ end }}|{{ range .Page.RouteParts }}{{ . }},{{ end }}"
	s := newSite(t, project(
		"content/posts/new.html", "static new",
		"content/posts/[slug].html", show,
		"content/posts/[cat]/[slug].html", show,
		"content/[section]/[slug].html", show,
		"content/[section]/latest.html", show,
		"content/docs/[...path].html", show,
		"content/[[...all]].html", show,
	), Options{})
	cases := map[string]string{
		"/posts/new/":      "static new",
		"/posts/hello/":    "content/posts/[slug].html|slug=hello;|",
		"/posts/go/intro/": "content/posts/[cat]/[slug].html|cat=go;slug=intro;|",
		"/blog/x/":         "content/[section]/[slug].html|section=blog;slug=x;|",
		"/blog/latest/":    "content/[section]/latest.html|section=blog;|",
		"/docs/a/b/":       "content/docs/[...path].html|path=a/b;|a,b,",
		"/docs/a/":         "content/docs/[...path].html|path=a;|a,",
		"/docs/":           "content/[[...all]].html|all=docs;|docs,",
		"/":                "content/[[...all]].html|all=;|",
		"/x/y/z/":          "content/[[...all]].html|all=x/y/z;|x,y,z,",
	}
	for u, want := range cases {
		if got := page(t, s, u); got != want {
			t.Errorf("%s: got %q, want %q", u, got, want)
		}
	}
	var dyn []string
	for _, r := range s.Patterns() {
		dyn = append(dyn, r.URL+" "+strings.Join(r.Params, ","))
	}
	for _, r := range s.Routes() {
		if len(r.Params) > 0 || strings.Contains(r.URL, "[") {
			t.Errorf("Routes lists a pattern: %+v", r)
		}
	}
	want := []string{
		"/posts/[slug]/ slug",
		"/posts/[cat]/[slug]/ cat,slug",
		"/docs/[...path]/ path",
		"/[section]/latest/ section",
		"/[section]/[slug]/ section,slug",
		"/[[...all]]/ all",
	}
	if strings.Join(dyn, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Patterns order:\n%s", strings.Join(dyn, "\n"))
	}
}

func TestDynamicRouteErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]fstest.MapFS{
		"match the same URLs":           project("content/posts/[slug].html", "a", "content/posts/[id].html", "b"),
		"whole name":                    project("content/posts/a[b].html", "a"),
		"catch-all must be the last":    project("content/[...x]/y.html", "a"),
		"appears twice":                 project("content/[a]/[a].html", "a"),
		"takes its URL from its file":   project("content/[a].html", "---\nbrick: {url: /x/}\n---\na"),
		"paths: must name a script fun": project("content/[a].html", "---\nbrick: {paths: 3}\n---\na"),
	}
	for want, fsys := range cases {
		_, err := New(context.Background(), Options{FS: fsys})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", want, err)
		}
	}
	_, err := New(context.Background(), Options{FS: cases["match the same URLs"]})
	var be *brick.Error
	if !errors.As(err, &be) || be.Code != CodeRoute || !strings.Contains(err.Error(), "[id].html") || !strings.Contains(err.Error(), "[slug].html") {
		t.Fatalf("conflict should be a CodeRoute error naming both files: %v", err)
	}
}

// The loader reads page.route; brick.ErrNotFound answers with the 404 page,
// and the miss is never cached while a hit is.
func TestDynamicRouteLoaderAndNotFound(t *testing.T) {
	t.Parallel()
	sc := newPosts()
	s := newSite(t, postsSite(false), Options{Engine: brick.Options{Scripting: sc}})
	h := s.Handler()
	for range 2 {
		rec, body := fetch(h, "/posts/hello/")
		if rec.Code != 200 || body != "<html><h1>Hello</h1>hello</html>" {
			t.Fatalf("hit: %d %q", rec.Code, body)
		}
	}
	if n := sc.calls.Load(); n != 1 {
		t.Fatalf("a found page is cached: %d loader calls", n)
	}
	for range 2 {
		rec, body := fetch(h, "/posts/nope/")
		if rec.Code != http.StatusNotFound || !strings.Contains(body, "missing page") {
			t.Fatalf("miss: %d %q", rec.Code, body)
		}
	}
	if n := sc.calls.Load(); n != 3 {
		t.Fatalf("a miss must not be cached: %d loader calls", n)
	}
	if _, err := s.Render(context.Background(), "/posts/nope/"); !errors.Is(err, brick.ErrNotFound) {
		t.Fatalf("Render: %v", err)
	}
	// No trailing slash redirects, as for a static page.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/posts/hello", nil))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/posts/hello/" {
		t.Fatalf("redirect: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// With paths: the world is closed: a value off the list is 404 without
// running the page.
func TestDynamicRouteClosedWorld(t *testing.T) {
	t.Parallel()
	sc := newPosts()
	sc.posts["unlisted"] = "Exists but unlisted"
	s := newSite(t, postsSite(true), Options{Engine: brick.Options{Scripting: sc}})
	h := s.Handler()
	if rec, body := fetch(h, "/posts/second/"); rec.Code != 200 || !strings.Contains(body, "Second") {
		t.Fatalf("listed: %d %q", rec.Code, body)
	}
	before := sc.calls.Load()
	if rec, _ := fetch(h, "/posts/unlisted/"); rec.Code != http.StatusNotFound {
		t.Fatalf("unlisted value answered %d", rec.Code)
	}
	if sc.calls.Load() != before {
		t.Fatal("the page ran for a value off its paths: list")
	}
}

// A route value is one segment of text: dot segments, an encoded slash or
// backslash, and control characters never reach a page.
func TestDynamicRouteValuesStayInside(t *testing.T) {
	t.Parallel()
	s := newSite(t, project(
		"content/404.html", "missing page",
		"content/f/[name].html", "one {{ .Page.Route.name }}",
		"content/d/[...rest].html", "rest {{ .Page.Route.rest }}",
	), Options{})
	h := s.Handler()
	for _, u := range []string{"/f/../", "/f/./", "/d/a/../", "/f/a%2Fb/", "/f/a%5Cb/", "/d/a%2fb/", "/f/a%00/", "/d//a/"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		if err := setPath(req, u); err != nil {
			t.Fatal(err)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d %q", u, rec.Code, rec.Body)
		}
	}
	if rec, body := fetch(h, "/d/a/b/"); rec.Code != 200 || body != "rest a/b" {
		t.Fatalf("a plain catch-all: %d %q", rec.Code, body)
	}
	for _, v := range []string{"", ".", "..", "a/b", `a\b`, "a\x00", "a\nb"} {
		if validSegment(v) {
			t.Errorf("validSegment(%q) = true", v)
		}
	}
	for _, v := range []string{"a", "hello-world", "café", "a.b", "..a"} {
		if !validSegment(v) {
			t.Errorf("validSegment(%q) = false", v)
		}
	}
}

// fetch GETs url and returns the response and its body.
func fetch(h http.Handler, u string) (*httptest.ResponseRecorder, string) {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
	return rec, rec.Body.String()
}

// setPath sets a request's path from its escaped form as the server reads
// it: no dot-segment cleaning, which is what a raw handler sees.
func setPath(r *http.Request, escaped string) error {
	u, err := url.ParseRequestURI(escaped)
	if err != nil {
		return err
	}
	r.URL = u
	return nil
}

func TestDynamicRouteExport(t *testing.T) {
	t.Parallel()
	sc := newPosts()
	s := newSite(t, postsSite(true), Options{Engine: brick.Options{Scripting: sc}})
	dir := filepath.Join(t.TempDir(), "out")
	rep, err := s.Export(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pages != 3 {
		t.Fatalf("pages: %+v", rep)
	}
	b, err := os.ReadFile(filepath.Join(dir, "posts", "hello", "index.html"))
	if err != nil || string(b) != "<html><h1>Hello</h1>hello</html>" {
		t.Fatalf("exported post: %q %v", b, err)
	}
	// Without paths: export cannot know the pages.
	open := newSite(t, postsSite(false), Options{Engine: brick.Options{Scripting: newPosts()}})
	if _, err := open.Export(context.Background(), filepath.Join(t.TempDir(), "o")); !errors.Is(err, ErrExportNeedsPaths) {
		t.Fatalf("export without paths: %v", err)
	}
	// A listed value that is not one segment is refused before any file is
	// written for it.
	bad := newPosts()
	bad.slugs = []any{map[string]any{"slug": "../../etc"}}
	evil := newSite(t, postsSite(true), Options{Engine: brick.Options{Scripting: bad}})
	if _, err := evil.Export(context.Background(), filepath.Join(t.TempDir(), "e")); err == nil || !strings.Contains(err.Error(), "not one URL segment") {
		t.Fatalf("export of a dot-dot value: %v", err)
	}
	extra := newPosts()
	extra.slugs = []any{map[string]any{"slug": "hello", "typo": "x"}}
	if _, err := newSite(t, postsSite(true), Options{Engine: brick.Options{Scripting: extra}}).Export(context.Background(), filepath.Join(t.TempDir(), "x")); err == nil || !strings.Contains(err.Error(), "not a parameter") {
		t.Fatalf("export with an unknown key: %v", err)
	}
}

func TestDynamicRouteBasePathAndLocales(t *testing.T) {
	t.Parallel()
	s := newSite(t, postsSite(false), Options{BasePath: "/b", Locales: []string{"en", "fr"}, Engine: brick.Options{Scripting: newPosts()}})
	h := s.Handler()
	for _, u := range []string{"/b/posts/hello/", "/b/fr/posts/hello/"} {
		if rec, body := fetch(h, u); rec.Code != 200 || !strings.Contains(body, "Hello") {
			t.Fatalf("%s: %d %q", u, rec.Code, body)
		}
	}
	if rec, _ := fetch(h, "/posts/hello/"); rec.Code != http.StatusNotFound {
		t.Fatalf("outside the base: %d", rec.Code)
	}
}

// A new [slug] file is routed after the change is applied (dev live reload).
func TestDynamicRouteAppliedLive(t *testing.T) {
	t.Parallel()
	fsys := project("content/index.html", "home")
	s := newSite(t, fsys, Options{})
	if _, err := s.Render(context.Background(), "/tags/go/"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before: %v", err)
	}
	fsys["content/tags/[tag].html"] = &fstest.MapFile{Data: []byte("tag {{ .Page.Route.tag }}")}
	dropped, err := s.Apply(context.Background(), []watch.Change{{Path: "content/tags/[tag].html", Op: watch.Created}})
	if err != nil || len(dropped) != 1 || dropped[0] != "*" {
		t.Fatalf("apply: %v %v", dropped, err)
	}
	if got := page(t, s, "/tags/go/"); got != "tag go" {
		t.Fatalf("after: %q", got)
	}
}

// A component with brick: {query: [x]} reads its first value from the
// request's query, filled per request.
func TestQueryStateThroughTheSite(t *testing.T) {
	t.Parallel()
	fsys := project(
		"components/Q.html", "---\nstate:\n  x: \"\"\nbrick: {query: [x]}\n---\n<brick:script>flip</brick:script>\n<div><b>x={{ .State.x }}</b><button brick:click=\"flip\">f</button></div>",
		"content/page.html", "<Q />",
	)
	s := newSite(t, fsys, Options{Engine: brick.Options{Scripting: flipScripting{}}})
	h := s.Handler()
	if rec, body := fetch(h, "/page/?x=v"); rec.Code != 200 || !strings.Contains(body, "<b>x=v</b>") {
		t.Fatalf("query: %d %s", rec.Code, body)
	}
	if rec, body := fetch(h, "/page/?x=w"); rec.Code != 200 || !strings.Contains(body, "<b>x=w</b>") {
		t.Fatalf("second visitor got the first query: %d %s", rec.Code, body)
	}
}
