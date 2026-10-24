package site

import (
	"bytes"
	"encoding/json"
	"regexp"
)

// prefetchLinkRE finds a link that asks to be prefetched (brick:prefetch,
// ). Only such pages get speculation rules, so a page without
// one carries no extra bytes.
var prefetchLinkRE = regexp.MustCompile(`(?i)<a\s[^>]*\bbrick:prefetch\b`)

// navScriptMark is how a page loading nav.js is recognized: the engine
// serves it as <asset base>c/nav.<hash>.js (assets.go assetDir, navName),
// the same name nav.js's own runtime check looks for.
const navScriptMark = "c/nav."

// Speculation-rules selectors. Each link is prefetched by
// exactly one mechanism, the one that will load it:
//   - With nav.js on the page, nav.js prefetches the links it intercepts
//     into its own snapshot cache (a fetch() never reads the browser's
//     prefetch cache, so a native prefetch of such a link is a wasted
//     request). The rules then cover only links that opted out with
//     brick:nav="off", which are full page loads.
//   - Without nav.js every link is a full page load, so the rules cover
//     every brick:prefetch link.
//
// brick:prefetch="visible" maps to eagerness "eager"; any other value
// (hover, the default) to "moderate", the browser's hover intent.
const (
	selAll      = `a[brick\:prefetch]`
	selVisible  = `a[brick\:prefetch="visible"]`
	selOptedOut = `a[brick\:prefetch][brick\:nav="off"], [brick\:nav="off"] a[brick\:prefetch]`
	// selMethod is a brick:method link (v23): its click posts, so a GET
	// prefetch of its href fetches a page the click never shows.
	selMethod = `a[brick\:method]`
)

// speculate adds a <script type="speculationrules"> before </body> when the
// page has a brick:prefetch link. Browsers without speculation rules
// ignore the script; nav.js's own prefetch still serves them.
func (s *Site) speculate(b []byte) []byte {
	if !prefetchLinkRE.Match(b) {
		return b
	}
	base := s.opts.BasePath
	nav := bytes.Contains(b, []byte(s.current().eng.AssetBase()+navScriptMark))
	rules := speculationRules(base, nav)
	i := bytes.LastIndex(bytes.ToLower(b), []byte(bodyClose))
	if i < 0 {
		return append(b, rules...)
	}
	out := append([]byte(nil), b[:i]...)
	out = append(out, rules...)
	return append(out, b[i:]...)
}

// speculationRules builds the rules script. Links are limited to the
// site's own paths (base + "/*"): a link out of the base belongs to another
// site of the same origin, whose pages the author did not ask for.
func speculationRules(base string, nav bool) string {
	scope := map[string]any{"href_matches": base + "/*"}
	where := func(sel string, excludeVisible bool) map[string]any {
		and := []any{scope, map[string]any{"selector_matches": sel}, map[string]any{"not": map[string]any{"selector_matches": selMethod}}}
		if nav {
			and = append(and, map[string]any{"selector_matches": selOptedOut})
		}
		if excludeVisible {
			and = append(and, map[string]any{"not": map[string]any{"selector_matches": selVisible}})
		}
		return map[string]any{"and": and}
	}
	rules := map[string]any{"prefetch": []any{
		map[string]any{"where": where(selAll, true), "eagerness": "moderate"},
		map[string]any{"where": where(selVisible, false), "eagerness": "eager"},
	}}
	// json.Marshal escapes <, > and & as \u003c and the like, so the JSON
	// can never close the script element it sits in. It cannot fail on
	// maps of strings and slices.
	js, _ := json.Marshal(rules)
	return `<script type="speculationrules">` + string(js) + `</script>`
}
