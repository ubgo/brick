package main

import (
	"context"
	"slices"
	"time"

	"github.com/ubgo/brick/site"
	"github.com/ubgo/brick/watch"
)

// Frontmatter keys the host reads from Route.Meta. They are ordinary
// author keys: the engine gives them no meaning, so a host is free to
// choose its own (a menu name here; a sitemap priority or an admin
// section elsewhere).
const (
	metaMenu   = "menu"
	metaWeight = "weight"
)

// pollEvery is how often watchSite looks for changed files. Short enough
// that an edit shows up while you are still looking at the page.
const pollEvery = time.Second

// menuLink is one entry of a navigation menu built from route metadata.
type menuLink struct {
	Title string
	URL   string
}

// menuLinks lists the pages whose frontmatter says `menu: <name>`, in
// `weight:` order (a page without a weight sorts last, then by URL, which
// is Routes' own order). Routes returns copies, so the menu can be sorted
// and kept without touching the site.
// ds:def id=menulinks-rerg2rdc
func menuLinks(s *site.Site, name string) []menuLink {
	type entry struct {
		weight int
		link   menuLink
	}
	var found []entry
	for _, r := range s.Routes() {
		if m, _ := r.Meta[metaMenu].(string); m != name {
			continue
		}
		w := weightOf(r.Meta[metaWeight])
		found = append(found, entry{w, menuLink{Title: r.Title, URL: r.URL}})
	}
	slices.SortStableFunc(found, func(a, b entry) int { return a.weight - b.weight })
	links := make([]menuLink, len(found))
	for i, e := range found {
		links[i] = e.link
	}
	return links
}

// unweighted sorts a page without a weight after every weighted one.
const unweighted = int(^uint(0) >> 1)

// weightOf reads a frontmatter number. The YAML decoder hands integers over
// as int64 (a plain int and a float are accepted too), so an assertion to
// int alone would silently treat every weight as missing.
// ds:def id=weightof-r5bzbjyn
func weightOf(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return unweighted
}

// watchSite applies file changes to a site served through Handler: it
// polls the project, drops the pages that read a changed file and tells
// live-reloading browsers. ListenAndServe in Dev does this itself; a host
// with its own http.Server runs this beside it until ctx ends, polling
// every `every` (pollEvery is a sensible default).
// ds:def id=watchsite-q2d3wh44
func watchSite(ctx context.Context, s *site.Site, every time.Duration) error {
	return s.Watch(ctx, watch.Options{Interval: every})
}
