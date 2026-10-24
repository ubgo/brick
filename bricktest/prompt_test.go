package bricktest_test

import (
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// brick:confirm.prompt appends the visitor's answer as the call's last
// argument; Click refuses such an element, so a test cannot skip the
// answer by accident.
func TestClickPrompt(t *testing.T) {
	eng := func(tb testing.TB) *brick.Engine {
		return bricktest.Engine(tb, brick.Options{States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
			"components/Ren.html", "---\nstate:\n  name: \"\"\n---\n<div><button id=\"ren\" brick:confirm.prompt=\"New name?\" brick:click='$set(\"name\")'>Rename</button><button id=\"plain\" brick:click='$set(\"name\", \"x\")'>x</button></div>",
			"content/a.html", `<main><Ren /></main>`,
		))
	}
	r := bricktest.Live(t, eng(t), "content/a.html").Component("Ren")
	r.ClickPrompt("#ren", "Bob")
	r.AssertSet(t, "name", "Bob")
	if !fails(t, func(ft testing.TB) { bricktest.Live(ft, eng(t), "content/a.html").Component("Ren").Click("#ren") }) {
		t.Error("Click on a .prompt element did not fail")
	}
	if !fails(t, func(ft testing.TB) {
		bricktest.Live(ft, eng(t), "content/a.html").Component("Ren").ClickPrompt("#plain", "x")
	}) {
		t.Error("ClickPrompt without .prompt did not fail")
	}
}
