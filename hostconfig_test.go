package brick

import (
	"reflect"
	"testing"
	"testing/fstest"
)

// brick.yaml's host: section is for programs that are not the host (brick
// check, brick lsp): it parses, and Apply leaves a real host's Options
// exactly as they would be without it.
func TestConfigHostIgnoredByApply(t *testing.T) {
	t.Parallel()
	load := func(src string) Config {
		t.Helper()
		c, err := LoadConfig(fstest.MapFS{"brick.yaml": {Data: []byte(src)}}, "brick.yaml")
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	base := "roots: [components]\nnavigation: true\n"
	with := load(base + "host:\n  globals: [db, now]\n  uploads: true\n")
	if with.Host == nil || !reflect.DeepEqual(with.Host.Globals, []string{"db", "now"}) || !with.Host.Uploads {
		t.Fatalf("host: %+v", with.Host)
	}
	var a, b Options
	if err := with.Apply(&a); err != nil {
		t.Fatal(err)
	}
	if err := load(base).Apply(&b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("host: changed Apply:\n%+v\n%+v", a, b)
	}
	if _, err := LoadConfig(fstest.MapFS{"brick.yaml": {Data: []byte("host:\n  globls: [db]\n")}}, "brick.yaml"); err == nil {
		t.Fatal("a misspelled host: key must be an error")
	}
}
