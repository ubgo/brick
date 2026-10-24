package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// The exact pages the languages guide shows.
const (
	homeFR = `<!doctype html>
<html lang="fr">
<head>
<title>Atelier</title>
<link rel="alternate" hreflang="fr" href="/fr/">
<link rel="alternate" hreflang="en" href="/">
<link rel="alternate" hreflang="de" href="/de/">
</head>
<body>
<nav>Langues: <a href="/">en</a> <a href="/de/">de</a></nav>
<main><h1>Bienvenue à l&#39;atelier</h1>
<p>dimanche 27 septembre 2026</p>
</main>
</body>
</html>
`
	aboutFR = `<!doctype html>
<html lang="fr">
<head>
<title>À propos</title>
<link rel="alternate" hreflang="fr" href="/fr/about/">
<link rel="alternate" hreflang="en" href="/about/">
<link rel="alternate" hreflang="de" href="/de/about/">
</head>
<body>
<nav>Langues: <a href="/about/">en</a> <a href="/de/about/">de</a></nav>
<main><h1>À propos de nous</h1>
</main>
</body>
</html>
`
	teamDE = `<!doctype html>
<html lang="de">
<head>
<title>Team</title>
<link rel="alternate" hreflang="de" href="/de/team/">
<link rel="alternate" hreflang="fr" href="/fr/equipe/">
</head>
<body>
<nav>Languages: <a href="/fr/equipe/">fr</a></nav>
<main><h1>Das Team</h1>
</main>
</body>
</html>
`
	sitemapEquipe = `  <url>
    <loc>https://example.com/fr/equipe/</loc>
    <xhtml:link rel="alternate" hreflang="fr" href="https://example.com/fr/equipe/"/>
    <xhtml:link rel="alternate" hreflang="de" href="https://example.com/de/team/"/>
  </url>
`
)

func TestLanguages(t *testing.T) {
	s, err := newSite(context.Background(), themeDir)
	if err != nil {
		t.Fatal(err)
	}
	var routes []string
	for _, r := range s.Routes() {
		routes = append(routes, r.URL+" "+r.Locale+" "+r.Path)
	}
	want := []string{
		"/ en content/index.html",
		"/about/ en content/about.html",
		"/de/ de content/index.html",
		"/de/about/ de content/about.html",
		"/de/team/ de content/team.de.html",
		"/fr/ fr content/index.html",
		"/fr/about/ fr content/about.fr.html", // the route names the variant file that renders in fr
		"/fr/equipe/ fr content/team.fr.html",
	}
	for _, c := range []struct{ url, body string }{{"/fr/", homeFR}, {"/fr/about/", aboutFR}, {"/de/team/", teamDE}} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.url, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != c.body {
			t.Errorf("%s: %d\n%s", c.url, rec.Code, rec.Body)
		}
	}
	// A page that exists only as variants has no default-language URL.
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/team/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/team/: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if !strings.Contains(rec.Body.String(), sitemapEquipe) {
		t.Fatalf("sitemap:\n%s", rec.Body)
	}
	if !slices.Equal(routes, want) {
		t.Fatalf("routes:\n%s", strings.Join(routes, "\n"))
	}
}
