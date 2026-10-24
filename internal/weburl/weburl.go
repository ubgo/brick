// Package weburl holds the one shape test for "an absolute http(s) URL with a
// host", shared by the root package (redirect targets, form Origin), the site
// package (sitemap base URL) and validate (the URL rule). It is a leaf with
// no brick imports so every one of them can use it without an import cycle,
// and it is internal so it adds no public API.
package weburl

import "net/url"

// The URL schemes brick treats as a web address. Everything else
// (javascript:, data:, mailto:) is refused wherever a URL decides where a
// visitor goes.
const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"
)

// Is reports whether u is an absolute http(s) URL with a host. It is the
// shape test only; which hosts are allowed is each caller's decision.
func Is(u *url.URL) bool {
	return (u.Scheme == SchemeHTTP || u.Scheme == SchemeHTTPS) && u.Host != ""
}
