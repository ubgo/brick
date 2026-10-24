package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// The server side of the morph controls and transitions: keyed rows in
// the order the state says, the control attributes passed through for the
// runtime, and each action's transition choice as an effect.
func TestTodos(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page)
	if !strings.Contains(live.HTML(), `<button type="button" aria-expanded="false" brick:js.click='toggle("#menu-list"); toggleAttr("this", "aria-expanded", "true", "false")'>Menu</button>`) {
		t.Fatalf("client commands pass through to the page:\n%s", live.HTML())
	}
	todos := live.Component("Todos")
	todos.Click("#add")
	res := todos.Click("#add")
	bricktest.AssertNoEffect(t, res, brick.EffectTransition) // the root's brick:transition="slide" animates it
	if !strings.Contains(todos.HTML(), `<ul><li brick:key="2">Write docs #2</li><li brick:key="1">Write docs #1</li></ul>`) {
		t.Fatalf("keyed rows, newest first:\n%s", todos.HTML())
	}
	res = todos.Click("#shuffle")
	bricktest.AssertEffect(t, res, brick.Effect{Kind: brick.EffectTransition, Name: "shuffle"})
	if !strings.Contains(todos.HTML(), `<li brick:key="1">Write docs #1</li><li brick:key="2">`) {
		t.Fatalf("reversed:\n%s", todos.HTML())
	}
	res = todos.Click("#quiet")
	bricktest.AssertEffect(t, res, brick.Effect{Kind: brick.EffectTransition, Name: "none"})
	if !strings.Contains(todos.HTML(), `<div id="map" brick:ignore></div>`) || !strings.Contains(todos.HTML(), `<div id="chart" brick:replace>2 rows</div>`) {
		t.Fatalf("morph controls:\n%s", todos.HTML())
	}
}

// What the compiler refuses, with the message a user sees.
func TestRefused(t *testing.T) {
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		t.Fatal(err)
	}
	stateful := "---\nstate: {n: 0}\n---\n"
	cases := map[string]string{
		stateful + `<div brick:ignore>x</div>`:                                  "brick:ignore on a stateful component's root would keep the old root after every action",
		`<div brick:transition="slide">x</div>`:                                 "brick:transition",
		`<button brick:js.click='setAttr("this", "onclick", "x()")'>x</button>`: `setAttr: "onclick"`,
		`<button brick:js.click='open("#m")'>x</button>`:                        `unknown command "open"`,
		`<button brick:js.click='toggle(#m)'>x</button>`:                        "arguments must be JSON literals",
		`<ul brick:key="">x</ul>`:                                               "brick:key needs a value",
	}
	for comp, want := range cases {
		eng, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), FS: fstest.MapFS{
			"components/Box.html": {Data: []byte(comp)},
			"content/index.html":  {Data: []byte(`<Box />`)},
		}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = eng.RenderChunks(context.Background(), "content/index.html", nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s:\n  err  = %v\n  want %q", comp, err, want)
		}
	}
}

// scriptRE finds the component module the page links.
var scriptRE = regexp.MustCompile(`src="(/_brick/c/Menu[^"]+\.js)"`)

// The Menu's client module reaches the page and calls brick.js with the
// command list the markup carries. Running it (the menu closing after a
// navigation) takes a browser; this checks both halves are served.
func TestMenuClientServed(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	get := func(path string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		return rec.Body.String()
	}
	page := get("/")
	if !strings.Contains(page, `data-on-close="hide(&#34;#menu-list&#34;)"`) && !strings.Contains(page, `data-on-close='hide("#menu-list")'`) {
		t.Fatalf("the command list is not in the markup:\n%s", page)
	}
	m := scriptRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no Menu module linked:\n%s", page)
	}
	if js := get(m[1]); !strings.Contains(js, `brick.mount("Menu"`) || !strings.Contains(js, `brick.js(menu, menu.getAttribute("data-on-close"))`) {
		t.Fatalf("module:\n%s", js)
	}
}
