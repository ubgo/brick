package liveclient

import "testing"

// Roots reads the three attributes the engine writes on a stateful root,
// in document order; Find names what it saw when the name is missing.
func TestRootsAndFind(t *testing.T) {
	page := `<p data-brick="b1" data-brick-state="default.h1" data-brick-token="t1" data-brick-name="Badge">0</p>` +
		`<section data-brick="b2" data-brick-state="default.h2" data-brick-token="t2" data-brick-name="Inbox"></section>`
	roots := Roots(page)
	if len(roots) != 2 || *roots[1] != (Root{Name: "Inbox", Handle: "default.h2", Token: "t2"}) {
		t.Fatalf("roots: %+v", roots)
	}
	if _, err := Find(page, "Orders"); err == nil || err.Error() != "no stateful <Orders> on the page (found [Badge Inbox])" {
		t.Fatalf("err = %v", err)
	}
}

// Tidy drops the per-render attributes and layout whitespace, and nothing
// else: an author's own attributes and text stay.
func TestTidy(t *testing.T) {
	for in, want := range map[string]string{
		"<p data-brick=\"b1\" data-brick-state=\"x\" class=\"badge\">\n  1  archived\n</p>": `<p class="badge"> 1 archived </p>`,
		"<ul>\n  <li>a</li>\n  <li>b</li>\n</ul>":                                           `<ul><li>a</li><li>b</li></ul>`,
		`<a data-id="7" brick:click="x">go</a>`:                                             `<a data-id="7" brick:click="x">go</a>`,
	} {
		if got := Tidy(in); got != want {
			t.Errorf("Tidy(%q) = %q, want %q", in, got, want)
		}
	}
}
