package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// referencePage is the committed page this program generates.
const referencePage = "../../docs/reference/frontmatter.md"

// The committed reference is exactly what the generator prints, so a key
// the engine gains (which the schema then has, and which write refuses
// without a note) cannot be missing from the page.
func TestReferenceIsCurrent(t *testing.T) {
	var out bytes.Buffer
	if err := write(&out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(referencePage)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != out.String() {
		t.Errorf("%s is stale; run: go run . > %s", referencePage, referencePage)
	}
}

// A schema key without a note is an error naming it, never a silent row
// with no default or guide.
func TestUndescribedKeyFails(t *testing.T) {
	saved := brickNotes["css"]
	delete(brickNotes, "css")
	defer func() { brickNotes["css"] = saved }()
	err := write(&bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "brick.css has no note") {
		t.Fatalf("err = %v", err)
	}
}

// Every schema description reaches the page with its angle brackets
// escaped, so <Card> shows as text instead of vanishing as a tag.
func TestCellsAreLiteral(t *testing.T) {
	var out bytes.Buffer
	if err := write(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "capitalized (&lt;Card&gt;)") {
		t.Error("the syntax row lost its escaped <Card>")
	}
}
