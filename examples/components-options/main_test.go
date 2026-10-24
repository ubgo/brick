package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// TestOutput compares what the program prints with testdata/output.txt,
// the text the guides show.
func TestOutput(t *testing.T) {
	want, err := os.ReadFile("testdata/output.txt")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), &out, "theme"); err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}

// With two roots the earlier one wins a name that both define, and the
// shadowing is logged as a warning rather than failing.
func TestEarlierRootWins(t *testing.T) {
	fsys := fstest.MapFS{
		"components/Note.html": {Data: []byte("<i>first</i>")},
		"shared/Note.html":     {Data: []byte("<i>second</i>")},
		"content/a.html":       {Data: []byte("<Note />")},
	}
	eng, err := brick.New(brick.Options{FS: fsys, Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Render(context.Background(), "content/a.html", nil)
	if err != nil || string(got) != "<i>first</i>" {
		t.Fatalf("got %q %v", got, err)
	}
}

// A data format replaces nothing it was not asked to: the parser sees the
// file's bytes, and its failure names the file.
func TestDataFormatErrorNamesFile(t *testing.T) {
	fsys := fstest.MapFS{
		"data/bad.kv":    {Data: []byte("not a pair\n")},
		"content/a.html": {Data: []byte("---\nuse:\n  d: ~/data/bad.kv\n---\n{{ .Data.d }}")},
	}
	eng, err := brick.New(brick.Options{FS: fsys, DataFormats: map[string]brick.DataFormat{".kv": parseKV}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Render(context.Background(), "content/a.html", nil)
	if err == nil || !strings.Contains(err.Error(), "data/bad.kv") || !strings.Contains(err.Error(), "want `name = value`") {
		t.Fatalf("err = %v", err)
	}
}

// An extension nobody registered is refused by use: with the list of what
// is accepted, so a data file is never silently ignored.
func TestUnregisteredExtensionRefused(t *testing.T) {
	fsys := fstest.MapFS{
		"data/c.xyz":     {Data: []byte("a = \"b\"\n")},
		"content/a.html": {Data: []byte("---\nuse:\n  d: ~/data/c.xyz\n---\n{{ .Data.d.a }}")},
	}
	eng, err := brick.New(brick.Options{FS: fsys, DataFormats: map[string]brick.DataFormat{".kv": parseKV}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Render(context.Background(), "content/a.html", nil)
	const want = `use: d: ".xyz" is not a component (.html), data (.json .yaml .toml, plus registered .kv) or script (.star) file`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// tag refuses an element that could run script, so data can pick a heading
// level but never a <script>.
func TestTagRefusesScript(t *testing.T) {
	fsys := fstest.MapFS{"content/a.html": {Data: []byte(`{{ tag "script" }}`)}}
	eng, err := brick.New(brick.Options{FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Render(context.Background(), "content/a.html", nil)
	if err == nil || !strings.Contains(err.Error(), `tag: "script" is not an element tag can write`) {
		t.Fatalf("err = %v", err)
	}
}
