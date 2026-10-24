package brick

import (
	"strings"
	"testing"
)

// The tag built-in writes an element whose name is data.
func TestTagBuiltin(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Heading.html", "---\nprops:\n  level: {type: int, default: 2}\n---\n"+
			`{{ $h := print "h" .Props.level }}{{ tag $h "class" "title" "data-x" .Props.note "hidden" false "open" true }}{{ children }}{{ endtag $h }}`,
		"content/a.html", "<Heading :level=\"3\" :note=\"printf `a%c%cb` 34 60\">Hi</Heading><Heading>Two</Heading>",
	)})
	got := render(t, e, "content/a.html", nil)
	want := `<h3 class="title" data-x="a&#34;&lt;b" open>Hi</h3><h2 class="title" open>Two</h2>`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// tag refuses every element off writableTags and every attribute that
// html/template would have had to escape by context.
func TestTagRefusesUnsafe(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		attrs []any
		want  string
	}{
		{"script", nil, "not an element tag can write"},
		{"a", nil, "not an element tag can write"},
		{"IFRAME", nil, "not an element tag can write"},
		{"h2 onclick", nil, "not an element tag can write"},
		{"", nil, "not an element tag can write"},
		{"div", []any{"onclick", "x"}, "not an attribute tag can write"},
		{"div", []any{"OnMouseOver", "x"}, "not an attribute tag can write"},
		{"div", []any{"href", "x"}, "not an attribute tag can write"},
		{"div", []any{"style", "x"}, "not an attribute tag can write"},
		{"div", []any{"data-src", "x"}, "not an attribute tag can write"},
		{"div", []any{`a"b`, "x"}, "not an attribute tag can write"},
		{"div", []any{1, "x"}, "not an attribute tag can write"},
		{"div", []any{"class"}, "name, value pairs"},
	} {
		if _, err := tagOpen(c.name, c.attrs...); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("tag %q %v: err = %v, want %q", c.name, c.attrs, err, c.want)
		}
	}
	if _, err := tagClose("script"); err == nil {
		t.Error("endtag script: want an error")
	}
	// The refusal surfaces as a render error, not as markup.
	e := newEngine(t, Options{FS: theme("content/a.html", `{{ tag (print "scr" "ipt") }}x`)})
	if err := renderErr(t, e, "content/a.html"); !strings.Contains(err.Error(), "not an element tag can write") {
		t.Fatalf("err = %v", err)
	}
}
