package brick

import (
	"context"
	"strings"
	"testing"
)

// An island's inner components ship their CSS and JS with the cached page:
// those it always renders, one it renders only inside {{ if }} (a dialog
// shown after an action), and one chosen from data at fill time.
func TestIslandLinksInnerComponentAssets(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Styled.html", "<brick:style>.s { color: red }</brick:style><b class=\"s\">s</b>",
		"components/Later.html", "<brick:style>.l { color: blue }</brick:style><i class=\"l\">l</i>",
		"components/Picked.html", "<brick:style>.p { color: green }</brick:style><u class=\"p\">p</u>",
		"components/Wrap.html", "<Styled />",
		"components/Isl.html", "---\nbrick:\n  island: true\n---\n<div><Wrap />{{ if .Props.later }}<Later />{{ end }}<brick:render :name=\"`Picked`\" /></div>",
		"content/a.html", "{{ assets }}<Isl />",
	)})
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	files := strings.Join(r.Files, " ")
	for _, f := range []string{"components/Wrap.html", "components/Styled.html", "components/Later.html"} {
		if !strings.Contains(files, f) {
			t.Errorf("the page does not depend on %s: %s", f, files)
		}
	}
	out, err := e.Fill(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/_brick/c/Styled.", "/_brick/c/Later.", "/_brick/c/Picked.", `<b class="s">s</b>`, `<u class="p">p</u>`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Count(string(out), "/_brick/c/Styled.") != 1 {
		t.Errorf("Styled linked more than once:\n%s", out)
	}
}
