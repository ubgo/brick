package site

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// sessionBinder binds every request to one fixed session id.
type sessionBinder string

func (b sessionBinder) Session(*http.Request) (string, error) { return string(b), nil }

// The Dev inspector lists live instances by component, age, size and a
// short session hash, and never shows state values, the handle or the
// session id.
func TestInspectorListsInstancesWithoutValues(t *testing.T) {
	t.Parallel()
	fsys := toggleSite()
	// A distinctive state value, to prove the page never shows it.
	fsys["components/Toggle.html"].Data = []byte("---\nstate:\n  on: false\n  secret: s3cr3t-value\n---\n<brick:script>toggle</brick:script>\n<div><b>{{ .State.on }}</b><button brick:click=\"flip\">x</button></div>")
	const session = "visitor-session-id-123"
	s := newSite(t, fsys, Options{Dev: true, Engine: brick.Options{Scripting: flipScripting{}, Sessions: sessionBinder(session)}})
	h := s.Handler()
	if _, handle, _ := get(t, h, "/toggle/"); handle == "" {
		t.Fatal("no stateful instance rendered")
	}
	rec, _, _ := get(t, h, "/"+instancesEndpoint)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Toggle") || !strings.Contains(body, "components/Toggle.html") {
		t.Errorf("component not listed:\n%s", body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	var inst *brick.Instance
	_ = s.states.(brick.Lister).Each(t.Context(), func(i *brick.Instance) bool { inst = i; return false })
	if inst == nil || inst.Session == "" {
		t.Fatalf("instance not session-bound: %+v", inst)
	}
	if !strings.Contains(body, inst.Session[:sessionPrefix]+"…") {
		t.Errorf("short session hash missing:\n%s", body)
	}
	for _, leak := range []string{"s3cr3t-value", session, inst.Session, inst.Handle, inst.Token} {
		if strings.Contains(body, leak) {
			t.Errorf("inspector shows %q:\n%s", leak, body)
		}
	}
	size := regexp.MustCompile(`<td class="n">(\d+)</td><td class="n">\d+</td>`).FindStringSubmatch(body)
	if size == nil || size[1] == "0" {
		t.Errorf("no size shown:\n%s", body)
	}
}

// The inspector is a Dev tool: with Dev off the path is an ordinary
// unknown URL (the 404 page), whatever the store. This is the guard that
// keeps instance metadata off a production server.
func TestInspectorUnreachableInProduction(t *testing.T) {
	t.Parallel()
	s := newSite(t, toggleSite(), Options{Engine: brick.Options{Scripting: flipScripting{}, States: brick.NewMemoryStates(0, 0)}})
	h := s.Handler()
	get(t, h, "/toggle/")
	rec, _, _ := get(t, h, "/"+instancesEndpoint)
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "Toggle") {
		t.Fatalf("production answered the inspector: %d\n%s", rec.Code, rec.Body)
	}
}

// A store that cannot list (sealed state keeps nothing server-side) gets a
// 404, not an empty table that would read as "nothing is alive".
func TestInspectorNotFoundWithoutLister(t *testing.T) {
	t.Parallel()
	s := newSite(t, toggleSite(), Options{Dev: true, StateKeys: testKeys(t, 1), Engine: brick.Options{Scripting: flipScripting{}}})
	rec, _, _ := get(t, s.Handler(), "/"+instancesEndpoint)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

// Speculation rules go only on pages with a brick:prefetch link. Without
// nav.js they cover every such link; with it, only links that opted out of
// navigation (nav.js prefetches the rest itself). Links stay in the base.
func TestSpeculationRules(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["layouts/default.html"] = &fstest.MapFile{Data: []byte("<html><body>{{ children }}{{ assets }}</body></html>")}
	fsys["content/pf.html"] = &fstest.MapFile{Data: []byte(`<a href="/about/" brick:prefetch>a</a><a href="/blog/" brick:prefetch="visible">b</a>`)}
	cases := []struct {
		name     string
		opts     Options
		url      string
		want     bool
		optedOut bool
		scope    string
	}{
		{"no prefetch link", Options{}, "/about/", false, false, ""},
		{"no nav", Options{}, "/pf/", true, false, `"/*"`},
		{"nav", Options{Engine: brick.Options{Navigation: true}}, "/pf/", true, true, `"/*"`},
		{"base path", Options{BasePath: "/docs"}, "/docs/pf/", true, false, `"/docs/*"`},
	}
	for _, c := range cases {
		s := newSite(t, fsys, c.opts)
		rec, _, _ := get(t, s.Handler(), c.url)
		body := rec.Body.String()
		i := strings.Index(body, `<script type="speculationrules">`)
		if (i >= 0) != c.want {
			t.Errorf("%s: rules present = %v\n%s", c.name, i >= 0, body)
			continue
		}
		if !c.want {
			continue
		}
		js := body[i+len(`<script type="speculationrules">`):]
		js = js[:strings.Index(js, "</script>")]
		var rules map[string]any
		if err := json.Unmarshal([]byte(js), &rules); err != nil {
			t.Errorf("%s: rules are not JSON: %v\n%s", c.name, err, js)
		}
		if got := strings.Contains(js, `brick\\:nav=\"off\"`); got != c.optedOut {
			t.Errorf("%s: opted-out restriction = %v\n%s", c.name, got, js)
		}
		if !strings.Contains(js, `"href_matches":`+c.scope) || !strings.Contains(js, `"eagerness":"eager"`) || !strings.Contains(js, `"eagerness":"moderate"`) {
			t.Errorf("%s: rules\n%s", c.name, js)
		}
	}
}
