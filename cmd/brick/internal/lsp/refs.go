package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/internal/frontmatter"
	"github.com/ubgo/brick/internal/lex"
)

// References and rename (textDocument/references, textDocument/rename,
// textDocument/prepareRename). Both read every brick file of the site with
// the engine's own scanner (internal/lex), never a text search and never an
// HTML parser: a `<Card>` inside a `{{ "…" }}` string, a template comment,
// an HTML comment or a raw body is text to the engine, so it is not a
// reference here either, and a rename never touches it. A tag resolves the
// way the engine resolves it: the file's `use:` first, then the site's
// components by name or by the `x-` hyphen fold.

// RawTest reports whether a component tag's body is raw (captured
// verbatim, so no tag inside it is a call).
type RawTest func(name string, attrs []lex.Attr) bool

// siteSkipDirs are folders a site walk never enters: vendored libraries
// and installed packages are someone else's files, so a rename must not
// edit them, and their calls are not this site's references.
var siteSkipDirs = map[string]bool{"brick_modules": true, "node_modules": true}

// The engine tags whose bodies are captured verbatim (the engine's isRaw
// in compile.go; internal/format keeps the same list).
const (
	tagScript    = "brick:script"
	tagClient    = "brick:client"
	tagStyle     = "brick:style"
	tagProp      = "brick:prop"
	attrRaw      = "raw"
	attrName     = "name"
	enginePrefix = "brick:"
	htmlExt      = ".html"
	useKey       = "use"
	propsKey     = "props:"
	rootPrefix   = "~/"
)

// engineRaw adds the engine's own raw tags to a site's raw components.
func engineRaw(user RawTest) RawTest {
	return func(name string, attrs []lex.Attr) bool {
		switch name {
		case tagScript, tagClient, tagStyle:
			return true
		case tagProp:
			return slices.ContainsFunc(attrs, func(a lex.Attr) bool { return a.Name == attrRaw })
		}
		if strings.HasPrefix(name, enginePrefix) {
			return false
		}
		return user != nil && user(name, attrs)
	}
}

// siteFile is one scanned brick file.
type siteFile struct {
	path string // absolute
	uri  string
	text string
	// body is where the scanned body starts in text; toks' offsets are
	// relative to it.
	body int
	toks []lex.Token
	// aliases is the file's use: map of tag name → component path
	// (relative to the site root), for the entries that name a .html file.
	aliases map[string]string
	// fm is the byte range of the frontmatter block, empty when none.
	fmStart, fmEnd int
}

// scanFile scans one file's text; ok is false for a file the engine could
// not read either (it has no references until it is fixed).
func scanFile(p, text string, raws func([]byte) RawTest) (*siteFile, bool) {
	src := []byte(text)
	doc, err := frontmatter.Parse(src)
	if err != nil {
		return nil, false
	}
	f := &siteFile{path: p, uri: pathToURI(p), text: text, body: len(src) - len(doc.Body), aliases: map[string]string{}}
	if doc.HasFrontmatter {
		f.fmStart, f.fmEnd = 0, f.body
	}
	if use, ok := doc.Params[useKey].(map[string]any); ok {
		for k, v := range use {
			if s, ok := v.(string); ok && strings.HasSuffix(s, htmlExt) {
				f.aliases[k] = path.Clean(strings.TrimPrefix(s, rootPrefix))
			}
		}
	}
	var user RawTest
	if raws != nil {
		user = raws(src)
	}
	toks, err := lex.Scan(doc.Body, lex.Options{IsRaw: engineRaw(user)})
	if err != nil {
		return nil, false
	}
	f.toks = toks
	return f, true
}

// resolve is the component path a written tag calls in this file, and
// whether it got there through a use: alias (the engine's lookupPath).
func (f *siteFile) resolve(list []brick.ComponentDoc, tag string) (p string, alias, ok bool) {
	if strings.HasPrefix(tag, enginePrefix) {
		return "", false, false
	}
	if p, ok := f.aliases[tag]; ok {
		return p, true, true
	}
	if strings.HasPrefix(tag, hyphenTagPrefix) {
		name := canonicalName(tag)
		for k, p := range f.aliases {
			if strings.EqualFold(k, name) {
				return p, true, true
			}
		}
	}
	c, ok := findComponent(list, tag)
	return c.Path, false, ok
}

// nameRange is the byte range of a tag token's name in the file.
func (f *siteFile) nameRange(t lex.Token) (int, int) {
	i := bytes.Index(t.Raw, []byte(t.Name))
	start := f.body + t.Pos.Offset + max(i, 0)
	return start, start + len(t.Name)
}

// attrRange is the byte range of an attribute's name in the file.
func (f *siteFile) attrRange(a lex.Attr) (int, int) {
	start := f.body + a.Pos.Offset
	return start, start + len(a.Name)
}

// valueRange is the byte range of a quoted attribute value, found after
// the attribute's name inside its tag.
func (f *siteFile) valueRange(t lex.Token, a lex.Attr) (int, int, bool) {
	rel := a.Pos.Offset - t.Pos.Offset + len(a.Name)
	if a.Quote == 0 || rel < 0 || rel > len(t.Raw) {
		return 0, 0, false
	}
	q := bytes.IndexByte(t.Raw[rel:], a.Quote)
	if q < 0 {
		return 0, 0, false
	}
	start := f.body + t.Pos.Offset + rel + q + 1
	return start, start + len(a.Value), true
}

// canonicalName turns a written tag into its component name, as the
// engine does: `x-post-list` → `PostList`.
func canonicalName(tag string) string {
	rest, ok := strings.CutPrefix(tag, hyphenTagPrefix)
	if !ok {
		return tag
	}
	var b strings.Builder
	for part := range strings.SplitSeq(rest, "-") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

// siteFiles scans every brick file of the site, open documents by their
// editor text (unsaved edits count), the rest from disk. Files the scanner
// cannot read are skipped and logged.
func (s *Server) siteFiles(root string) []*siteFile {
	var raws func([]byte) RawTest
	if s.opts.RawComponents != nil {
		raws = s.opts.RawComponents(root)
	}
	s.mu.Lock()
	open := map[string]string{}
	for uri, text := range s.docs {
		if p, err := uriToPath(uri); err == nil {
			open[p] = text
		}
	}
	s.mu.Unlock()
	var out []*siteFile
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || siteSkipDirs[d.Name()]) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, htmlExt) {
			return nil
		}
		text, ok := open[p]
		if !ok {
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			text = string(b)
		}
		if f, ok := scanFile(p, text, raws); ok {
			out = append(out, f)
		} else {
			fmt.Fprintf(s.opts.Log, "brick lsp: %s does not scan; its references are skipped\n", p)
		}
		return nil
	})
	return out
}

// target is what a references or rename request is about: a component,
// and optionally one of its props.
type target struct {
	comp brick.ComponentDoc
	prop string
	// start and end are the byte range of the name under the cursor.
	start, end int
	// word is the name as written there (`Card`, `x-card`, `title`).
	word string
	// onTag says the cursor is on a tag's name (rename needs one).
	onTag bool
}

// targetAt finds what the cursor is on: a component tag's name (open or
// close tag), a prop on a component tag, or, anywhere else in a
// component's own file, that component.
func (s *Server) targetAt(ctx context.Context, c cursor) (target, bool) {
	list := s.components(ctx, c.root)
	p, err := uriToPath(c.uri)
	if err != nil {
		return target{}, false
	}
	var raws func([]byte) RawTest
	if s.opts.RawComponents != nil {
		raws = s.opts.RawComponents(c.root)
	}
	if f, ok := scanFile(p, c.text, raws); ok {
		for _, t := range f.toks {
			if t.Kind != lex.Open && t.Kind != lex.Close {
				continue
			}
			cp, _, ok := f.resolve(list, t.Name)
			if !ok {
				continue
			}
			comp, ok := compByPath(list, cp)
			if !ok {
				continue
			}
			if start, end := f.nameRange(t); c.offset >= start && c.offset <= end {
				return target{comp: comp, start: start, end: end, word: t.Name, onTag: true}, true
			}
			for _, a := range t.Attrs {
				if start, end := f.attrRange(a); c.offset >= start && c.offset <= end && !strings.HasPrefix(a.Name, enginePrefix) {
					name := strings.TrimPrefix(a.Name, exprPrefix)
					return target{comp: comp, prop: name, start: end - len(name), end: end, word: name}, true
				}
			}
		}
		if tg, ok := blockPropTargetAt(f, list, c.offset); ok {
			return tg, true
		}
		if tg, ok := propTargetAt(f, c.root, list, c.offset); ok {
			return tg, true
		}
	}
	for _, comp := range list {
		if filepath.Join(c.root, filepath.FromSlash(comp.Path)) == p {
			return target{comp: comp, start: c.offset, end: c.offset}, true
		}
	}
	return target{}, false
}

// compByPath is the component whose file is p.
func compByPath(list []brick.ComponentDoc, p string) (brick.ComponentDoc, bool) {
	for _, c := range list {
		if c.Path == p {
			return c, true
		}
	}
	return brick.ComponentDoc{}, false
}

// tagRef is one tag naming a component.
type tagRef struct {
	file       *siteFile
	start, end int
	// alias says the tag reached the component through the file's use:
	// (its name is the file's own, so a rename leaves it alone).
	alias  bool
	hyphen bool
}

// componentRefs lists every open and close tag that calls comp.
func componentRefs(files []*siteFile, list []brick.ComponentDoc, comp brick.ComponentDoc) []tagRef {
	var out []tagRef
	for _, f := range files {
		for _, t := range f.toks {
			if t.Kind != lex.Open && t.Kind != lex.Close {
				continue
			}
			if p, alias, ok := f.resolve(list, t.Name); ok && p == comp.Path {
				start, end := f.nameRange(t)
				out = append(out, tagRef{file: f, start: start, end: end, alias: alias, hyphen: strings.HasPrefix(t.Name, hyphenTagPrefix)})
			}
		}
	}
	return out
}

// propDeclaration finds `  name:` directly under the component file's
// block `props:` key.
func propDeclaration(f *siteFile, prop string) (int, int, bool) {
	fm := f.text[f.fmStart:f.fmEnd]
	inProps, level, off := false, -1, f.fmStart
	for line := range strings.SplitSeq(fm, "\n") {
		lineStart := off
		off += len(line) + 1
		trimmed := strings.TrimRight(line, "\r \t")
		indent := len(trimmed) - len(strings.TrimLeft(trimmed, " "))
		switch {
		case trimmed == "" || strings.HasPrefix(strings.TrimSpace(trimmed), "#"):
			continue
		case indent == 0:
			inProps, level = trimmed == propsKey, -1
		case inProps:
			if level < 0 {
				level = indent
			}
			if indent == level && strings.HasPrefix(trimmed[indent:], prop+":") {
				return lineStart + indent, lineStart + indent + len(prop), true
			}
		}
	}
	return 0, 0, false
}

// location is an LSP Location.
func location(uri, text string, start, end int) map[string]any {
	return map[string]any{"uri": uri, "range": rangeOf(text, start, end)}
}

// references answers textDocument/references.
func (s *Server) references(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		position
		Context struct {
			IncludeDeclaration bool `json:"includeDeclaration"`
		} `json:"context"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, badParams(err)
	}
	return s.withPosition(ctx, params, func(ctx context.Context, c cursor) any {
		tg, ok := s.targetAt(ctx, c)
		if !ok {
			return nil
		}
		list := s.components(ctx, c.root)
		files := s.siteFiles(c.root)
		out := []map[string]any{}
		compFile := filepath.Join(c.root, filepath.FromSlash(tg.comp.Path))
		if tg.prop != "" {
			pl := planProp(c.root, files, list, tg.comp, tg.prop, "")
			refs := pl.sites()
			if !p.Context.IncludeDeclaration {
				refs = refs[len(pl.decls):]
			}
			for _, r := range refs {
				out = append(out, location(r.file.uri, r.file.text, r.start, r.end))
			}
			return out
		}
		if p.Context.IncludeDeclaration {
			if _, err := os.Stat(compFile); err == nil {
				out = append(out, location(pathToURI(compFile), "", 0, 0))
			}
		}
		for _, r := range componentRefs(files, list, tg.comp) {
			out = append(out, location(r.file.uri, r.file.text, r.start, r.end))
		}
		return out
	})
}

// renameRefusal is a rename brick will not carry out, with the reason the
// editor shows.
func renameRefusal(format string, a ...any) *rpcError {
	return &rpcError{Code: codeRequestFailed, Message: "brick: " + fmt.Sprintf(format, a...)}
}

// renamable checks that a component's file can be renamed: it is a file
// of this site (not the embedded kit, not a vendored library) whose name
// is its tag.
func (s *Server) renamable(c cursor, tg target) *rpcError {
	if !tg.onTag {
		return renameRefusal("put the cursor on a component tag's name to rename it")
	}
	p := filepath.Join(c.root, filepath.FromSlash(tg.comp.Path))
	if _, err := os.Stat(p); err != nil {
		return renameRefusal("<%s> is built into brick (the kit); it has no file in this site to rename", tg.comp.Name)
	}
	if slices.Contains(strings.Split(tg.comp.Path, "/"), "brick_modules") {
		return renameRefusal("<%s> comes from a library in brick_modules; rename it in the library", tg.comp.Name)
	}
	if strings.TrimSuffix(path.Base(tg.comp.Path), htmlExt) != tg.comp.Name {
		return renameRefusal("<%s>'s tag is not its file name (%s); rename the file by hand", tg.comp.Name, tg.comp.Path)
	}
	return nil
}

// isComponentIdent is the engine's rule for a component file name: an
// uppercase ASCII letter, then letters and digits.
func isComponentIdent(name string) bool {
	if name == "" || !isUpper(name[0]) {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !isUpper(c) && !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// prepareRename answers textDocument/prepareRename: the range of the tag
// name and its text, or the reason it cannot be renamed.
func (s *Server) prepareRename(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var rerr *rpcError
	res, perr := s.withPosition(ctx, params, func(ctx context.Context, c cursor) any {
		tg, ok := s.targetAt(ctx, c)
		if ok && tg.prop != "" {
			if rerr = propRenamable(c, tg); rerr != nil {
				return nil
			}
			return map[string]any{"range": rangeOf(c.text, tg.start, tg.end), "placeholder": tg.word}
		}
		if !ok || !tg.onTag {
			return nil
		}
		if rerr = s.renamable(c, tg); rerr != nil {
			return nil
		}
		return map[string]any{"range": rangeOf(c.text, tg.start, tg.end), "placeholder": tg.word}
	})
	if perr != nil {
		return nil, perr
	}
	if rerr != nil {
		return nil, rerr
	}
	return res, nil
}

// rename answers textDocument/rename for a component: every tag that
// calls it by name (open and close, `<Card>` and `<x-card>` each keeping
// its form), every use: path naming its file, and the file itself (with
// its locale variants, `Card.fr.html`) renamed by RenameFile operations.
// A tag reached through a use: alias keeps the alias, its file's own name.
// The file rename is the point, so a client that cannot apply one is
// refused rather than handed tag edits that leave the site broken.
func (s *Server) rename(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		position
		NewName string `json:"newName"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, badParams(err)
	}
	var rerr *rpcError
	res, perr := s.withPosition(ctx, params, func(ctx context.Context, c cursor) any {
		var edit any
		edit, rerr = s.renameAt(ctx, c, strings.TrimSpace(p.NewName))
		return edit
	})
	if perr != nil {
		return nil, perr
	}
	if rerr != nil {
		return nil, rerr
	}
	return res, nil
}

// renameAt builds the WorkspaceEdit for one rename.
func (s *Server) renameAt(ctx context.Context, c cursor, newName string) (any, *rpcError) {
	tg, ok := s.targetAt(ctx, c)
	if ok && tg.prop != "" {
		return s.renameProp(ctx, c, tg, newName)
	}
	if !ok || !tg.onTag {
		return nil, renameRefusal("put the cursor on a component tag's name or a prop to rename it")
	}
	if rerr := s.renamable(c, tg); rerr != nil {
		return nil, rerr
	}
	if !s.renameFiles {
		return nil, renameRefusal("renaming <%s> renames its file, and this editor does not accept file renames in an edit (workspace.workspaceEdit.resourceOperations has no \"rename\")", tg.comp.Name)
	}
	newName = canonicalName(newName)
	if !isComponentIdent(newName) {
		return nil, renameRefusal("%q is not a component name: start with an uppercase letter and use only letters and digits", newName)
	}
	if newName == tg.comp.Name {
		return map[string]any{"documentChanges": []any{}}, nil
	}
	list := s.components(ctx, c.root)
	for _, other := range list {
		if other.Path != tg.comp.Path && (other.Name == newName || foldHyphen(other.Name) == foldHyphen(newName)) {
			return nil, renameRefusal("<%s> already exists (%s)", other.Name, other.Path)
		}
	}
	oldPath := tg.comp.Path
	newPath := path.Join(path.Dir(oldPath), newName+htmlExt)
	if _, err := os.Stat(filepath.Join(c.root, filepath.FromSlash(newPath))); err == nil {
		return nil, renameRefusal("%s already exists", newPath)
	}

	files := s.siteFiles(c.root)
	edits := map[string][]map[string]any{}
	add := func(f *siteFile, start, end int, text string) {
		edits[f.uri] = append(edits[f.uri], map[string]any{"range": rangeOf(f.text, start, end), "newText": text})
	}
	for _, r := range componentRefs(files, list, tg.comp) {
		if r.alias {
			continue
		}
		name := newName
		if r.hyphen {
			name = HyphenName(newName)
		}
		add(r.file, r.start, r.end, name)
	}
	oldUse, newUse := rootPrefix+oldPath, rootPrefix+newPath
	for _, f := range files {
		if !slices.Contains(mapValues(f.aliases), oldPath) {
			continue
		}
		fm := f.text[f.fmStart:f.fmEnd]
		for i := 0; ; {
			j := strings.Index(fm[i:], oldUse)
			if j < 0 {
				break
			}
			add(f, f.fmStart+i+j, f.fmStart+i+j+len(oldUse), newUse)
			i += j + len(oldUse)
		}
	}

	changes := []any{}
	for _, uri := range sortedKeys(edits) {
		changes = append(changes, map[string]any{"textDocument": map[string]any{"uri": uri, "version": nil}, "edits": edits[uri]})
	}
	// The file renames come last: the text edits above name the old URIs.
	oldAbs := filepath.Join(c.root, filepath.FromSlash(oldPath))
	dir, oldStem := filepath.Dir(oldAbs), tg.comp.Name
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, htmlExt) {
			continue
		}
		stem := strings.TrimSuffix(n, htmlExt)
		if stem != oldStem && !strings.HasPrefix(stem, oldStem+".") {
			continue
		}
		to := filepath.Join(dir, newName+strings.TrimPrefix(stem, oldStem)+htmlExt)
		changes = append(changes, map[string]any{"kind": resourceRename, "oldUri": pathToURI(filepath.Join(dir, n)), "newUri": pathToURI(to),
			"options": map[string]any{"overwrite": false, "ignoreIfExists": false}})
	}
	return map[string]any{"documentChanges": changes}, nil
}

// mapValues is a map's values.
func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
