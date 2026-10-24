package format

import (
	"errors"
	"strings"
	"testing"
)

// formatTwice formats in with opts and checks that formatting the result
// again changes nothing, the guarantee every option keeps.
func formatTwice(t *testing.T, in string, opts Options) string {
	t.Helper()
	res, err := Format([]byte(in), opts)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Format(res.Src, opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(again.Src) != string(res.Src) {
		t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", res.Src, again.Src)
	}
	return string(res.Src)
}

func TestLineLength(t *testing.T) {
	cases := []struct {
		name string
		max  int
		in   string
		want string
	}{
		{"a long tag gets one attribute per line, > on the last",
			30,
			"<div>\n<a href=\"/x\"   class=\"btn btn-primary\" id=\"go\">Go</a>\n</div>\n",
			"<div>\n  <a\n    href=\"/x\"\n    class=\"btn btn-primary\"\n    id=\"go\">Go</a>\n</div>\n"},
		{"a line within the limit is left on one line",
			80,
			"<a href=\"/x\" class=\"btn\">Go</a>\n",
			"<a href=\"/x\" class=\"btn\">Go</a>\n"},
		{"zero never wraps",
			0,
			"<a href=\"/x\" class=\"btn btn-primary btn-large btn-block\" id=\"go\">Go</a>\n",
			"<a href=\"/x\" class=\"btn btn-primary btn-large btn-block\" id=\"go\">Go</a>\n"},
		{"a component tag wraps and keeps its />",
			20,
			"<Card title=\"Hello\" tone=\"info\" />\n",
			"<Card\n  title=\"Hello\"\n  tone=\"info\" />\n"},
		{"a tag with one attribute has nothing to break",
			10,
			"<a href=\"/a-very-long-path\">x</a>\n",
			"<a href=\"/a-very-long-path\">x</a>\n"},
		{"no break next to an action or around =",
			20,
			"<div {{ attrs }} class = \"wide-class-name\" id=\"x\" title=\"t\">a</div>\n",
			"<div {{ attrs }} class=\"wide-class-name\"\n  id=\"x\"\n  title=\"t\">a</div>\n"},
		{"one break point is not enough to wrap",
			20,
			"<div {{ attrs }} class=\"wide-class-name\" id=\"x\">a</div>\n",
			"<div {{ attrs }} class=\"wide-class-name\" id=\"x\">a</div>\n"},
		{"a tag already spread over lines is not wrapped again",
			10,
			"<a\n  href=\"/x\"\n  class=\"y\">Go</a>\n",
			"<a\n  href=\"/x\"\n  class=\"y\">Go</a>\n"},
		{"no wrap before an element left open later on the line",
			20,
			"<Prose class=\"wide\" size=\"sm\"><table>\n<tr><td>x</td></tr>\n</table></Prose>\n",
			"<Prose class=\"wide\" size=\"sm\"><table>\n  <tr><td>x</td></tr>\n</table></Prose>\n"},
		{"an element opened inside the wrapped tag's line before it still wraps",
			20,
			"<div><Prose class=\"wide\" size=\"sm\">\nx\n</Prose></div>\n",
			"<div><Prose\n  class=\"wide\"\n  size=\"sm\">\n  x\n</Prose></div>\n"},
		{"a quoted value with spaces is one attribute",
			20,
			"<p class=\"a b c d e f\" id=\"p\">x</p>\n",
			"<p\n  class=\"a b c d e f\"\n  id=\"p\">x</p>\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatTwice(t, c.in, Options{LineLength: c.max}); got != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

// TestLineLengthOnlyWhitespace pins the rendering guarantee for wrapping:
// with every whitespace run collapsed to one space, the wrapped file is
// the unwrapped one.
func TestLineLengthOnlyWhitespace(t *testing.T) {
	in := "<ul>\n<li><a href=\"/one\" class=\"link link-primary\" data-x=\"1\">One</a> and <b class=\"x\" id=\"y\">two</b></li>\n</ul>\n"
	plain := formatTwice(t, in, Options{})
	wrapped := formatTwice(t, in, Options{LineLength: 20})
	if plain == wrapped {
		t.Fatal("nothing wrapped")
	}
	collapse := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	if collapse(plain) != collapse(wrapped) {
		t.Errorf("wrapping changed more than whitespace:\n%s\n%s", plain, wrapped)
	}
}

func TestAttrOrder(t *testing.T) {
	order := []string{"brick:*", "id", "class"}
	cases := []struct{ name, in, want string }{
		{"named attributes move to the front, the rest keep their order",
			"<a href=\"/x\" data-x=\"1\" class=\"btn\" id=\"go\">Go</a>\n",
			"<a id=\"go\" class=\"btn\" href=\"/x\" data-x=\"1\">Go</a>\n"},
		{"a prefix entry matches every attribute it starts",
			"<Card title=\"t\" brick:key=\"k\" brick:if=\"x\" />\n",
			"<Card brick:key=\"k\" brick:if=\"x\" title=\"t\" />\n"},
		{"spacing around = is tidied in the moved text",
			"<p title = \"t\" id=\"p\">x</p>\n",
			"<p id=\"p\" title=\"t\">x</p>\n"},
		{"an action between attributes leaves the tag as written",
			"<p title=\"t\" {{ if .X }}class=\"x\"{{ end }} id=\"p\">x</p>\n",
			"<p title=\"t\" {{ if .X }}class=\"x\"{{ end }} id=\"p\">x</p>\n"},
		{"an action inside a quoted value is fine",
			"<p title=\"{{ .T }}\" id=\"p\">x</p>\n",
			"<p id=\"p\" title=\"{{ .T }}\">x</p>\n"},
		{"a repeated attribute leaves the tag as written",
			"<p class=\"a\" id=\"p\" class=\"b\">x</p>\n",
			"<p class=\"a\" id=\"p\" class=\"b\">x</p>\n"},
		{"a tag over several lines is left as written",
			"<p title=\"t\"\n   id=\"p\">x</p>\n",
			"<p title=\"t\"\n   id=\"p\">x</p>\n"},
		{"boolean attributes move too",
			"<input type=\"text\" required id=\"q\">\n",
			"<input id=\"q\" type=\"text\" required>\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatTwice(t, c.in, Options{AttrOrder: order}); got != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

func TestAttrOrderThenWrap(t *testing.T) {
	got := formatTwice(t, "<a href=\"/x\" class=\"btn\" id=\"go\">Go</a>\n", Options{AttrOrder: []string{"id"}, LineLength: 20})
	want := "<a\n  id=\"go\"\n  href=\"/x\"\n  class=\"btn\">Go</a>\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// upper is a stand-in external formatter: it changes every letter, so a
// test sees exactly which bytes went through it.
func upper(b []byte) ([]byte, error) { return []byte(strings.ToUpper(string(b))), nil }

func TestTagFormatters(t *testing.T) {
	fm := map[string]func([]byte) ([]byte, error){tagStyle: upper, tagClient: upper}
	cases := []struct{ name, in, want string }{
		{"the body is dedented, formatted and laid one level under its tag",
			"<div>\n<brick:style>\n        .a{color:red}\n          .b{x:y}\n</brick:style>\n</div>\n",
			"<div>\n  <brick:style>\n    .A{COLOR:RED}\n      .B{X:Y}\n  </brick:style>\n</div>\n"},
		{"a one-line body becomes a block",
			"<brick:client>export default 1</brick:client>\n",
			"<brick:client>\n  EXPORT DEFAULT 1\n</brick:client>\n"},
		{"a body holding an action is left alone",
			"<brick:style>\n.a{color:{{ .C }}}\n</brick:style>\n",
			"<brick:style>\n.a{color:{{ .C }}}\n</brick:style>\n"},
		{"a tag with no formatter is left alone",
			"<brick:script>\nx = 1\n</brick:script>\n",
			"<brick:script>\nx = 1\n</brick:script>\n"},
		{"a no-format region is left alone",
			"{{/* brick:no-format */}}\n<brick:style>\n.a{}\n</brick:style>\n{{/* brick:format */}}\n",
			"{{/* brick:no-format */}}\n<brick:style>\n.a{}\n</brick:style>\n{{/* brick:format */}}\n"},
		{"an empty result leaves the element empty",
			"<brick:style>\n   \n</brick:style>\n",
			"<brick:style></brick:style>\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatTwice(t, c.in, Options{TagFormatters: fm}); got != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

func TestTagFormatterErrors(t *testing.T) {
	fail := func([]byte) ([]byte, error) { return nil, errors.New("prettier: syntax error") }
	_, err := Format([]byte("---\ntitle: x\n---\n<p>a</p>\n<brick:client>\nx(\n</brick:client>\n"), Options{TagFormatters: map[string]func([]byte) ([]byte, error){tagClient: fail}})
	var fe *Error
	if !errors.As(err, &fe) || fe.Line != 5 || !strings.Contains(fe.Msg, "prettier: syntax error") {
		t.Errorf("err = %v, want line 5 naming the formatter's error", err)
	}
	_, err = Format([]byte("<p>a</p>\n"), Options{TagFormatters: map[string]func([]byte) ([]byte, error){"script": upper}})
	if !errors.Is(err, ErrTagFormatter) {
		t.Errorf("err = %v, want ErrTagFormatter for a key that is not an engine tag", err)
	}
}

// TestZeroOptionsUnchanged pins that the new options are off at their zero
// value: a file with long lines, unordered attributes and raw bodies
// formats exactly as with no options at all.
func TestZeroOptionsUnchanged(t *testing.T) {
	in := "<div>\n<a href=\"/x\" class=\"btn btn-primary btn-large btn-block\" id=\"go\">Go</a>\n<brick:style>\n   .a{}\n</brick:style>\n</div>\n"
	a, err := Format([]byte(in), Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Format([]byte(in), Options{LineLength: 0, AttrOrder: nil, TagFormatters: map[string]func([]byte) ([]byte, error){}})
	if err != nil {
		t.Fatal(err)
	}
	if string(a.Src) != string(b.Src) {
		t.Errorf("zero options changed the output:\n%s\n%s", a.Src, b.Src)
	}
}

func TestInline(t *testing.T) {
	in := "<p>\n  Read <a href=\"/x\">\n  the docs</a>\n  <Icon name=\"i\">\n  label</Icon>\n  <div>\n  block\n  </div>\n</p>\n"
	// The default: every element indents its content.
	plain := "<p>\n  Read <a href=\"/x\">\n    the docs</a>\n  <Icon name=\"i\">\n    label</Icon>\n  <div>\n    block\n  </div>\n</p>\n"
	if got := formatTwice(t, in, Options{}); got != plain {
		t.Errorf("default:\n%s\nwant:\n%s", got, plain)
	}
	// Inline elements keep their content at the parent's level; HTML
	// names compare without case, component tags exactly.
	inline := "<p>\n  Read <a href=\"/x\">\n  the docs</a>\n  <Icon name=\"i\">\n  label</Icon>\n  <div>\n    block\n  </div>\n</p>\n"
	if got := formatTwice(t, in, Options{Inline: []string{"A", "Icon"}}); got != inline {
		t.Errorf("inline:\n%s\nwant:\n%s", got, inline)
	}
	if got := formatTwice(t, in, Options{Inline: []string{"a", "icon"}}); !strings.Contains(got, "    label") {
		t.Errorf("a component tag matched without case:\n%s", got)
	}
	// An empty list is the default layout, byte for byte.
	if got := formatTwice(t, in, Options{Inline: []string{}}); got != plain {
		t.Errorf("empty list changed the layout:\n%s", got)
	}
}
