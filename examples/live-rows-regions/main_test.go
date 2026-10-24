package main

import (
	"os"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// The whole flow over real HTTP; testdata/transcript.txt is the output
// docs/guides/live/rows-and-regions.md shows under "Try it".
func TestTranscript(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := demo(&out, a.handler()); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/transcript.txt")
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("transcript:\n%s\nwant:\n%s", out.String(), want)
	}
}

// A row is a component's render, so a user's text arrives escaped.
func TestRowTextIsEscaped(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page)
	res := live.Component("Chat").Call("send", "<b>hi</b>")
	rows := bricktest.Effects(res, brick.EffectRows)
	if len(rows) != 1 || string(rows[0].Rows[0].HTML) != `<li class="msg">&lt;b&gt;hi&lt;/b&gt;</li>` {
		t.Fatalf("rows: %+v", rows)
	}
}

// The pitfalls the guide lists, each with the error a user sees.
func TestPitfalls(t *testing.T) {
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body, want string
	}{
		{"row without a key", `brick.rows("log", component = "Msg", items = [{"text": "x"}])`, `item 0 has no "id" field to key its row by`},
		{"region after skip_render", "brick.skip_render()\n    brick.region(\"log\")", `the action skipped its render (brick.skip_render), and a region is a piece of that render`},
		{"skip_render after region", "brick.region(\"log\")\n    brick.skip_render()", `the action skipped its render (brick.skip_render), and a region is a piece of that render`},
		{"rows without a component", `brick.rows("log", items = [{"id": 1}])`, `an insert needs the component that renders each item`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng := bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
				"components/Msg.html", `<li>{{ .Props.text }}</li>`,
				"components/Go.html", "---\nstate: {}\n---\n<brick:script>\ndef go(state, props):\n    "+tc.body+"\n    return state\n</brick:script>\n<div><ul brick:ref=\"log\" brick:ignore></ul><button brick:click=\"go\">Go</button></div>",
				"content/index.html", `<Go />`,
			))
			live := bricktest.Live(t, eng, "content/index.html")
			_, err := live.Component("Go").TryCall("go")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
