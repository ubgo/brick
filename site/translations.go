package site

import (
	"cmp"
	"slices"
	"strings"

	"github.com/ubgo/brick"
)

// translations pairs every static route with the same page's routes in
// the other locales. Two routes are the same page when their files share a
// stem once the locale is dropped: a shared page has one file and a route
// per locale, while variant-only files (about.fr.html, about.de.html) are
// separate pages of one stem. The real routes are used, never a computed
// prefix, so a variant's own brick: {url:} is honoured. Each list follows
// locale order and leaves out the route itself.
func translations(routes map[string]Route, locales []string) map[string][]brick.Translation {
	byStem := map[string][]Route{}
	for _, u := range sortedKeys(routes) {
		r := routes[u]
		if r.Locale == "" {
			continue
		}
		stem, _ := variantOf(r.Path)
		byStem[stem] = append(byStem[stem], r)
	}
	out := map[string][]brick.Translation{}
	for _, group := range byStem {
		if len(group) < 2 {
			continue
		}
		slices.SortFunc(group, func(a, b Route) int { return compareLocale(a.Locale, b.Locale, locales) })
		for _, r := range group {
			ts := make([]brick.Translation, 0, len(group)-1)
			for _, o := range group {
				if o.Locale != r.Locale {
					ts = append(ts, brick.Translation{Locale: o.Locale, URL: o.URL})
				}
			}
			out[r.URL] = ts
		}
	}
	return out
}

// compareLocale orders locales as Options.Locales lists them, any locale
// it does not list after those by tag, so the order never depends on map
// iteration.
func compareLocale(a, b string, locales []string) int {
	ia, ib := slices.Index(locales, a), slices.Index(locales, b)
	if ia < 0 {
		ia = len(locales)
	}
	if ib < 0 {
		ib = len(locales)
	}
	if c := cmp.Compare(ia, ib); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// translationsOf is the list for one route being rendered. A static route
// reads the pairs built with the routes. A dynamic one is translated by
// its locale prefix: its other-locale patterns are the same file with
// another prefix (a dynamic page cannot set brick: {url:}), so the matched
// values carry over unchanged. Whether the other locale's paths: list
// holds the same values is not checked; a site whose lists differ per
// locale should build its switcher from its own data.
func (st *state) translationsOf(r Route, static bool) []brick.Translation {
	if static {
		return st.trans[r.URL]
	}
	if r.Locale == "" {
		return nil
	}
	stem, _ := variantOf(r.Path)
	rest := strings.TrimPrefix(r.URL, localeURL("", r.Locale, st.locales))
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	var group []string
	for _, p := range st.patterns {
		if s, _ := variantOf(p.route.Path); s == stem && p.route.Locale != r.Locale && !slices.Contains(group, p.route.Locale) {
			group = append(group, p.route.Locale)
		}
	}
	slices.SortFunc(group, func(a, b string) int { return compareLocale(a, b, st.locales) })
	out := make([]brick.Translation, 0, len(group))
	for _, loc := range group {
		out = append(out, brick.Translation{Locale: loc, URL: localeURL(rest, loc, st.locales)})
	}
	return out
}
