package brick

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ubgo/brick/internal/frontmatter"
	"github.com/ubgo/brick/internal/lex"
	"github.com/ubgo/brick/internal/tree"
	"github.com/ubgo/brick/runtime"
)

// Asset URL layout under Options.AssetBase (A8). Pinned: cached pages
// reference these URLs.
const (
	assetDir = "c"
	extJS    = ".js"
	extCSS   = ".css"
	// scopeAttr marks a scoped-style component's root element.
	scopeAttr = "data-brick-s"
	// fnClientAttrs is the runtime function writing a client component's
	// root attributes; injected by the engine, never written by authors.
	fnClientAttrs = "__clientAttrs"
	// mountAttr defers a client component's mounting; it is
	// written on the root element as data-brick-mount.
	mountAttr = "brick:mount"
	// runtimeName and navName are the runtime files under assetDir.
	runtimeName = "runtime"
	navName     = "nav"
	// fnIDAttr writes `data-brick="<id>"` on a component root when
	// Options.EmitIDs is on.
	fnIDAttr = "__idAttr"
)

// assetFile is one served asset.
type assetFile struct {
	name string // path under AssetBase, e.g. c/Accordion.a3f9c1.js
	data []byte
}

// registerAssets builds a component's module and stylesheet (content-hashed
// URLs, SRI integrity) and indexes them for Assets().
func (e *Engine) registerAssets(c *compiled, name string) error {
	client, deps, err := e.linkClient(c.client, useAliases(c.meta), []string{c.path})
	if err != nil {
		return err
	}
	c.client = client
	c.deps = append(c.deps, deps...)
	slices.Sort(c.deps)
	c.deps = slices.Compact(c.deps)
	var files []asset
	add := func(ext string, data []byte, css bool) {
		sum := sha256.Sum256(data)
		fname := path.Join(assetDir, name+"."+hex.EncodeToString(sum[:])[:hashLen]+ext)
		sri := sha512.Sum384(data)
		files = append(files, asset{URL: e.opts.AssetBase + fname, Integrity: "sha384-" + base64.StdEncoding.EncodeToString(sri[:]), CSS: css})
		e.mu.Lock()
		if e.assetFiles == nil {
			e.assetFiles = map[string]assetFile{}
		}
		e.assetFiles[fname] = assetFile{name: fname, data: data}
		e.mu.Unlock()
	}
	if len(c.client) > 0 {
		add(extJS, c.client, false)
	}
	if len(c.styles) > 0 {
		var css bytes.Buffer
		for _, s := range c.styles {
			b, err := e.styleCSS(c, s)
			if err != nil {
				return err
			}
			css.Write(b)
		}
		add(extCSS, css.Bytes(), true)
	}
	e.mu.Lock()
	if e.assetsByFile == nil {
		e.assetsByFile = map[string][]asset{}
	}
	e.assetsByFile[c.path] = files
	e.mu.Unlock()
	return nil
}

// clientImportRE matches an ES module specifier naming a component:
// `from "brick:Modal"` or `import "brick:Modal"`.
var clientImportRE = regexp.MustCompile(`((?:\bfrom|\bimport)\s*)(["'])brick:([A-Za-z][\w-]*)(["'])`)

// linkClient rewrites `brick:Name` import specifiers in client code to the
// named component's content-hashed module URL (resolved like a tag: use:,
// then the roots), registering that module so it is served even when the
// component is not on the page. stack detects import cycles, which
// content-hashed names cannot express.
func (e *Engine) linkClient(src []byte, aliases map[string]string, stack []string) ([]byte, []string, error) {
	var deps []string
	var firstErr error
	// link resolves one import match; sub is [whole, prefix, quote, name,
	// quote]. On failure it records the first error and keeps the text.
	link := func(sub [][]byte) []byte {
		name := string(sub[3])
		p, ok := e.lookupPath(name, aliases)
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("<brick:client> imports brick:%s, but no component of that name exists", name)
			}
			return sub[0]
		}
		if slices.Contains(stack, p) {
			if firstErr == nil {
				firstErr = fmt.Errorf("client module import cycle: %s", strings.Join(append(stack, p), " → "))
			}
			return sub[0]
		}
		url, more, err := e.clientModule(p, append(slices.Clone(stack), p))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return sub[0]
		}
		deps = append(append(deps, p), more...)
		// A relative specifier: every component module sits in the same
		// asset directory, so ./X.<hash>.js resolves beside the importing
		// module wherever the assets are served from — the root, a site's
		// BasePath (site.Mount) or a CDN AssetBase. An absolute URL would
		// be right only at the root, and no HTML rewrite can reach it
		// inside a module body.
		return []byte(string(sub[1]) + string(sub[2]) + "./" + path.Base(url) + string(sub[4]))
	}
	// Imports are found in a copy with the comments blanked out, so an
	// import quoted in a doc comment is neither linked nor reported as a
	// cycle. Offsets match src, whose text — comments included — is kept.
	var out []byte
	last := 0
	for _, loc := range clientImportRE.FindAllSubmatchIndex(jsCommentMask(src), -1) {
		sub := make([][]byte, len(loc)/2)
		for i := range sub {
			if loc[2*i] >= 0 {
				sub[i] = src[loc[2*i]:loc[2*i+1]]
			}
		}
		out = append(out, src[last:loc[0]]...)
		out = append(out, link(sub)...)
		last = loc[1]
	}
	out = append(out, src[last:]...)
	return out, deps, firstErr
}

// jsCommentMask returns src with the text of every JavaScript comment
// replaced by spaces (newlines kept), so a pattern search over it matches
// code only and its offsets still index src. Strings and template literals
// are skipped so a "//" inside one is not taken for a comment; regular
// expression literals are not parsed, which is enough for import scanning.
func jsCommentMask(src []byte) []byte {
	out := slices.Clone(src)
	blank := func(from, to int) {
		for i := from; i < to && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case c == '"' || c == '\'' || c == '`':
			for i++; i < len(src) && src[i] != c; i++ {
				if src[i] == '\\' {
					i++
				}
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			end := bytes.IndexByte(src[i:], '\n')
			if end < 0 {
				end = len(src) - i
			}
			blank(i, i+end)
			i += end
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := bytes.Index(src[i+2:], []byte("*/"))
			stop := len(src)
			if end >= 0 {
				stop = i + 2 + end + 2
			}
			blank(i, stop)
			i = stop - 1
		}
	}
	return out
}

// clientModule registers a component's client module for import and
// returns its URL. It reads only the component's <brick:client> block, so
// importing never compiles (or renders) the component.
func (e *Engine) clientModule(p string, stack []string) (string, []string, error) {
	src, err := e.readFile(p)
	if err != nil {
		return "", nil, err
	}
	doc, err := frontmatter.Parse(src)
	if err != nil {
		return "", nil, &Error{File: p, Code: CodeFrontmatter, Err: err}
	}
	m, err := parseMeta(doc, p, e.resolveFrom, e.opts.PropTypes, e.opts.DataFormats)
	if err != nil {
		return "", nil, &Error{File: p, Code: CodeFrontmatter, Err: err}
	}
	toks, err := lex.Scan(doc.Body, lex.Options{IsRaw: func(n string, _ []lex.Attr) bool { return n == tree.TagClient }})
	if err != nil {
		return "", nil, &Error{File: p, Code: CodeSyntax, Err: err}
	}
	var client []byte
	for i, t := range toks {
		if t.Kind == lex.Open && t.Name == tree.TagClient && i+1 < len(toks) && toks[i+1].RawBody {
			client = append(client, toks[i+1].Raw...)
		}
	}
	if len(client) == 0 {
		return "", nil, fmt.Errorf("%s has no <brick:client> to import", displayPath(p))
	}
	linked, deps, err := e.linkClient(client, useAliases(m), stack)
	if err != nil {
		return "", nil, err
	}
	stem, _, _ := splitStem(strings.TrimSuffix(path.Base(p), htmlExt))
	return e.embedAsset(stem, linked).URL, deps, nil
}

// useAliases is a file's use: component imports, by key.
func useAliases(m *meta) map[string]string {
	out := map[string]string{}
	for _, u := range m.use {
		if u.Kind == useComponent {
			out[u.Key] = u.Path
		}
	}
	return out
}

// mountModes are the values brick:mount accepts.
var mountModes = []string{"visible", "idle"}

// assetsFor returns a component file's assets.
func (e *Engine) assetsFor(p string) []asset {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.assetsByFile[p]
}

// InheritAssets makes every asset file old serves servable by e too. A
// host that rebuilds its engine on a file change (the site layer in Dev)
// keeps serving cached pages rendered by the old engine, and those pages
// link the old engine's files; without this, a page whose own files did
// not change would link a stylesheet or module that now 404s. Names are
// content-hashed, so an inherited file can never shadow a new one.
func (e *Engine) InheritAssets(old *Engine) {
	if old == nil || old == e {
		return
	}
	old.mu.Lock()
	files := make(map[string]assetFile, len(old.assetFiles))
	for k, v := range old.assetFiles {
		files[k] = v
	}
	old.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.assetFiles == nil {
		e.assetFiles = map[string]assetFile{}
	}
	for k, v := range files {
		if _, ok := e.assetFiles[k]; !ok {
			e.assetFiles[k] = v
		}
	}
}

// runtimeAssets registers the browser runtime once and returns what a page
// links: the mount runtime, then the navigation runtime when
// Options.Navigation is on. They go first in an asset block, so
// window.brick exists before any component module runs.
func (e *Engine) runtimeAssets() []asset {
	e.runtimeOnce.Do(func() {
		rt := e.embedAsset(runtimeName, runtime.JS)
		// The runtime reads its config once when it loads, so the meta
		// goes just before it.
		rt.Config = e.opts.Runtime.meta()
		e.runtime = []asset{rt}
		if e.opts.Navigation {
			e.runtime = append(e.runtime, e.embedAsset(navName, runtime.Nav))
		}
	})
	return e.runtime
}

// embedAsset registers an embedded script as a content-hashed asset.
func (e *Engine) embedAsset(stem string, data []byte) asset {
	sum := sha256.Sum256(data)
	name := path.Join(assetDir, stem+"."+hex.EncodeToString(sum[:])[:hashLen]+extJS)
	sri := sha512.Sum384(data)
	e.mu.Lock()
	if e.assetFiles == nil {
		e.assetFiles = map[string]assetFile{}
	}
	e.assetFiles[name] = assetFile{name: name, data: data}
	e.mu.Unlock()
	return asset{URL: e.opts.AssetBase + name, Integrity: "sha384-" + base64.StdEncoding.EncodeToString(sri[:])}
}

// assetFS serves registered assets. It lists only what has been compiled in
// this snapshot, so call CompileAll first to populate it fully.
type assetFS struct{ e *Engine }

func (a assetFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	a.e.mu.Lock()
	f, ok := a.e.assetFiles[name]
	a.e.mu.Unlock()
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{name: path.Base(name), r: bytes.NewReader(f.data), size: int64(len(f.data))}, nil
}

// ReadDir lists the registered assets, so fs.WalkDir can copy them all (a
// static export). The tree is `c/` (component code) and `img/` (images).
func (a assetFS) ReadDir(name string) ([]fs.DirEntry, error) {
	a.e.mu.Lock()
	defer a.e.mu.Unlock()
	if name == "." {
		var out []fs.DirEntry
		seen := map[string]bool{}
		for _, p := range sortedKeys(a.e.assetFiles) {
			if d := path.Dir(p); !seen[d] {
				seen[d] = true
				out = append(out, dirEntry{name: d, dir: true})
			}
		}
		return out, nil
	}
	var out []fs.DirEntry
	for _, p := range sortedKeys(a.e.assetFiles) {
		if path.Dir(p) == name {
			out = append(out, dirEntry{name: path.Base(p), size: int64(len(a.e.assetFiles[p].data))})
		}
	}
	if out == nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return out, nil
}

// Stat reports the directories and each registered asset.
func (a assetFS) Stat(name string) (fs.FileInfo, error) {
	if name == "." || name == assetDir || name == imageDir {
		return memInfo{name: path.Base(name), dir: true}, nil
	}
	a.e.mu.Lock()
	f, ok := a.e.assetFiles[name]
	a.e.mu.Unlock()
	if !ok {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return memInfo{name: path.Base(name), size: int64(len(f.data))}, nil
}

// dirEntry is an entry of assetFS.
type dirEntry struct {
	name string
	dir  bool
	size int64
}

func (d dirEntry) Name() string { return d.name }
func (d dirEntry) IsDir() bool  { return d.dir }
func (d dirEntry) Type() fs.FileMode {
	if d.dir {
		return fs.ModeDir
	}
	return 0
}
func (d dirEntry) Info() (fs.FileInfo, error) {
	return memInfo{name: d.name, size: d.size, dir: d.dir}, nil
}

// memFile is an in-memory fs.File.
type memFile struct {
	name string
	r    *bytes.Reader
	size int64
}

func (f *memFile) Stat() (fs.FileInfo, error) { return memInfo{name: f.name, size: f.size}, nil }
func (f *memFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *memFile) Close() error               { return nil }
func (f *memFile) Seek(off int64, whence int) (int64, error) {
	return f.r.Seek(off, whence)
}

type memInfo struct {
	name string
	size int64
	dir  bool
}

func (i memInfo) Name() string { return i.name }
func (i memInfo) Size() int64  { return i.size }
func (i memInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return i.dir }
func (i memInfo) Sys() any           { return nil }

var _ io.Seeker = (*memFile)(nil)

// voidElements never have a close tag.
var voidElements = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true, "input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}

// tagRE finds start and end tags in plain markup.
var tagRE = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9-]*)(?:\s[^<>]*?)?(/?)>`)

// errRootCount is returned when a component needing one root has several.
var errRootCount = errors.New("a component with <brick:style scoped>, brick: {css: scoped} or <brick:client> must have exactly one top-level element, so the engine has one place for its marker")

// markRoot injects attributes into the component's single root element: the
// scoped-style marker (static) and, for a client or stateful component, the
// runtime attributes. It counts top-level elements to enforce one root.
//
// The top level is scanned as one string in which every {{ }} action is
// blanked out, so a start tag that contains an action — <p {{ if .X }}hidden
// {{ end }}> — is still seen whole, and its end tag does not unbalance the
// count. A component tag at the top level counts as a root of its own.
func markRoot(nodes []tree.Node, attr string) ([]tree.Node, error) {
	return markRootSep(nodes, " "+attr)
}

// markRootTight injects attr with no space before it, for a function that
// writes its own leading space, so an empty result changes no byte.
func markRootTight(nodes []tree.Node, attr string) ([]tree.Node, error) {
	return markRootSep(nodes, attr)
}

func markRootSep(nodes []tree.Node, attr string) ([]tree.Node, error) {
	type span struct {
		node, start int // node index and its offset in the joined string
	}
	var joined bytes.Buffer
	var spans []span
	for i, n := range nodes {
		spans = append(spans, span{node: i, start: joined.Len()})
		switch n := n.(type) {
		case tree.Text:
			if n.RawBody {
				joined.Write(bytes.Repeat([]byte("_"), len(n.Raw)))
			} else {
				joined.Write(n.Raw)
			}
		case tree.Action:
			joined.Write(bytes.Repeat([]byte("_"), len(n.Tok.Raw)))
		case *tree.Element:
			// A placeholder root that cannot carry the marker.
			joined.WriteString("<brick-component/>")
		}
	}
	src := joined.Bytes()
	depth, roots := 0, 0
	inject := -1 // offset in src where attr goes
	for _, m := range tagRE.FindAllSubmatchIndex(src, -1) {
		closing := m[3] > m[2]
		name := strings.ToLower(string(src[m[4]:m[5]]))
		selfClose := m[7] > m[6]
		if closing {
			depth--
			continue
		}
		if depth == 0 {
			roots++
			if roots == 1 && name != "brick-component" {
				inject = m[5]
			}
		}
		if !selfClose && !voidElements[name] {
			depth++
		}
	}
	if roots != 1 || inject < 0 {
		return nil, errRootCount
	}
	// Map the offset back to the text node that holds it.
	out := make([]tree.Node, len(nodes))
	copy(out, nodes)
	for i := len(spans) - 1; i >= 0; i-- {
		if spans[i].start <= inject {
			t, ok := out[spans[i].node].(tree.Text)
			if !ok {
				return nil, errRootCount
			}
			off := inject - spans[i].start
			raw := append(append(append([]byte(nil), t.Raw[:off]...), attr...), t.Raw[off:]...)
			out[spans[i].node] = tree.Text{Raw: raw, Pos: t.Pos}
			return out, nil
		}
	}
	return nil, errRootCount
}

// clientAttrs writes a client component's root attributes: its instance id,
// its type and its client: true props (D12). Only declared client props are
// serialized, by construction.
func (st *renderState) clientAttrs(s *Scope) (template.HTMLAttr, error) {
	props := map[string]any{}
	if pm, ok := s.Props.(map[string]any); ok {
		for name, d := range s.file.meta.props {
			if d.Client {
				if v, ok := pm[name]; ok {
					props[name] = v
				}
			}
		}
	}
	b, err := json.Marshal(props)
	if err != nil {
		return "", fmt.Errorf("client props of <%s> cannot be serialized: %w", s.inst.name, err)
	}
	id := s.ID
	mount := ""
	if s.inst.mount != "" {
		mount = fmt.Sprintf(` data-brick-mount=%q`, s.inst.mount)
	}
	// The attribute value is escaped here; html/template trusts HTMLAttr as a
	// complete, safe attribute sequence.
	// The type is the component file's name — what its client code mounts —
	// never the alias an importer gave it (use: KitTabs: …/Tabs.html).
	typ, _, _ := splitStem(strings.TrimSuffix(path.Base(s.file.path), htmlExt))
	if typ == "" {
		typ = s.inst.name
	}
	return template.HTMLAttr(fmt.Sprintf(`data-brick=%q data-brick-type=%q data-brick-props="%s"%s`, id, typ, template.HTMLEscapeString(string(b)), mount)), nil
}

// instanceID is the current component's id: a hash of its dynamic path
// (E5, C10). The hash function and length are pinned (A14).
func (st *renderState) instanceID() string {
	sum := sha256.Sum256([]byte(strings.Join(st.path, "/")))
	return "b" + hex.EncodeToString(sum[:])[:hashLen]
}
