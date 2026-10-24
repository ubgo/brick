package brick

import "io"

// MarkdownHeadings is an optional upgrade of Markdown: a backend that can
// report the headings it rendered implements it, and the site layer type-
// asserts for it to build a page's table of contents. A backend without
// the capability implements only Markdown and is never forced to fake an
// outline; callers treat a failed assertion as "no headings known".
//
// ConvertHeadings must write exactly the bytes Convert writes for the same
// src, so switching a caller from one method to the other never changes
// output (A14). Heading (declared beside Page.Headings) gives each ID as
// the id attribute written on the element, unique within one document, or
// "" when the backend writes no ids (heading ids switched off), so a table
// of contents can always list headings but link only to those with an ID.
type MarkdownHeadings interface {
	Markdown
	ConvertHeadings(src []byte, w io.Writer) ([]Heading, error)
}
