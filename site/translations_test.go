package site

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// hreflangLayout writes the alternates block a multilingual site puts in
// its <head>: the page itself, then each translation.
const hreflangLayout = `<html lang="{{ .Page.Locale }}"><head><link rel="alternate" hreflang="{{ .Page.Locale }}" href="{{ .Page.URL }}">{{ range .Page.Translations }}<link rel="alternate" hreflang="{{ .Locale }}" href="{{ .URL }}">{{ end }}</head><body>{{ children }}</body></html>`

func i18nSite() map[string]string {
	return map[string]string{
		"layouts/default.html": hreflangLayout,
		// A shared page: one file, a route per locale.
		"content/about.html": "about",
		// Variant-only pages of one stem; fr has its own URL.
		"content/team.fr.html": "---\nbrick: {url: /equipe/}\n---\néquipe",
		"content/team.de.html": "team",
		// A page in one language only.
		"content/solo.fr.html": "seul",
	}
}

// .Page.Translations pairs a page's variants in locale order, never lists
// the page itself, honours a variant's own URL, and is empty for a page in
// one language.
func TestTranslations(t *testing.T) {
	t.Parallel()
	files := i18nSite()
	fs := fstest.MapFS{}
	for k, v := range files {
		fs[k] = &fstest.MapFile{Data: []byte(v)}
	}
	s := newSite(t, fs, Options{Locales: []string{"en", "fr", "de"}})
	link := func(loc, u string) string {
		return `<link rel="alternate" hreflang="` + loc + `" href="` + u + `">`
	}
	cases := map[string]string{
		"/about/":     link("en", "/about/") + link("fr", "/fr/about/") + link("de", "/de/about/"),
		"/fr/about/":  link("fr", "/fr/about/") + link("en", "/about/") + link("de", "/de/about/"),
		"/de/about/":  link("de", "/de/about/") + link("en", "/about/") + link("fr", "/fr/about/"),
		"/fr/equipe/": link("fr", "/fr/equipe/") + link("de", "/de/team/"),
		"/de/team/":   link("de", "/de/team/") + link("fr", "/fr/equipe/"),
		"/fr/solo/":   link("fr", "/fr/solo/"),
	}
	for u, want := range cases {
		got := page(t, s, u)
		head := got[strings.Index(got, "<head>")+len("<head>") : strings.Index(got, "</head>")]
		if head != want {
			t.Errorf("%s:\n got %s\nwant %s", u, head, want)
		}
	}
}

// One language: no prefixes, no translations, .Page.URL still set.
func TestTranslationsSingleLocale(t *testing.T) {
	t.Parallel()
	s := newSite(t, project("layouts/default.html", hreflangLayout, "content/about.html", "about"), Options{})
	if got := page(t, s, "/about/"); !strings.Contains(got, `<head><link rel="alternate" hreflang="" href="/about/"></head>`) {
		t.Fatalf("%s", got)
	}
}

// A dynamic page's translations carry the matched values to the other
// locales' patterns, and BasePath rebases them like every root URL.
func TestTranslationsDynamicAndBasePath(t *testing.T) {
	t.Parallel()
	fs := postsSite(false)
	fs["layouts/default.html"] = &fstest.MapFile{Data: []byte(hreflangLayout)}
	s := newSite(t, fs, Options{BasePath: "/b", Locales: []string{"en", "fr"}, Engine: brick.Options{Scripting: newPosts()}})
	_, body := fetch(s.Handler(), "/b/fr/posts/hello/")
	want := `<link rel="alternate" hreflang="fr" href="/b/fr/posts/hello/"><link rel="alternate" hreflang="en" href="/b/posts/hello/">`
	if !strings.Contains(body, want) {
		t.Fatalf("%s", body)
	}
}

// Route.Path names the file a URL really renders: a locale with a variant
// file gets it (exact tag, then language), the others the shared file. The
// sitemap still follows the shared page's flags (BV: replacing route's
// page: with Path lists /fr/hidden/ here).
func TestRoutePathNamesRenderedVariant(t *testing.T) {
	t.Parallel()
	fs := fstest.MapFS{}
	for k, v := range map[string]string{
		"layouts/default.html":   "{{ children }}",
		"content/about.html":     "about",
		"content/about.fr.html":  "à propos",
		"content/hidden.html":    "---\nbrick: {sitemap: false}\n---\nhidden",
		"content/hidden.fr.html": "caché",
		"content/pt.html":        "pt",
		"content/pt.pt.html":     "português",
	} {
		fs[k] = &fstest.MapFile{Data: []byte(v)}
	}
	s := newSite(t, fs, Options{Locales: []string{"en", "fr", "de", "pt-BR"}, Sitemap: &Sitemap{BaseURL: "https://example.com"}})
	want := map[string]string{
		"/about/":     "content/about.html",
		"/fr/about/":  "content/about.fr.html",
		"/de/about/":  "content/about.html",
		"/fr/hidden/": "content/hidden.fr.html",
		"/pt-BR/pt/":  "content/pt.pt.html",
	}
	got := map[string]string{}
	for _, r := range s.Routes() {
		got[r.URL] = r.Path
	}
	for u, p := range want {
		if got[u] != p {
			t.Errorf("%s: Path %q, want %q", u, got[u], p)
		}
	}
	if body := page(t, s, "/fr/about/"); body != "à propos" {
		t.Errorf("/fr/about/ renders %q", body)
	}
	_, sm := fetch(s.Handler(), "/sitemap.xml")
	if strings.Contains(sm, "hidden") || !strings.Contains(sm, "/fr/about/") {
		t.Errorf("sitemap:\n%s", sm)
	}
}
