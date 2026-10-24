package disk

import (
	"regexp"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

var stateRE = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([^"]+)"`)

func engineFor(t *testing.T, files map[string]string, sink brick.UploadSink) *brick.Engine {
	t.Helper()
	fs := fstest.MapFS{}
	for k, v := range files {
		fs[k] = &fstest.MapFile{Data: []byte(v)}
	}
	eng, err := brick.New(brick.Options{FS: fs, States: brick.NewMemoryStates(0, 0), Uploads: sink})
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func handleIn(t *testing.T, page string) (string, string) {
	t.Helper()
	m := stateRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no handle in %s", page)
	}
	return m[1], m[2]
}
