package brick

import (
	"testing"
)

// locTheme has a single-root component whose root is on line 5 of its file
// (after frontmatter and a comment), one with two roots, and one with none.
func locTheme() Options {
	return Options{FS: theme(
		"components/Card.html", "---\nprops:\n  title: {type: string}\n---\n<article\n  class=\"card\"><h2>{{ .Props.title }}</h2>{{ children }}</article>",
		"components/Pair.html", "<b>a</b><b>b</b>",
		"components/Bare.html", "just text",
		"content/a.html", `<Card title="T">x</Card><Pair /><Bare />`,
	)}
}

// In Dev mode a single-root component's root names its source file and
// line, where data-brick-up goes; other components get nothing.
func TestLocDevOnly(t *testing.T) {
	t.Parallel()
	dev := locTheme()
	dev.Mode = Dev
	got := render(t, newEngine(t, dev), "content/a.html", nil)
	want := `<article data-brick-loc="components/Card.html:5"` + "\n" + `  class="card"><h2>T</h2>x</article><b>a</b><b>b</b>just text`
	if got != want {
		t.Fatalf("dev:\n got %s\nwant %s", got, want)
	}
}

// Production bytes are unchanged: no data-brick-loc outside Dev.
func TestLocNeverInProd(t *testing.T) {
	t.Parallel()
	got := render(t, newEngine(t, locTheme()), "content/a.html", nil)
	want := "<article\n" + `  class="card"><h2>T</h2>x</article><b>a</b><b>b</b>just text`
	if got != want {
		t.Fatalf("prod:\n got %s\nwant %s", got, want)
	}
}
