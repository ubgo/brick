// Package bricktest helps test a brick theme (A10): an in-memory project
// and golden-file assertions. Deterministic output (E6) is what makes golden
// tests possible.
//
//	func TestCard(t *testing.T) {
//	    eng := bricktest.Engine(t, brick.Options{}, bricktest.Theme(
//	        "components/Card.html", `<div>{{ children }}</div>`,
//	    ))
//	    out, err := eng.RenderComponent(ctx, "Card", nil, "hi")
//	    bricktest.Golden(t, "card", out)
//	}
//
// Run `go test ./... -update` to rewrite the golden files.
package bricktest

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// update is the conventional golden-file flag. It is registered by the test
// binary that imports this package and read only by Golden.
var update = flag.Bool("update", false, "rewrite bricktest golden files")

// goldenDir is where Golden keeps its files, relative to the test's package.
const goldenDir = "testdata"

// goldenExt is the golden file extension.
const goldenExt = ".golden"

// Theme builds an in-memory project from path, content pairs.
func Theme(pairs ...string) fstest.MapFS {
	if len(pairs)%2 != 0 {
		panic("bricktest.Theme: pairs must be path, content")
	}
	fsys := fstest.MapFS{}
	for i := 0; i < len(pairs); i += 2 {
		fsys[pairs[i]] = &fstest.MapFile{Data: []byte(pairs[i+1])}
	}
	return fsys
}

// Engine returns an engine over fsys, failing the test when New fails.
func Engine(t testing.TB, opts brick.Options, fsys fstest.MapFS) *brick.Engine {
	t.Helper()
	opts.FS = fsys
	e, err := brick.New(opts)
	if err != nil {
		t.Fatalf("brick.New: %v", err)
	}
	return e
}

// Golden compares got with testdata/<name>.golden, rewriting it with
// -update. A missing golden file fails with a hint rather than passing.
func Golden(t testing.TB, name string, got []byte) {
	t.Helper()
	p := filepath.Join(goldenDir, name+goldenExt)
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("bricktest: %v", err)
		}
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatalf("bricktest: %v", err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("bricktest: %s: %v (run with -update to create it)", p, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("bricktest: output differs from %s (run with -update to accept)\n got: %s\nwant: %s", p, got, want)
	}
}
