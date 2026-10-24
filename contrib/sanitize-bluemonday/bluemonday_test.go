package sanitizebluemonday

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

func TestSanitize(t *testing.T) {
	t.Parallel()
	e, err := brick.New(brick.Options{Sanitizer: New(), FS: fstest.MapFS{
		"content/a.html": {Data: []byte(`<div>{{ sanitize .Props.bio }}</div>`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	bio := `<p onclick="x()">Hi <b>there</b><script>alert(1)</script><a href="javascript:alert(1)">x</a><a href="https://ok.test">ok</a><iframe src="x"></iframe></p>`
	out, err := e.Render(context.Background(), "content/a.html", map[string]any{"bio": bio})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, bad := range []string{"onclick", "<script", "javascript:", "<iframe"} {
		if strings.Contains(s, bad) {
			t.Errorf("kept %q: %s", bad, s)
		}
	}
	for _, want := range []string{"<b>there</b>", `href="https://ok.test"`, "nofollow", "noreferrer"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q: %s", want, s)
		}
	}
}
