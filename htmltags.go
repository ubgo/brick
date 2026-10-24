package brick

import "strings"

// htmlTags are the HTML and SVG element names. A capitalized tag with one of
// these names that resolves to no component is emitted as HTML with a
// warning (S7); a registered component of that name still wins.
var htmlTags = map[string]bool{}

func init() {
	for t := range strings.FieldsSeq(`a abbr address area article aside audio b base bdi bdo blockquote body br button canvas caption cite code col colgroup data datalist dd del details dfn dialog div dl dt em embed fieldset figcaption figure footer form h1 h2 h3 h4 h5 h6 head header hgroup hr html i iframe img input ins kbd label legend li link main map mark menu meta meter nav noscript object ol optgroup option output p param picture pre progress q rp rt ruby s samp script search section select slot small source span strong style sub summary sup table tbody td template textarea tfoot th thead time title tr track u ul var video wbr
svg circle clippath defs ellipse g line lineargradient mask path pattern polygon polyline radialgradient rect stop symbol text tspan use foreignobject image marker filter`) {
		htmlTags[t] = true
	}
}

// knownHTMLTag reports whether a tag is an HTML or SVG element, ignoring case.
func knownHTMLTag(tag string) bool { return htmlTags[strings.ToLower(tag)] }

// writableTags are the elements the tag built-in may write. A
// computed element name cannot sit in template text, because html/template
// escapes a `<` that an action follows (it cannot know what context the
// tag opens), so tag writes the whole tag. html/template then sees opaque
// markup, not a tag, and cannot escape the attributes by context. That is
// safe only for elements with no URL, script or raw-text content, so the
// list leaves out a, img, form, button, iframe, script, style, textarea,
// svg and every other element whose attributes or body html/template would
// otherwise have to treat specially; tag refuses those, and its attributes
// are limited in the same way (tagAttrAllowed).
var writableTags = map[string]bool{}

func init() {
	for t := range strings.FieldsSeq(`h1 h2 h3 h4 h5 h6 p div span section article aside header footer main nav hgroup search address
ul ol li dl dt dd figure figcaption blockquote pre code kbd samp var
strong em b i u s small mark sub sup abbr cite dfn time q del ins
label legend fieldset details summary
table caption thead tbody tfoot tr th td`) {
		writableTags[t] = true
	}
}

// urlAttrs hold a URL (or, for style, CSS that can load one): html/template
// filters them only where it sees an attribute, which it cannot inside the
// markup tag writes, so tag refuses them.
var urlAttrs = map[string]bool{"href": true, "src": true, "srcset": true, "action": true, "formaction": true, "cite": true, "data": true, "poster": true, "background": true, "longdesc": true, "usemap": true, "ping": true, "manifest": true, "codebase": true, "xlink:href": true, "style": true}
