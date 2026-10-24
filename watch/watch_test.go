package watch

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestDiff(t *testing.T) {
	t.Parallel()
	old := fstest.MapFS{"a.txt": {Data: []byte("1")}, "b.txt": {Data: []byte("2")}, "c.txt": {Data: []byte("3")}}
	cur := fstest.MapFS{"a.txt": {Data: []byte("1")}, "b.txt": {Data: []byte("changed")}, "d.txt": {Data: []byte("4")}}
	s1, _ := scan(old, ".", nil)
	s2, _ := scan(cur, ".", nil)
	got := diff(s1, s2)
	var parts []string
	for _, c := range got {
		parts = append(parts, c.Op.String()+":"+c.Path)
	}
	if strings.Join(parts, " ") != "modified:b.txt deleted:c.txt created:d.txt" {
		t.Fatalf("diff: %v", parts)
	}
	if len(diff(s1, s1)) != 0 {
		t.Fatal("identical snapshots differ")
	}
}

func TestSkip(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"a.txt": {Data: []byte("1")}, ".git/x": {Data: []byte("1")}, "b.tmp": {Data: []byte("1")}}
	s, err := scan(fsys, ".", func(p string, d fs.DirEntry) bool {
		return strings.HasPrefix(d.Name(), ".") || strings.HasSuffix(p, ".tmp")
	})
	if err != nil || len(s) != 1 {
		t.Fatalf("scan: %v %v", s, err)
	}
}

// A real directory: a write is reported once, rewriting identical bytes is
// not reported, and deleting is.
func TestWatchReal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.html", "one")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, _, err := Watch(ctx, os.DirFS(dir), ".", Options{Interval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	next := func() []Change {
		select {
		case b := <-changes:
			return b
		case <-time.After(2 * time.Second):
			t.Fatal("no change reported")
			return nil
		}
	}
	write("a.html", "two")
	if b := next(); len(b) != 1 || b[0] != (Change{Path: "a.html", Op: Modified}) {
		t.Fatalf("modify: %v", b)
	}
	write("a.html", "two") // same bytes
	write("b.html", "new")
	if b := next(); len(b) != 1 || b[0] != (Change{Path: "b.html", Op: Created}) {
		t.Fatalf("create (and no false modify): %v", b)
	}
	if err := os.Remove(filepath.Join(dir, "a.html")); err != nil {
		t.Fatal(err)
	}
	if b := next(); len(b) != 1 || b[0].Op != Deleted {
		t.Fatalf("delete: %v", b)
	}
	cancel()
	for range changes {
	}
}

func TestWatchMissingRoot(t *testing.T) {
	t.Parallel()
	if _, _, err := Watch(context.Background(), fstest.MapFS{}, "nope", Options{}); err == nil {
		t.Fatal("a missing root was accepted")
	}
}
