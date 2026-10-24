package main

import (
	"strings"
	"testing"

	"go.starlark.net/starlark"

	sanitizebluemonday "github.com/ubgo/brick/contrib/sanitize-bluemonday"
)

// sanitize_html strips scripts, handlers and javascript: links with the
// same policy the template's sanitize uses.
func TestSanitizeHTMLCapability(t *testing.T) {
	fn := sanitizeHTML(sanitizebluemonday.New())
	v, err := starlark.Call(&starlark.Thread{}, fn, starlark.Tuple{starlark.String(`<p onclick="x()">hi <a href="javascript:alert(1)">l</a></p><script>alert(1)</script><img src=x onerror=alert(1)>`)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := string(v.(starlark.String))
	for _, bad := range []string{"<script", "onclick", "onerror", "javascript:"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q survived: %s", bad, out)
		}
	}
	if !strings.Contains(out, "<p>hi ") {
		t.Errorf("safe markup lost: %s", out)
	}
}
