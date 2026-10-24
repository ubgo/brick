package tree

import (
	"errors"
	"strings"
	"testing"

	"github.com/ubgo/brick/internal/lex"
)

func build(t *testing.T, src string) (*File, error) {
	t.Helper()
	toks, err := lex.Scan([]byte(src), lex.Options{IsRaw: func(n string, _ []lex.Attr) bool { return n == TagScript }})
	if err != nil {
		t.Fatalf("scan %q: %v", src, err)
	}
	return Build(toks)
}

func TestBuildShapes(t *testing.T) {
	t.Parallel()
	f, err := build(t, `<h1>x</h1>{{ range .P }}<Card :p=".">{{ .T }}<brick:prop name="h"><b>h</b></brick:prop></Card>{{ end }}<brick:component name="Price">$</brick:component><brick:script>def f(): pass</brick:script>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Nodes) != 4 { // text, range action, Card, end action
		t.Fatalf("nodes = %d", len(f.Nodes))
	}
	card, ok := f.Nodes[2].(*Element)
	if !ok || card.Name != "Card" || len(card.Children) != 1 || len(card.Props) != 1 {
		t.Fatalf("card = %+v", f.Nodes[2])
	}
	if len(f.Locals) != 1 || len(f.Blocks) != 1 {
		t.Fatalf("locals=%d blocks=%d", len(f.Locals), len(f.Blocks))
	}
}

func TestStructuralRules(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, msg string
	}{
		{"T1 straddle else", `{{ if .B }}<Box>{{ else }}<div>{{ end }}x{{ if .B }}</Box>{{ end }}`, "closes a block opened outside <Box>"},
		{"T1 close in other branch", `{{ if .B }}<Box>{{ else }}</Box>{{ end }}`, "closes a block opened outside"},
		{"T1 close deeper", `<Box>{{ if .B }}</Box>{{ end }}`, "different template branch"},
		{"break crossing body", `{{ range .P }}<Card>{{ break }}</Card>{{ end }}`, "cannot leave the {{ range }}"},
		{"break without range in body", `<Card>{{ break }}</Card>`, "outside any {{ range }}"},
		{"break in define without range", `{{ define "r" }}{{ break }}{{ end }}`, "outside any {{ range }}"},
		{"define in body", `<Card>{{ define "x" }}{{ end }}</Card>`, "move it to the top level"},
		{"prop outside component", `<brick:prop name="a">x</brick:prop>`, "directly inside a component"},
		{"prop in condition", `<Card>{{ if .X }}<brick:prop name="a">x</brick:prop>{{ end }}</Card>`, "put the condition inside"},
		{"prop named children", `<Card><brick:prop name="children">x</brick:prop></Card>`, "children is reserved"},
		{"prop bad name", `<Card><brick:prop name="{{ .x }}">x</brick:prop></Card>`, "plain identifier"},
		{"prop twice", `<Card><brick:prop name="a">1</brick:prop><brick:prop name="a">2</brick:prop></Card>`, "given twice"},
		{"component nested", `<Card><brick:component name="P">x</brick:component></Card>`, "top level"},
		{"reserved tag", `<brick:slot>x</brick:slot>`, "is reserved"},
		{"mismatched close", `<Card>x</x-card>`, "must match its open tag"},
		{"unclosed", `<Card>x`, "has no closing"},
		{"stray close", `x</Card>`, "has no opening tag"},
		{"unclosed block", `{{ if .X }}a`, "has no {{ end }}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := build(t, tc.src)
			var te *Error
			if !errors.As(err, &te) || !strings.Contains(te.Msg, tc.msg) {
				t.Fatalf("err = %v, want %q", err, tc.msg)
			}
		})
	}
}

func TestValidNesting(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`{{ range .P }}{{ if .A }}<Card>{{ if .B }}x{{ else }}y{{ end }}</Card>{{ end }}{{ end }}`,
		`<Card>{{ range .I }}{{ break }}{{ end }}</Card>`,
		`{{ if .A }}<Card>a</Card>{{ else }}<Card>b</Card>{{ end }}`,
		`{{ define "row" }}<Card />{{ end }}`,
	} {
		if _, err := build(t, src); err != nil {
			t.Fatalf("%s: %v", src, err)
		}
	}
}
