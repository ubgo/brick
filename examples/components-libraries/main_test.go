package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/kit"
)

// TestOutput compares what the program prints with testdata/output.txt,
// the text docs/guides/components/libraries.md shows.
func TestOutput(t *testing.T) {
	want, err := os.ReadFile("testdata/output.txt")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), &out, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}

// The vendored copy is what renders: the lock pins every file, and
// brick_modules/ is where the engine reads them from.
func TestVendorWritesLockAndModules(t *testing.T) {
	work := t.TempDir()
	if _, err := setup(context.Background(), work); err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(work + "/brick.lock")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"github.com/acme/ui/components/Tag.html@v1.2.0 sha256-", "github.com/acme/ui/brick-library.yaml@v1.2.0 sha256-"} {
		if !strings.Contains(string(lock), want) {
			t.Errorf("brick.lock lacks %q:\n%s", want, lock)
		}
	}
	if _, err := os.Stat(work + "/brick_modules/github.com/acme/ui@v1.2.0/components/Tag.html"); err != nil {
		t.Errorf("not vendored: %v", err)
	}
}

// only: hides every other component of the library, and a project
// component beats a library component of the same name.
func TestOnlyAndProjectWins(t *testing.T) {
	work := t.TempDir()
	if _, err := setup(context.Background(), work); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work+"/components", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(work+"/components/Badge.html", []byte("<i>mine</i>"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng, err := newEngine(work)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	got, err := eng.Render(ctx, "content/probe.html", nil, brick.WithSource([]byte(`<Badge>x</Badge>`)))
	if err != nil || string(got) != "<i>mine</i>" {
		t.Errorf("project component: %q %v", got, err)
	}
	_, err = eng.Render(ctx, "content/probe.html", nil, brick.WithSource([]byte(`<Alert>x</Alert>`)))
	if err == nil || !strings.Contains(err.Error(), "unknown component <Alert>") {
		t.Errorf("a component outside only: should be unknown: %v", err)
	}
}

// A bare name in `libraries:` reaches only what the program offered.
func TestBareNameMustBeOffered(t *testing.T) {
	fsys := fstest.MapFS{"brick.yaml": {Data: []byte("libraries: [brick-kit]\n")}}
	cfg, err := brick.LoadConfig(fsys, "brick.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var opts brick.Options
	err = cfg.Apply(&opts)
	const want = `brick.yaml: libraries: "brick-kit" is not a ~/ folder, a remote library (host/owner/repo@version), or a library this program offers (none)`
	if err == nil || err.Error() != want {
		t.Errorf("Apply without the kit: %v", err)
	}
	if err := cfg.Apply(&opts, kit.Library()); err != nil || len(opts.Libraries) != 1 {
		t.Errorf("Apply with the kit: %v %+v", err, opts.Libraries)
	}
}

// The same options in Go: a Library value with a prefix and a subset, and
// the kit's Theme linked once for every page through the manifest's global.
func TestLibraryInGoAndKitTheme(t *testing.T) {
	lib := kit.Library()
	lib.Prefix = "K"
	lib.Only = []string{"Badge"}
	eng, err := brick.New(brick.Options{FS: fstest.MapFS{}, Libraries: []brick.Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Render(context.Background(), "content/p.html", nil, brick.WithSource([]byte(`{{ assets }}<KBadge>x</KBadge>`)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "/_brick/c/Theme.") || !strings.Contains(string(got), `class="bk-badge`) {
		t.Errorf("page:\n%s", got)
	}
}
