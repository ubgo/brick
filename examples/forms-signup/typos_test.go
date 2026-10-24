package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// formComponent is a stateful component with one action around body.
const formComponent = "---\nstate:\n  name: \"\"\n  tags: []\n---\n<brick:script>\ndef save(state, props):\n    return state\n</brick:script>\n"

// typoEngine compiles a one-component theme whose form is body.
func typoEngine(t *testing.T, body string) (*brick.Engine, error) {
	t.Helper()
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{
		"content/index.html": {Data: []byte(`<F />`)},
		"components/F.html":  {Data: []byte(formComponent + body + "\n")},
	}
	eng, err := brick.New(brick.Options{FS: fsys, Scripting: sc, States: brick.NewMemoryStates(0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Render(context.Background(), "content/index.html", nil)
	return eng, err
}

// A misspelt field or modifier fails when the file is read, with the file,
// line and column and the allowed list.
func TestFormTyposFailAtCompile(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{`<form brick:submit="save"><input brick:model="nmae"></form>`, `components/F.html:10:40: brick:model="nmae": "nmae" is not a field of this component's state (declared: name, tags)`},
		{`<form brick:submit="save"><input brick:model.defer.lazy="name"></form>`, `components/F.html:10:40: brick:model.defer.lazy: .defer cannot be combined with .lazy (brick:model accepts .lazy, .defer, .debounce.<ms>, .throttle.<ms>)`},
		{`<form brick:submit="save"><button brick:click.onec="save">x</button></form>`, `components/F.html:10:41: brick:click.onec: unknown modifier "onec" (brick:click accepts .once, .debounce.<ms>, .throttle.<ms>, .optimistic, .outside, .details)`},
		{`<form brick:submit="save"><span brick:loading="sve" hidden>x</span></form>`, `brick:loading: "sve" is not a function of this component (have: save, $refresh)`},
	} {
		_, err := typoEngine(t, c.body)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s:\n got %v\nwant %s", c.body, err, c.want)
		}
	}
}

// The compiler checks an action call's syntax, not its name: a misspelt
// brick:submit compiles, and the click is refused when it reaches the
// server.
func TestMisspeltActionFailsAtRunTime(t *testing.T) {
	eng, err := typoEngine(t, `<form brick:submit="sve"></form>`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	h := handler(eng)
	html, err := eng.Render(context.Background(), "content/index.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := instanceRE.FindStringSubmatch(string(html))
	if m == nil {
		t.Fatalf("no stateful root:\n%s", html)
	}
	code, body := post(t, h, `{"handle":"`+m[1]+`","token":"`+m[2]+`","action":"sve"}`)
	if code != http.StatusBadRequest || strings.TrimSpace(body) != `brick: unknown action "sve" on <F>` {
		t.Fatalf("%d %q", code, body)
	}
}
