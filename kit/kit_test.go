package kit_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/kit"
)

// The embedded kit is a complete library: a page uses <Button> by name with
// no import, and Theme's tokens are linked through the manifest's global
// list although the page never writes <Theme />.
func TestLibrary(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"content/a.html": {Data: []byte(`<Button>Save</Button>{{ assets }}`)}}
	e, err := brick.New(brick.Options{FS: fsys, Libraries: []brick.Library{kit.Library()}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{`class="bk-btn`, `>Save`, `/_brick/c/Button.`, `/_brick/c/Theme.`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "<template") {
		t.Errorf("a global component's markup was rendered; only its assets should be linked:\n%s", got)
	}
}

// The package's name, the manifest's name and the library's name agree, so
// brick.yaml's `libraries: [brick-kit]` picks the embedded kit.
func TestName(t *testing.T) {
	t.Parallel()
	var opts brick.Options
	cfg := brick.Config{Libraries: []brick.LibrarySpec{{From: kit.Name}}}
	if err := cfg.Apply(&opts, kit.Library()); err != nil {
		t.Fatal(err)
	}
	if len(opts.Libraries) != 1 || opts.Libraries[0].Name != kit.Name {
		t.Fatalf("libraries: %+v", opts.Libraries)
	}
	b, err := fs.ReadFile(kit.FS(), brick.LibraryManifest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\nname: "+kit.Name+"\n") {
		t.Fatalf("brick-library.yaml does not name the library %q", kit.Name)
	}
}
