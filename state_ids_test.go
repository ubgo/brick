package brick

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// Components nested in a stateful root take ids scoped by that root's
// instance id: two copies of the root on one page give their children
// different ids (label/for, aria-controls), and an action's re-render of a
// root gives its children the same ids as its first render, so the morph
// does not churn them.
func TestNestedIDsScopedByStatefulRoot(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: counterScripting(), FS: theme(
		"components/Kid.html", `<b id="kid-{{ .ID }}"></b>`,
		"components/Counter.html", "---\nstate:\n  count: 0\nprops:\n  step: {type: int, default: 1}\n---\n<brick:script>counter</brick:script>\n<div><span>{{ .State.count }}</span><Kid /></div>",
		"content/a.html", `<Counter /><Counter />`,
	)})
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	kidRE := regexp.MustCompile(`id="kid-(b[0-9a-f]+)"`)
	kids := kidRE.FindAllStringSubmatch(string(page), -1)
	if len(kids) != 2 || kids[0][1] == kids[1][1] {
		t.Fatalf("two copies need two nested ids: %s", page)
	}
	roots := stateRE.FindAllStringSubmatch(string(page), -1)
	if len(roots) != 2 {
		t.Fatalf("want two stateful roots: %s", page)
	}
	for i, m := range roots {
		out, err := e.Action(ctx, ActionRequest{Handle: m[1], Token: m[2], Action: "increment"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "<span>1</span>") {
			t.Fatalf("action did not re-render copy %d: %s", i, out)
		}
		got := kidRE.FindStringSubmatch(string(out))
		if got == nil || got[1] != kids[i][1] {
			t.Fatalf("copy %d: re-render nested id %v, first render %s", i, got, kids[i][1])
		}
	}
}
