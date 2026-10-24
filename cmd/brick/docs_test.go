package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

// docsCard documents a prop and a slot, with a markup example that JSON
// must not escape.
const docsCard = "---\nprops:\n" +
	"  title: {type: string, required: true, doc: The heading., examples: ['<Card title=\"Hi\" />']}\n" +
	"  tone: {type: string, default: plain, oneof: [plain, loud]}\n" +
	"brick:\n  slots:\n    footer: {doc: Actions under the body.}\n---\n<div>{{ .Props.title }}</div>"

func TestDocsCLI(t *testing.T) {
	dir := project(t,
		"brick.yaml", "roots: [components]\n",
		"components/Card.html", docsCard,
		"components/Box.html", "<div></div>",
		"content/index.html", "<Box />",
	)
	code, out, errs := cli(t, "docs", dir)
	if code != 0 {
		t.Fatalf("docs: %d %s", code, errs)
	}
	for _, want := range []string{
		"Box  components/Box.html\n",
		"Card  components/Card.html\n  props:\n    title  string  required\n      The heading.\n      e.g. <Card title=\"Hi\" />\n",
		"    tone  string  default \"plain\"  one of \"plain\", \"loud\"\n",
		"  slots:\n    footer\n      Actions under the body.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text listing lacks %q:\n%s", want, out)
		}
	}
	code, out, errs = cli(t, "docs", "-json", dir)
	var list []brick.ComponentDoc
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list) != 2 {
		t.Fatalf("docs -json: %d %s %s", code, out, errs)
	}
	if !strings.Contains(out, `"<Card title=\"Hi\" />"`) {
		t.Fatalf("markup was escaped:\n%s", out)
	}
	if code, _, errs := cli(t, "docs", "-h"); code != 0 || !strings.Contains(errs, "Examples:") {
		t.Fatalf("-h: %d %s", code, errs)
	}
}

func TestDocsCLIFailures(t *testing.T) {
	bad := project(t,
		"brick.yaml", "roots: [components]\n",
		"components/Card.html", "---\nprops:\n  title: {type: string, doc: [not, text]}\n---\n<div></div>",
		"content/index.html", "<p>x</p>",
	)
	if code, _, errs := cli(t, "docs", bad); code != exitFail || !strings.Contains(errs, "Card.html") {
		t.Fatalf("bad frontmatter: %d %s", code, errs)
	}
	if code, _, _ := cli(t, "docs", "a", "b"); code != exitUsage {
		t.Fatalf("two dirs: %d", code)
	}
}
