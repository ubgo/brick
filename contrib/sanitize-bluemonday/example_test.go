package sanitizebluemonday_test

import (
	"context"
	"fmt"
	"testing/fstest"

	"github.com/ubgo/brick"
	sanitizebluemonday "github.com/ubgo/brick/contrib/sanitize-bluemonday"
)

// bioTheme renders a user's bio, untrusted HTML from a form or a database,
// through the sanitize built-in.
var bioTheme = fstest.MapFS{
	"components/Bio.html": {Data: []byte("---\nprops:\n  bio: {type: string}\n---\n<div class=\"bio\">{{ sanitize .Props.bio }}</div>\n")},
}

// The sanitizer is the policy behind {{ sanitize }}: formatting and links
// survive, scripts, event handlers and javascript: URLs do not, and links
// gain rel="nofollow noreferrer".
func Example() {
	eng, err := brick.New(brick.Options{FS: bioTheme, Sanitizer: sanitizebluemonday.New()})
	if err != nil {
		fmt.Println(err)
		return
	}
	bio := `<p onclick="steal()">Hi, I <em>write</em> <a href="https://example.com">here</a>.</p><script>steal()</script><a href="javascript:steal()">x</a>`
	html, err := eng.RenderComponent(context.Background(), "Bio", map[string]any{"bio": bio}, "")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(html))
	// Output:
	// <div class="bio"><p>Hi, I <em>write</em> <a href="https://example.com" rel="nofollow noreferrer">here</a>.</p>x</div>
}

// Without a sanitizer, sanitize is an error: brick has no unsafe default.
func Example_noSanitizer() {
	eng, err := brick.New(brick.Options{FS: bioTheme})
	if err != nil {
		fmt.Println(err)
		return
	}
	_, err = eng.RenderComponent(context.Background(), "Bio", map[string]any{"bio": "<b>hi</b>"}, "")
	fmt.Println(err)
	// Output:
	// components/Bio.html:5:21: <Bio>: at <sanitize .Props.bio>: error calling sanitize: sanitize needs Options.Sanitizer (for example contrib/sanitize-bluemonday); there is no unsafe default
}
