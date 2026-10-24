// Package sanitizebluemonday is the default policy for brick's `sanitize`
// built-in: bluemonday's user-generated-content allowlist.
//
//	eng, err := brick.New(brick.Options{Sanitizer: sanitizebluemonday.New()})
//
// It keeps safe formatting — paragraphs, emphasis, lists, links, images —
// and removes scripts, event handlers, style, iframes and javascript: URLs.
// Links gain rel="nofollow noreferrer" (noreferrer implies noopener).
package sanitizebluemonday

import "github.com/microcosm-cc/bluemonday"

// New returns a sanitizer for Options.Sanitizer. The policy is built once
// and is safe for concurrent use.
func New() func(string) string {
	p := bluemonday.UGCPolicy()
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)
	return p.Sanitize
}
