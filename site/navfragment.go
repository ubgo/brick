package site

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"regexp"

	"github.com/ubgo/brick"
)

// Fragment navigation. A served
// page is rendered with brick.WithPageMarks, so two comments bracket the
// page inside its layout. Everything outside them, the layout as this
// visitor sees it, is named by a fingerprint the page carries in a meta
// tag and the Brick-Layout header. nav.js sends the fingerprint of the
// page it shows as Brick-Nav; when the next page's layout renders to the
// same bytes the answer is only the page (Brick-Fragment: 1), and nav.js
// splices it between its own marks. A mismatch, or anything unusual, is
// the full page, exactly as before.
const (
	// headerNav is the request header carrying the shown layout's
	// fingerprint.
	headerNav = "Brick-Nav"
	// headerLayout is the response header naming the answered page's
	// layout fingerprint.
	headerLayout = "Brick-Layout"
	// headerFragment marks an answer that holds only the page.
	headerFragment = "Brick-Fragment"
	// layoutMetaName is the <meta name> the fingerprint is written under,
	// for the first page a tab loads (a document load cannot read its own
	// response headers).
	layoutMetaName = "brick-layout"
	// fingerprintBytes is how much of the SHA-256 the fingerprint keeps: 16
	// bytes make a collision between two layouts of one site negligible,
	// and the header stays short.
	fingerprintBytes = 16
)

// Per-page parts of the document outside the marks. They differ on every
// page, yet nav.js already updates the title and description itself and
// merges a page's assets, so they are left out of the fingerprint and
// sent with the fragment instead.
var (
	titleRE  = regexp.MustCompile(`(?is)<title[\s>].*?</title>`)
	descRE   = regexp.MustCompile(`(?is)<meta\s[^>]*\bname\s*=\s*["']?description\b[^>]*>`)
	assetsRE = regexp.MustCompile(`(?is)<brick-assets[\s>].*?</brick-assets>`)
	headRE   = regexp.MustCompile(`(?i)</head\s*>`)
	// currentLinkRE finds a brick:current link; currentAttrRE the two
	// attributes nav.js sets on it after every navigation (markCurrent).
	// They are the client's, so a sidebar that marks the page it is on
	// server-side (for readers without JavaScript) still counts as one
	// layout across its pages.
	currentLinkRE = regexp.MustCompile(`(?is)<a\s[^>]*\bbrick:current[^>]*>`)
	currentAttrRE = regexp.MustCompile(`(?i)\s+(?:aria-current="page"|data-current(?:="[^"]*")?)`)
)

// pageRegion finds the one pair of marks in a page: start is where the
// start mark begins, end where the end mark ends. ok is false without
// exactly one of each in order (no layout, or a layout that renders its
// children twice), and such a page is never cut.
func pageRegion(b []byte) (start, end int, ok bool) {
	start = bytes.Index(b, []byte(brick.PageStartMark))
	e := bytes.Index(b, []byte(brick.PageEndMark))
	if start < 0 || e < start || bytes.Count(b, []byte(brick.PageStartMark)) != 1 || bytes.Count(b, []byte(brick.PageEndMark)) != 1 {
		return 0, 0, false
	}
	return start, e + len(brick.PageEndMark), true
}

// layoutPrint is the fingerprint of everything outside the page region,
// without the title, the description and the asset blocks. Two pages with
// the same fingerprint differ only inside their regions, so splicing one's
// region into the other shows exactly the full page.
func layoutPrint(b []byte, start, end int) string {
	head, rest := splitHead(append(append([]byte(nil), b[:start]...), b[end:]...))
	head = descRE.ReplaceAll(titleRE.ReplaceAll(head, nil), nil)
	outside := currentLinkRE.ReplaceAllFunc(append(head, rest...), func(tag []byte) []byte {
		return currentAttrRE.ReplaceAll(tag, nil)
	})
	sum := sha256.Sum256(assetsRE.ReplaceAll(outside, nil))
	return hex.EncodeToString(sum[:fingerprintBytes])
}

// fragmentDoc is the answer to a navigation whose layout matches: a small
// document with the page's title, description and asset blocks, and the
// region (without its marks) as the body. The asset blocks end up in the
// parsed body, where nav.js reads and removes them before splicing.
func fragmentDoc(b []byte, start, end int) []byte {
	outside := append(append([]byte(nil), b[:start]...), b[end:]...)
	head, _ := splitHead(outside)
	var out bytes.Buffer
	out.WriteString("<!doctype html><html><head>")
	if t := titleRE.Find(head); t != nil {
		out.Write(t)
	}
	if d := descRE.Find(head); d != nil {
		out.Write(d)
	}
	out.WriteString("</head><body>")
	for _, a := range assetsRE.FindAll(outside, -1) {
		out.Write(a)
	}
	out.Write(b[start+len(brick.PageStartMark) : end-len(brick.PageEndMark)])
	out.WriteString("</body></html>")
	return out.Bytes()
}

// splitHead cuts a document at its </head>: the title and description
// are looked for in the head only, never in an inline SVG's <title> in
// the body. Without a </head> the head is empty.
func splitHead(b []byte) (head, rest []byte) {
	loc := headRE.FindIndex(b)
	if loc == nil {
		return nil, b
	}
	return b[:loc[0]:loc[0]], b[loc[0]:]
}

// stripMarks removes the marks from a page that will never be cut (it
// does not load nav.js), so its bytes are what they were before marks
// existed.
func stripMarks(b []byte) []byte {
	b = bytes.Replace(b, []byte(brick.PageStartMark), nil, 1)
	return bytes.Replace(b, []byte(brick.PageEndMark), nil, 1)
}

// injectLayoutMeta writes the fingerprint into the page's <head>; a page
// without a </head> gets none, and nav.js then never asks for a fragment.
func injectLayoutMeta(b []byte, fp string) []byte {
	loc := headRE.FindIndex(b)
	if loc == nil {
		return b
	}
	meta := `<meta name="` + layoutMetaName + `" content="` + fp + `">`
	out := make([]byte, 0, len(b)+len(meta))
	out = append(append(append(out, b[:loc[0]]...), meta...), b[loc[0]:]...)
	return out
}
