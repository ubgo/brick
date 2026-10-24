package site

import (
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

// outputSite is a blog with a feed, a robots.txt, a translated page, a
// draft, an opted-out page and a markdown page.
func outputSite(extra ...string) map[string]string {
	files := map[string]string{
		"site.yaml":               "title: Blog\nbaseURL: https://example.com\n",
		"layouts/default.html":    "<html><body>{{ children }}</body></html>",
		"content/index.html":      "---\ntitle: Home\n---\nhome",
		"content/about.html":      "---\ntitle: About\n---\nabout",
		"content/about.fr.html":   "---\ntitle: À propos\n---\nà propos",
		"content/blog/hello.html": "---\ntitle: Hello & <welcome>\ndate: 2026-10-01\n---\nhi",
		"content/blog/draft.html": "---\ntitle: Draft\ndraft: true\n---\nwip",
		"content/private.html":    "---\ntitle: Private\nbrick: {sitemap: false}\n---\nhidden",
		"content/feed.xml.html": "---\ntitle: Feed\n---\n<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<rss version=\"2.0\"><channel><title>{{ .Site.title }}</title>" +
			"{{ range .Site.Pages }}{{ if eq .Section `blog` }}<item><title>{{ .Title }}</title><link>{{ $.Site.baseURL }}{{ .URL }}</link></item>{{ end }}{{ end }}</channel></rss>\n",
		"content/robots.txt.html": "User-agent: *\nSitemap: {{ .Site.baseURL }}/sitemap.xml\n",
	}
	for i := 0; i+1 < len(extra); i += 2 {
		files[extra[i]] = extra[i+1]
	}
	return files
}

func projectOf(files map[string]string) []string {
	var out []string
	for _, k := range slices.Sorted(maps.Keys(files)) {
		out = append(out, k, files[k])
	}
	return out
}

// An output page renders at its file name with its format's Content-Type,
// no layout, nothing injected, the XML prolog intact and HTML escaping that
// XML reads correctly; it is not in .Site.Pages.
func TestOutputPages(t *testing.T) {
	s := newSite(t, project(projectOf(outputSite())...), Options{Dev: true})
	h := s.Handler()
	rec, body := fetch(h, "/feed.xml")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/xml; charset=utf-8" {
		t.Fatalf("feed: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	want := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<rss version=\"2.0\"><channel><title>Blog</title><item><title>Hello &amp; &lt;welcome&gt;</title><link>https://example.com/blog/hello/</link></item></channel></rss>\n"
	if body != want {
		t.Fatalf("feed body:\n%q\nwant\n%q", body, want)
	}
	rec, body = fetch(h, "/robots.txt")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" || body != "User-agent: *\nSitemap: https://example.com/sitemap.xml\n" {
		t.Fatalf("robots: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), body)
	}
	// A directory-style URL of the output page is not a page.
	if rec, _ := fetch(h, "/feed.xml/"); rec.Code != 404 {
		t.Fatalf("/feed.xml/ = %d", rec.Code)
	}
	for _, pg := range s.Pages() {
		if pg.Output != "" {
			t.Fatalf(".Site.Pages lists the output page %s", pg.Path)
		}
	}
	var outs []string
	for _, r := range s.Routes() {
		if r.Output != "" {
			outs = append(outs, r.URL+"="+r.Output)
		}
	}
	if strings.Join(outs, " ") != "/feed.xml=xml /robots.txt=txt" {
		t.Fatalf("output routes: %v", outs)
	}
}

// An output page that names a layout gets it; frontmatter, a URL override
// keeping the extension, and .rss/.atom types work.
func TestOutputPageLayoutAndURL(t *testing.T) {
	s := newSite(t, project(
		"layouts/default.html", "<html>{{ children }}</html>",
		"layouts/xml.html", "<wrap>{{ children }}</wrap>",
		"content/a.xml.html", "---\nlayout: xml\ntitle: A\n---\n<a>{{ .Title }}</a>",
		"content/b.rss.html", "---\nbrick: {url: /feeds/main.rss}\n---\n<rss/>",
		"content/c.atom.html", "<feed/>",
	), Options{})
	h := s.Handler()
	if _, body := fetch(h, "/a.xml"); body != "<wrap><a>A</a></wrap>" {
		t.Fatalf("a.xml = %q", body)
	}
	if rec, body := fetch(h, "/feeds/main.rss"); body != "<rss/>" || rec.Header().Get("Content-Type") != "application/rss+xml; charset=utf-8" {
		t.Fatalf("main.rss = %q %q", body, rec.Header().Get("Content-Type"))
	}
	if rec, _ := fetch(h, "/c.atom"); rec.Header().Get("Content-Type") != "application/atom+xml; charset=utf-8" {
		t.Fatalf("c.atom type %q", rec.Header().Get("Content-Type"))
	}
	// An override that drops the extension is refused.
	_, err := New(context.Background(), Options{FS: project("content/b.rss.html", "---\nbrick: {url: /feed/}\n---\nx")})
	if err == nil || !strings.Contains(err.Error(), "must end in .rss") {
		t.Fatalf("override without extension: %v", err)
	}
}

// html/template escapes for HTML, which is wrong for JSON, JS, CSS and CSV:
// those output pages are refused by name with a *brick.Error. Other second
// extensions are not output pages at all.
func TestOutputPageRefused(t *testing.T) {
	for file, want := range map[string]string{
		"content/data.json.html":      "html/template escapes text for HTML",
		"content/app.js.html":         "not JavaScript",
		"content/feed.xml.md":         "an output page is a .html file",
		"content/t/[tag].xml.html":    "put the [param] in a directory",
		"content/t/[tag]/a.json.html": "html/template escapes text for HTML",
	} {
		_, err := New(context.Background(), Options{FS: project(file, "x"), Engine: brick.Options{Markdown: fakeMD{}}})
		var be *brick.Error
		if err == nil || !errors.As(err, &be) || be.Code != CodeOutput || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", file, err)
		}
	}
	// v1.2.html is an ordinary page, as before.
	s := newSite(t, project("content/v1.2.html", "x"), Options{})
	if r := s.Routes(); len(r) != 1 || r[0].URL != "/v1.2/" || r[0].Output != "" {
		t.Fatalf("v1.2.html routes: %+v", r)
	}
}

// Export writes an output page at its exact file name, untouched by the
// HTML rewrites, and the sitemap beside it.
func TestOutputPageExport(t *testing.T) {
	s := newSite(t, project(projectOf(outputSite())...), Options{BasePath: "/docs", Sitemap: &Sitemap{}})
	dir := filepath.Join(t.TempDir(), "out")
	rep, err := s.Export(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Sitemap {
		t.Fatal("report: no sitemap")
	}
	feed, _ := os.ReadFile(filepath.Join(dir, "feed.xml"))
	if !strings.HasPrefix(string(feed), "<?xml version=") || strings.Contains(string(feed), "<base") || strings.Contains(string(feed), "/docs") {
		t.Fatalf("exported feed:\n%s", feed)
	}
	robots, _ := os.ReadFile(filepath.Join(dir, "robots.txt"))
	if string(robots) != "User-agent: *\nSitemap: https://example.com/sitemap.xml\n" {
		t.Fatalf("robots %q", robots)
	}
	sm, _ := os.ReadFile(filepath.Join(dir, "sitemap.xml"))
	if !strings.Contains(string(sm), "<loc>https://example.com/docs/about/</loc>") {
		t.Fatalf("sitemap under /docs:\n%s", sm)
	}
}

// The sitemap lists the indexable pages with absolute URLs, lastmod and
// hreflang alternates (self included), and leaves out drafts even when
// they are served, output pages and opted-out pages.
func TestSitemap(t *testing.T) {
	s := newSite(t, project(projectOf(outputSite())...), Options{Drafts: true, Locales: []string{"en", "fr"}, Sitemap: &Sitemap{}})
	rec, body := fetch(s.Handler(), "/sitemap.xml")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/xml; charset=utf-8" {
		t.Fatalf("sitemap: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		"<loc>https://example.com/about/</loc>\n    <xhtml:link rel=\"alternate\" hreflang=\"en\" href=\"https://example.com/about/\"/>\n    <xhtml:link rel=\"alternate\" hreflang=\"fr\" href=\"https://example.com/fr/about/\"/>",
		"<loc>https://example.com/fr/about/</loc>\n    <xhtml:link rel=\"alternate\" hreflang=\"en\" href=\"https://example.com/about/\"/>",
		"<loc>https://example.com/blog/hello/</loc>\n    <lastmod>2026-10-01</lastmod>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sitemap lacks %q:\n%s", want, body)
		}
	}
	for _, bad := range []string{"draft", "private", "feed.xml", "robots.txt", "404"} {
		if strings.Contains(body, bad) {
			t.Errorf("sitemap lists %q:\n%s", bad, body)
		}
	}
	// The draft is still served (Drafts), just not listed.
	if rec, _ := fetch(s.Handler(), "/blog/draft/"); rec.Code != 200 {
		t.Fatalf("draft not served: %d", rec.Code)
	}
	// Same site, same bytes.
	if _, again := fetch(s.Handler(), "/sitemap.xml"); again != body {
		t.Fatal("sitemap is not deterministic")
	}
}

// Off unless asked; a project's own sitemap wins; the base URL must be
// absolute; Options.Sitemap.BaseURL beats site.yaml.
func TestSitemapOptions(t *testing.T) {
	off := newSite(t, project(projectOf(outputSite())...), Options{})
	if rec, _ := fetch(off.Handler(), "/sitemap.xml"); rec.Code != 404 {
		t.Fatalf("sitemap without the option: %d", rec.Code)
	}
	own := newSite(t, project(projectOf(outputSite("content/sitemap.xml.html", "<mine/>"))...), Options{Sitemap: &Sitemap{}})
	if _, body := fetch(own.Handler(), "/sitemap.xml"); body != "<mine/>" {
		t.Fatalf("own sitemap page = %q", body)
	}
	static := newSite(t, project(projectOf(outputSite("static/sitemap.xml", "<static/>"))...), Options{Sitemap: &Sitemap{}})
	if _, body := fetch(static.Handler(), "/sitemap.xml"); body != "<static/>" {
		t.Fatalf("static sitemap = %q", body)
	}
	if rep, err := static.Export(context.Background(), filepath.Join(t.TempDir(), "o")); err != nil || rep.Sitemap {
		t.Fatalf("export with a static sitemap: %+v %v", rep, err)
	}
	for _, base := range []string{"", "/relative", "example.com"} {
		files := outputSite("site.yaml", "title: Blog\n")
		_, err := New(context.Background(), Options{FS: project(projectOf(files)...), Sitemap: &Sitemap{BaseURL: base}})
		if err == nil || !strings.Contains(err.Error(), "absolute base URL") {
			t.Errorf("base %q: %v", base, err)
		}
	}
	opt := newSite(t, project(projectOf(outputSite())...), Options{Sitemap: &Sitemap{BaseURL: "https://other.org/"}})
	if _, body := fetch(opt.Handler(), "/sitemap.xml"); !strings.Contains(body, "<loc>https://other.org/about/</loc>") {
		t.Fatalf("option base:\n%s", body)
	}
	// brick: {sitemap: } must be a bool.
	_, err := New(context.Background(), Options{FS: project("content/a.html", "---\nbrick: {sitemap: no-thanks}\n---\na")})
	if err == nil || !strings.Contains(err.Error(), "sitemap: must be true or false") {
		t.Fatalf("bad sitemap value: %v", err)
	}
}

// A dynamic page is listed through its paths: function, and left out
// without one.
func TestSitemapDynamic(t *testing.T) {
	withPaths := newSite(t, project(append(projectOf(postsSiteFiles(true)), "site.yaml", "baseURL: https://example.com\n")...), Options{Engine: brick.Options{Scripting: newPosts()}, Sitemap: &Sitemap{}})
	_, body := fetch(withPaths.Handler(), "/sitemap.xml")
	if !strings.Contains(body, "<loc>https://example.com/posts/hello/</loc>") || !strings.Contains(body, "<loc>https://example.com/posts/second/</loc>") {
		t.Fatalf("dynamic with paths:\n%s", body)
	}
	without := newSite(t, project(append(projectOf(postsSiteFiles(false)), "site.yaml", "baseURL: https://example.com\n")...), Options{Engine: brick.Options{Scripting: newPosts()}, Sitemap: &Sitemap{}})
	if _, body := fetch(without.Handler(), "/sitemap.xml"); strings.Contains(body, "/posts/") {
		t.Fatalf("dynamic without paths listed:\n%s", body)
	}
}

// postsSiteFiles is postsSite as a map, so a test can add files.
func postsSiteFiles(paths bool) map[string]string {
	out := map[string]string{}
	for k, f := range postsSite(paths) {
		out[k] = string(f.Data)
	}
	return out
}

// headingsMD reports headings, as brick.MarkdownHeadings.
type headingsMD struct{ fakeMD }

func (headingsMD) ConvertHeadings(src []byte, w io.Writer) ([]brick.Heading, error) {
	_, err := io.WriteString(w, `<h2 id="install">Install</h2>`)
	return []brick.Heading{{Level: 2, ID: "install", Text: "Install"}, {Level: 3, Text: "No id"}}, err
}

// A markdown page's layout reads .Page.Headings when the backend reports
// them, and an empty list when it cannot.
func TestMarkdownHeadings(t *testing.T) {
	files := project(
		"layouts/default.html", `<nav>{{ range .Page.Headings }}[{{ .Level }} {{ .ID }} {{ .Text }}]{{ end }}{{ len .Page.Headings }}</nav>{{ children }}`,
		"content/guide.md", "---\ntitle: Guide\n---\n## Install",
		"content/plain.html", "plain",
	)
	s := newSite(t, files, Options{Engine: brick.Options{Markdown: headingsMD{}}})
	if got := page(t, s, "/guide/"); got != `<nav>[2 install Install][3  No id]2</nav><h2 id="install">Install</h2>` {
		t.Fatalf("with headings: %q", got)
	}
	if got := page(t, s, "/plain/"); got != "<nav>0</nav>plain" {
		t.Fatalf("html page: %q", got)
	}
	plain := newSite(t, files, Options{})
	if got := page(t, plain, "/guide/"); !strings.HasPrefix(got, "<nav>0</nav>") {
		t.Fatalf("without the capability: %q", got)
	}
}
