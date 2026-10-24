package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// brick new writes a starter file that teaches the conventions by example
//: a component with the frontmatter its flags ask for, or a
// page. It never overwrites, because a starter that replaced real work
// would be the most expensive file it ever wrote.

// Where brick new writes, relative to the site directory.
const (
	componentsDir = "components"
	contentDir    = "content"
	newFileExt    = ".html"
	newFileMode   = 0o644
	newDirMode    = 0o755
)

// The kinds brick new writes.
const (
	kindSite      = "site"
	kindComponent = "component"
	kindPage      = "page"
)

const newUsage = `brick new — write a starter project, component or page

Usage:
  brick new site <dir>
  brick new component <Name> [-state] [-client] [-island] [-lazy] [-dir site]
  brick new page <path> [-dir site]

Examples:
  brick new site blog                      a working project in blog/: brick serve blog
  brick new component Card                 components/Card.html: props and children
  brick new component Counter -state       a stateful component with one action
  brick new component Chart -client        a component with client code (brick.mount)
  brick new component Stock -island        rendered per request inside a cached page
  brick new component Chart -lazy          fetched after the page loads, when it scrolls into view
  brick new page blog/hello                content/blog/hello.html
  brick new page blog/[slug]               a dynamic route: one file answers /blog/<slug>/
  brick new page docs/[...path]            a catch-all route: /docs/a/b/c/

It refuses to overwrite an existing file, and new site refuses a folder
that is not empty.
`

var (
	// componentNameRE is a component file name: a capital letter, then
	// letters and digits (<Card>, which <x-card> also resolves to).
	componentNameRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	// pageSegmentRE is one segment of a page path.
	pageSegmentRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	// routeSegmentRE is a dynamic route segment: [slug] for
	// one segment, [...path] for one or more, [[...path]] for any number.
	// The groups are the opening brackets, the dots and the name.
	routeSegmentRE = regexp.MustCompile(`^(\[\[?)(\.\.\.)?([a-z][a-zA-Z0-9_]*)\]\]?$`)
)

// newOptions are what brick new was asked to write.
type newOptions struct {
	kind, name, dir             string
	state, client, island, lazy bool
}

// lazyDefault is the trigger a -lazy starter declares: when the island
// scrolls into view, the case lazy islands exist for.
const lazyDefault = "visible"

// newCmd is brick new: parse, render the starter, write it once.
func newCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, newUsage)
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	if args[0] == kindSite {
		return newSite(args[1:], stdout, stderr)
	}
	o := newOptions{kind: args[0]}
	fl := flag.NewFlagSet("brick new "+o.kind, flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.BoolVar(&o.state, "state", false, "component: declare state and one action (a stateful component)")
	fl.BoolVar(&o.client, "client", false, "component: add client code run by brick.mount")
	fl.BoolVar(&o.island, "island", false, "component: render per request (brick: {island: true})")
	fl.BoolVar(&o.lazy, "lazy", false, "component: a lazy island, fetched after the page loads (brick: {lazy: visible})")
	fl.StringVar(&o.dir, "dir", ".", "the site directory")
	// The name comes before the flags in the usage; accept it in either place.
	rest := args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		o.name, rest = rest[0], rest[1:]
	}
	if err := fl.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if o.name == "" && fl.NArg() == 1 {
		o.name = fl.Arg(0)
	} else if fl.NArg() > 0 || o.name == "" {
		fmt.Fprintf(stderr, "brick new: one name\n\n%s", newUsage)
		return exitUsage
	}
	rel, body, err := starter(o)
	if err != nil {
		fmt.Fprintln(stderr, "brick new:", err)
		return exitUsage
	}
	path := filepath.Join(o.dir, filepath.FromSlash(rel))
	if err := writeNew(path, body); err != nil {
		fmt.Fprintln(stderr, "brick new:", err)
		return exitFail
	}
	fmt.Fprintln(stdout, "wrote", rel)
	return exitOK
}

// writeNew creates path and its directories, refusing an existing file.
func writeNew(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), newDirMode); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, newFileMode)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists; brick new never overwrites", path)
	}
	if err != nil {
		return err
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// starter returns the file's path in the site and its content.
func starter(o newOptions) (string, string, error) {
	switch o.kind {
	case kindComponent:
		if !componentNameRE.MatchString(o.name) {
			return "", "", fmt.Errorf("%q is not a component name: start with a capital letter and use letters and digits, such as Card", o.name)
		}
		return componentsDir + "/" + o.name + newFileExt, componentStarter(o), nil
	case kindPage:
		if o.state || o.client || o.island || o.lazy {
			return "", "", errors.New("-state, -client, -island and -lazy are for components; a page uses components")
		}
		p := strings.Trim(strings.TrimSuffix(o.name, newFileExt), "/")
		segs := strings.Split(p, "/")
		var route *routeParam
		for i, seg := range segs {
			if strings.ContainsAny(seg, "[]") {
				m := routeSegmentRE.FindStringSubmatch(seg)
				if m == nil || (m[1] == "[[" && (m[2] == "" || !strings.HasSuffix(seg, "]]"))) || (m[1] == "[" && strings.HasSuffix(seg, "]]")) {
					return "", "", fmt.Errorf("%q: a dynamic segment is [name], [...name] or [[...name]], with a lowercase name", o.name)
				}
				if route != nil {
					return "", "", fmt.Errorf("%q: brick new writes one dynamic segment; add more by renaming folders", o.name)
				}
				if m[2] != "" && i != len(segs)-1 {
					return "", "", fmt.Errorf("%q: a catch-all segment ([...%s]) must be the last", o.name, m[3])
				}
				route = &routeParam{name: m[3], catchAll: m[2] != ""}
				continue
			}
			if !pageSegmentRE.MatchString(seg) {
				return "", "", fmt.Errorf("%q: each path segment is lowercase letters, digits, ., - and _", o.name)
			}
		}
		if route != nil {
			return contentDir + "/" + p + newFileExt, routeStarter(p, *route), nil
		}
		return contentDir + "/" + p + newFileExt, pageStarter(p), nil
	}
	return "", "", fmt.Errorf("unknown kind %q (site, component or page)", o.kind)
}

// cssClass is the starter's class name for a component: Card -> card,
// DataTable -> data-table.
func cssClass(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// componentStarter writes a component with the frontmatter its flags ask
// for. A stateful component takes props only (children of a stateful
// component are), so only a stateless one renders children.
func componentStarter(o newOptions) string {
	var fm, doc, blocks, inner strings.Builder
	cls := cssClass(o.name)
	fm.WriteString("props:\n  title: {type: string, default: \"" + o.name + "\"}\n")
	if o.state {
		fm.WriteString("state:\n  count: 0\n")
	}
	switch {
	case o.lazy:
		// Lazy implies island, so island: true would only repeat it.
		fm.WriteString("brick:\n  lazy: " + lazyDefault + "\n")
	case o.island && !o.state:
		fm.WriteString("brick:\n  island: true\n")
	}
	fmt.Fprintf(&doc, "%s: one sentence on what it is for.\n\n", o.name)
	switch {
	case o.state:
		fmt.Fprintf(&doc, "  <%s title=\"Clicks\" />\n\nStateful: its state lives on the server and each click runs the\nincrement action, which returns the new state; the component re-renders\nin place.", o.name)
		blocks.WriteString("<brick:script>\ndef increment(state, props):\n    return dict(state, count=state[\"count\"] + 1)\n</brick:script>\n")
		inner.WriteString("  <button type=\"button\" brick:click=\"increment\">Clicked {{ .State.count }} times</button>\n")
	case o.lazy:
		fmt.Fprintf(&doc, "  <brick:boundary>\n    <%s title=\"Revenue\" />\n    <brick:prop name=\"loading\"><p>Loading…</p></brick:prop>\n  </brick:boundary>\n\nA lazy island: the page is sent without it, and the browser fetches it\nwhen it scrolls into view, so a slow query never holds up the page. The\nboundary's loading block is the placeholder until then. A call can pick\nanother moment: brick:lazy (right after load) or brick:lazy=\"idle\".", o.name)
		inner.WriteString("  <p>Rendered after the page, when it scrolls into view.</p>\n")
	case o.island:
		fmt.Fprintf(&doc, "  <%s title=\"Today\" />\n\nAn island: rendered for each request (it may read .Ctx), while the page\naround it is cached.", o.name)
		inner.WriteString("  <p>Rendered per request.</p>\n")
	default:
		fmt.Fprintf(&doc, "  <%s title=\"Hello\"><p>Any markup.</p></%s>", o.name, o.name)
		inner.WriteString("  {{ children }}\n")
	}
	if o.client {
		fmt.Fprintf(&blocks, "<brick:client>\nbrick.mount(%q, (el, props) => {\n  // Runs once per element; return a cleanup for when it is removed.\n  el.dataset.ready = \"\";\n  return () => { delete el.dataset.ready; };\n});\n</brick:client>\n", o.name)
		doc.WriteString("\n\nIts <brick:client> code runs once per element (brick.mount).")
	}
	return "---\n" + fm.String() + "---\n{{/*\n" + doc.String() + "\n*/}}\n" + blocks.String() +
		"<section class=\"" + cls + "\">\n  <h2>{{ .Props.title }}</h2>\n" + inner.String() + "</section>\n"
}

// routeParam is the dynamic segment of a route starter.
type routeParam struct {
	name     string
	catchAll bool
}

// routeStarter writes a dynamic route page: it shows the
// segment's value and says how to load the record and list the pages.
func routeStarter(p string, r routeParam) string {
	// The URL as a reader writes it, /blog/<slug>/, and a title from the
	// folder the pages live in (blog), or the parameter at the top level.
	segs := strings.Split(p, "/")
	title := r.name
	for i, seg := range segs {
		if strings.HasPrefix(seg, "[") {
			if i > 0 {
				title = segs[i-1]
			}
			segs[i] = "<" + r.name + ">"
		}
	}
	url := "/" + strings.Join(segs, "/") + "/"
	title = strings.ToUpper(title[:1]) + title[1:]
	what := ".Page.Route." + r.name + " is the segment from the URL"
	if r.catchAll {
		what = ".Page.Route." + r.name + " is the rest of the path, joined with /, and\n.Page.RouteParts the same as a list"
	}
	return "---\ntitle: " + title + "\n---\n{{/*\nA dynamic route: this file answers every " + url + " URL;\n" + what + ".\n\nTo load the record and give brick build the list of pages (serve then\nanswers 404 for any other value without running the page), add:\n\n  use: {posts: ~/scripts/posts.star}\n  load: posts.one\n  brick: {paths: posts.slugs}\n\nwhere one(input) reads input[\"page\"][\"route\"][\"" + r.name + "\"] and calls\nbrick.not_found() when there is no such record, and slugs() returns a\nlist of {\"" + r.name + "\": …} maps.\n*/}}\n<h1>{{ .Page.Route." + r.name + " }}</h1>\n"
}

// pageStarter writes a page: a title in frontmatter and a heading.
func pageStarter(p string) string {
	base := p[strings.LastIndex(p, "/")+1:]
	title := strings.ReplaceAll(strings.ReplaceAll(base, "-", " "), "_", " ")
	if title != "" {
		title = strings.ToUpper(title[:1]) + title[1:]
	}
	return "---\ntitle: " + title + "\n---\n<h1>" + title + "</h1>\n<p>Write the page here; components are tags, such as <code>&lt;Card&gt;</code>.</p>\n"
}

// brick new site writes the smallest project that shows every part working
// together: brick.yaml turning on the kit, site.yaml with the title, a
// layout that links the components' assets, and a home page calling kit
// components. It passes brick check as written, and brick serve renders it.

// siteTitleMark is replaced in a starter file by the site's title, quoted
// for YAML.
const siteTitleMark = "$SITE_TITLE"

// siteStarterFiles are the project brick new site writes, by path.
var siteStarterFiles = []struct{ path, body string }{
	{"brick.yaml", `# brick-kit is built into the brick CLI: every kit component (<Button>,
# <Card>, <Dialog>, …) is usable by name, with no copy and no vendoring.
libraries: [brick-kit]
`},
	{"site.yaml", `# Read-only data every page and layout sees as .Site.
title: $SITE_TITLE
`},
	{"layouts/default.html", `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{ .Title }} · {{ .Site.title }}</title>
</head>
<body>
{{/* The CSS and JavaScript of the components this page uses. */}}
{{ assets }}
<main>
  {{ children }}
</main>
</body>
</html>
`},
	{"content/index.html", `---
title: Home
---
<Card title="It works" description="This page is content/index.html, wrapped in layouts/default.html.">
  <p>Components are tags: this card is the kit's Card. Add your own with brick new component Name.</p>
  <brick:prop name="actions">
    <Button href="https://github.com/ubgo/brick">Read the guide</Button>
  </brick:prop>
</Card>
`},
}

// newSite is brick new site <dir>.
func newSite(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("brick new site", flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.Usage = func() { fmt.Fprint(stderr, newUsage) }
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if fl.NArg() != 1 {
		fmt.Fprintf(stderr, "brick new site: one directory\n\n%s", newUsage)
		return exitUsage
	}
	dir := fl.Arg(0)
	// A folder that already holds anything is someone's work: refuse it
	// whole rather than write a project around it.
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(stderr, "brick new site:", err)
		return exitFail
	}
	if len(entries) > 0 {
		fmt.Fprintf(stderr, "brick new site: %s is not empty; brick new site writes only into a new or empty folder\n", dir)
		return exitFail
	}
	title := siteTitle(dir)
	for _, f := range siteStarterFiles {
		if err := writeNew(filepath.Join(dir, filepath.FromSlash(f.path)), strings.ReplaceAll(f.body, siteTitleMark, strconv.Quote(title))); err != nil {
			fmt.Fprintln(stderr, "brick new site:", err)
			return exitFail
		}
		fmt.Fprintln(stdout, "wrote", filepath.ToSlash(filepath.Join(dir, f.path)))
	}
	fmt.Fprintf(stdout, "\nNext:\n  brick serve %[1]s    http://localhost:8080\n  brick check %[1]s\n", dir)
	return exitOK
}

// siteTitle is a site's starting title, from its folder: my-blog -> My blog.
func siteTitle(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	t := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(filepath.Base(abs)))
	if t == "" || t == "." || t == string(filepath.Separator) {
		return "My site"
	}
	return strings.ToUpper(t[:1]) + t[1:]
}
