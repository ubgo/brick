package lsp

import (
	"slices"
	"strings"
	"testing"
)

// at classifies text at the `|` marker (removed before classifying).
func at(t *testing.T, marked string) Context {
	t.Helper()
	off := strings.Index(marked, "|")
	if off < 0 {
		t.Fatalf("no | in %q", marked)
	}
	return Classify(marked[:off]+marked[off+1:], off)
}

func TestClassifyMarkup(t *testing.T) {
	tests := []struct {
		name, text string
		kind       Kind
		tag, attr  string
		prefix     string
		word       string
		written    []string
	}{
		{"tag name prefix", "<p>\n<Ca|rd title=\"x\">", KindTagName, "Card", "", "Ca", "Card", nil},
		{"hyphen tag", "<x-post-l|", KindTagName, "x-post-l", "", "x-post-l", "x-post-l", nil},
		{"after < alone", "<|", KindNone, "", "", "", "", nil},
		{"attr position", `<Card title="Pro" |`, KindAttrName, "Card", "", "", "", []string{"title"}},
		{"attr prefix", `<Card title="Pro" vari|`, KindAttrName, "Card", "", "vari", "vari", []string{"title"}},
		{"attr word under cursor", `<Card ti|tle="Pro">`, KindAttrName, "Card", "", "ti", "title", nil},
		{"inside value", `<Card variant="fe|`, KindAttrValue, "Card", "variant", "", "", []string{"variant"}},
		{"after a closed tag", `<Card title="x">te|xt`, KindNone, "", "", "", "", nil},
		{"closing tag", `</Ca|rd>`, KindNone, "", "", "", "", nil},
		{"inside an action", `<Card title="{{ .Props.x }}" {{ if .x| }}`, KindNone, "", "", "", "", nil},
		{"after a conditional action", `<Button {{ if .x }}dis|`, KindAttrName, "Button", "", "dis", "dis", nil},
		{"after an action", `<Card {{ attrs }} :posts=".x" |`, KindAttrName, "Card", "", "", "", []string{":posts"}},
		{"multi-line tag", "<Card\n  title=\"a\"\n  va|", KindAttrName, "Card", "", "va", "va", []string{"title"}},
		{"bool attr then cursor", `<Button block |`, KindAttrName, "Button", "", "", "", []string{"block"}},
		{"text with a bare <", "if x < 1|0", KindNone, "", "", "", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := at(t, tt.text)
			if c.Kind != tt.kind || c.Tag != tt.tag || c.Attr != tt.attr || c.Prefix != tt.prefix || c.Word != tt.word || !slices.Equal(c.Written, tt.written) {
				t.Fatalf("got %+v", c)
			}
		})
	}
}

func TestClassifyFrontmatter(t *testing.T) {
	tests := []struct {
		name, text string
		kind       Kind
		path       []string
		prefix     string
	}{
		{"top-level key", "---\nlay|\n---\n<p>", KindFrontmatterKey, nil, "lay"},
		{"empty top line", "---\ntitle: x\n|\n---\n", KindFrontmatterKey, nil, ""},
		{"value", "---\nsyntax: |\n---\n", KindFrontmatterValue, []string{"syntax"}, ""},
		{"block child", "---\nbrick:\n  isl|\n---\n", KindFrontmatterKey, []string{"brick"}, "isl"},
		{"block grandchild value", "---\nbrick:\n  island: t|\n---\n", KindFrontmatterValue, []string{"brick", "island"}, "t"},
		{"flow mapping key", "---\nbrick: {isl|\n---\n", KindFrontmatterKey, []string{"brick"}, "isl"},
		{"flow mapping value", "---\nbrick: {island: |\n---\n", KindFrontmatterValue, []string{"brick", "island"}, ""},
		{"prop settings in a block", "---\nprops:\n  variant: {type: string, def|\n---\n", KindFrontmatterKey, []string{"props", "variant"}, "def"},
		{"flow list value", "---\nstate:\n  tags: [a, |\n---\n", KindFrontmatterValue, []string{"state", "tags", ListItem}, ""},
		{"closed flow", "---\nbrick: {island: true}\n|\n---\n", KindFrontmatterKey, nil, ""},
		{"block list item", "---\nslots:\n  - na|\n---\n", KindFrontmatterKey, []string{"slots", ListItem}, "na"},
		{"in a list item's mapping", "---\nrows:\n  - title: x\n    do|\n---\n", KindFrontmatterKey, []string{"rows", ListItem}, "do"},
		{"sibling items", "---\nrows:\n  - a: 1\n  - b|\n---\n", KindFrontmatterKey, []string{"rows", ListItem}, "b"},
		{"comment", "---\n# a no|te\n---\n", KindNone, nil, ""},
		{"unclosed block", "---\nla|", KindFrontmatterKey, nil, "la"},
		{"markup after the block", "---\na: b\n---\n<Ca|", KindTagName, nil, "Ca"},
		{"no frontmatter", "title: x|", KindNone, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := at(t, tt.text)
			if c.Kind != tt.kind || !slices.Equal(c.Path, tt.path) || c.Prefix != tt.prefix {
				t.Fatalf("got kind %d path %q prefix %q", c.Kind, c.Path, c.Prefix)
			}
		})
	}
}

// TestWordStartIsAbsolute pins that a frontmatter word's offset is in the
// whole file, not the block, so hover and completion ranges land on it.
func TestWordStartIsAbsolute(t *testing.T) {
	text := "---\ntitle: x\nlayout: y\n---\n"
	off := strings.Index(text, "layout") + 3
	c := Classify(text, off)
	if got := text[c.WordStart : c.WordStart+len(c.Word)]; got != "layout" {
		t.Fatalf("word at WordStart = %q, want layout", got)
	}
}

func TestPositions(t *testing.T) {
	text := "a\n😀é<Card"
	// 😀 is two UTF-16 units and four bytes; é is one unit and two bytes.
	off := OffsetOf(text, 1, 3)
	if text[off:] != "<Card" {
		t.Fatalf("OffsetOf = %d (%q)", off, text[off:])
	}
	if l, c := PositionOf(text, off); l != 1 || c != 3 {
		t.Fatalf("PositionOf = %d:%d, want 1:3", l, c)
	}
	if got := OffsetOf(text, 9, 0); got != len(text) {
		t.Fatalf("past the end = %d", got)
	}
	if got := OffsetOf(text, 0, 99); got != 1 {
		t.Fatalf("past the line end = %d, want 1", got)
	}
}

func TestHyphenName(t *testing.T) {
	for name, want := range map[string]string{"Card": "x-card", "PostList": "x-post-list", "KitAppShell": "x-kit-app-shell", "UIButton": "x-ui-button", "H1": "x-h1"} {
		if got := HyphenName(name); got != want {
			t.Errorf("HyphenName(%s) = %s, want %s", name, got, want)
		}
		if foldHyphen(strings.TrimPrefix(HyphenName(name), hyphenTagPrefix)) != foldHyphen(name) {
			t.Errorf("%s does not fold back", name)
		}
	}
}
