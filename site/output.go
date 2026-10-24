package site

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/ubgo/brick"
)

// Output pages (v14): a page file named with a second extension before
// .html, content/feed.xml.html or content/robots.txt.html, renders a
// non-HTML file at that exact name (/feed.xml, /robots.txt) instead of a
// directory-style page. The page is still an html/template page, so only
// formats html/template escapes correctly are allowed: its escaping writes
// &lt; &amp; &#34; and &#39;, which XML reads as the same characters and a
// text file shows harmlessly. A format whose escaping that would corrupt is
// refused by name.

// outputTypes maps an output page's extension to the Content-Type serve
// sends. The keys are the whole allow-list.
var outputTypes = map[string]string{
	"xml":  "application/xml; charset=utf-8",
	"rss":  "application/rss+xml; charset=utf-8",
	"atom": "application/atom+xml; charset=utf-8",
	"txt":  "text/plain; charset=utf-8",
}

// refusedOutputs are second extensions that name a format html/template
// would escape wrongly, each with the reason the error gives. Any other
// second extension (v1.2.html) is not an output page and keeps meaning
// what it always did.
var refusedOutputs = map[string]string{
	"json": "html/template escapes text for HTML, so a quote becomes &#34; and the JSON breaks; serve JSON from a Go handler, or pass data to the page through a loader",
	"js":   "html/template escapes text for HTML, not JavaScript; put scripts in static/ or a component's <brick:client>",
	"mjs":  "html/template escapes text for HTML, not JavaScript; put scripts in static/ or a component's <brick:client>",
	"css":  "html/template escapes text for HTML, not CSS; put stylesheets in static/ or a component's <brick:style>",
	"csv":  "html/template escapes text for HTML, so a quote becomes &#34; and the CSV breaks; serve CSV from a Go handler",
}

// CodeOutput is an output page the site refuses: a format html/template
// cannot escape (data.json.html), or a [param] inside its file name.
const CodeOutput brick.Code = "output"

// outputExt is the second extension of a page file once its locale is
// dropped ("xml" for content/feed.xml.html and content/feed.xml.fr.html),
// or "" when it has none.
func outputExt(file string) string {
	stem, _ := variantOf(file)
	ext := path.Ext(stem)
	if ext == "" || strings.Contains(ext, "/") {
		return ""
	}
	return strings.ToLower(ext[1:])
}

// isOutputExt reports whether a second extension is an output format or a
// refused one: either way it is never read as a locale tag (xml and txt are
// shaped like language codes).
func isOutputExt(ext string) bool {
	ext = strings.ToLower(ext)
	_, ok := outputTypes[ext]
	_, refused := refusedOutputs[ext]
	return ok || refused
}

// outputOf classifies a page file: the output format of an output page,
// "" for an ordinary page, or a *brick.Error for a refused format, a
// markdown output page (markdown renders HTML) or a [param] in the file
// name.
//
// A dynamic output page (v22) takes its parameters from its directories,
// content/tags/[tag]/feed.xml.html → /tags/go/feed.xml: the file name is
// the file served, so it stays one literal segment. [tag].xml.html is
// refused rather than read, because a route value is one whole segment
// (validSegment) and splicing it into a name would let the value choose
// the file's extension.
func outputOf(file string, dynamic bool) (string, error) {
	ext := outputExt(file)
	if ext == "" {
		return "", nil
	}
	if why, ok := refusedOutputs[ext]; ok {
		return "", &brick.Error{File: file, Code: CodeOutput, Err: fmt.Errorf(".%s output pages are not supported: %s", ext, why)}
	}
	if _, ok := outputTypes[ext]; !ok {
		return "", nil
	}
	if path.Ext(file) != extHTML {
		return "", &brick.Error{File: file, Code: CodeOutput, Err: errors.New("an output page is a .html file (feed.xml.html); markdown always renders HTML")}
	}
	if dynamic && strings.ContainsAny(path.Base(file), "[]") {
		return "", &brick.Error{File: file, Code: CodeOutput, Err: errors.New("an output page's file name is the file it serves; put the [param] in a directory: tags/[tag]/feed.xml.html")}
	}
	return ext, nil
}

// outputContentType is the Content-Type of a route: HTML for a page, the
// format's type for an output page.
func outputContentType(r Route) string {
	if ct, ok := outputTypes[r.Output]; ok {
		return ct
	}
	return htmlContentType
}

// liftProlog takes the XML prolog off the start of an output page's body:
// every leading processing instruction (<?xml version="1.0"?>,
// <?xml-stylesheet …?>). html/template would escape its "<" as text, so it
// is written verbatim in front of the rendered bytes instead (withProlog).
// rest is the body with each instruction blanked to its newlines, so error
// line numbers still match the file.
func liftProlog(body []byte) (prolog string, rest []byte) {
	i := 0
	var pis []string
	for {
		j := i
		for j < len(body) && (body[j] == ' ' || body[j] == '\t' || body[j] == '\r' || body[j] == '\n') {
			j++
		}
		if !bytes.HasPrefix(body[j:], []byte("<?")) {
			break
		}
		end := bytes.Index(body[j:], []byte("?>"))
		if end < 0 {
			break
		}
		pis = append(pis, string(body[j:j+end+2]))
		i = j + end + 2
	}
	if len(pis) == 0 {
		return "", body
	}
	out := make([]byte, 0, len(body))
	out = append(out, bytes.Repeat([]byte("\n"), bytes.Count(body[:i], []byte("\n")))...)
	out = append(out, body[i:]...)
	return strings.Join(pis, "\n"), out
}

// withProlog puts a lifted prolog back in front of an output page's
// rendered bytes, on its own line.
func withProlog(prolog string, b []byte) []byte {
	if prolog == "" {
		return b
	}
	out := append([]byte(prolog), '\n')
	return append(out, bytes.TrimLeft(b, " \t\r\n")...)
}
