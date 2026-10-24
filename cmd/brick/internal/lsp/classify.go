// Package lsp is brick's language server (`brick lsp`): diagnostics,
// completion, hover, go-to-definition, formatting, references and
// component rename for brick .html files over the Language Server
// Protocol.
//
// It is split so the editor-independent part is testable without a client:
// classify.go turns a cursor position into what the cursor is on (a
// frontmatter key, a component tag name, a prop name) using only the text;
// conn.go is the JSON-RPC transport; refs.go scans the whole site with
// the engine's lexer for references and rename; server.go answers requests from a
// Project, which the CLI implements with the real engine. The package never
// opens a site itself, so tests drive it with fixed component lists.
package lsp

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Kind is what the cursor is on.
type Kind int

// The places completion, hover and definition answer for. KindNone is
// everywhere else: page markup, text, a template action.
const (
	KindNone Kind = iota
	// KindTagName: inside a tag's name, `<Ca|` or `<x-ca|`.
	KindTagName
	// KindAttrName: in attribute-name position inside a tag, `<Card ti|`.
	KindAttrName
	// KindAttrValue: inside an attribute's quoted value.
	KindAttrValue
	// KindFrontmatterKey: a key position in the `---` block.
	KindFrontmatterKey
	// KindFrontmatterValue: after `key:` in the `---` block.
	KindFrontmatterValue
)

// Context is the classification of one cursor position.
type Context struct {
	Kind Kind
	// Tag is the tag the cursor is in, as written (`Card`, `x-card`), for
	// the tag and attribute kinds.
	Tag string
	// Attr is the attribute whose value holds the cursor (KindAttrValue).
	Attr string
	// Prefix is the part of the current word before the cursor (what
	// completion filters on); Word is the whole word under the cursor
	// (what hover and definition look up).
	Prefix string
	Word   string
	// WordStart is the byte offset where Word starts, so a completion can
	// replace it.
	WordStart int
	// Written lists the attribute names already in the tag, so completion
	// does not offer them twice.
	Written []string
	// Path is the frontmatter key path that holds the cursor: for
	// `brick: {isl|` it is ["brick"]; for the value in `brick: {island: |`
	// it is ["brick", "island"]. A flow or block list adds "[]".
	Path []string
}

// ListItem is the Path segment for a list's items (`[a, b]` or `- a`).
const ListItem = "[]"

// frontmatterFence opens and closes the frontmatter block.
const frontmatterFence = "---"

// Classify reports what the byte offset in text is on.
func Classify(text string, offset int) Context {
	offset = max(0, min(offset, len(text)))
	if start, end, ok := frontmatterBody(text); ok && offset >= start && offset <= end {
		ctx := classifyYAML(text[start:end], offset-start)
		ctx.WordStart += start
		return ctx
	}
	return classifyMarkup(text, offset)
}

// frontmatterBody returns the byte range of the lines between the opening
// and closing `---` fences, when the file starts with one.
func frontmatterBody(text string) (start, end int, ok bool) {
	first, rest, found := strings.Cut(text, "\n")
	if !found || strings.TrimRight(first, "\r") != frontmatterFence {
		return 0, 0, false
	}
	start = len(first) + 1
	pos := start
	for line := range strings.SplitSeq(rest, "\n") {
		if strings.TrimRight(line, "\r") == frontmatterFence {
			return start, pos, true
		}
		pos += len(line) + 1
	}
	// An unclosed block: everything after the fence is frontmatter while
	// it is being typed.
	return start, len(text), true
}

// isNameByte reports a byte that can be part of a tag or attribute name.
func isNameByte(c byte) bool {
	return c == '-' || c == '_' || c == ':' || c == '.' || c == '@' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// wordAt returns the name word around offset: its start, the part before
// offset, and the whole word.
func wordAt(text string, offset int, isWord func(byte) bool) (start int, prefix, word string) {
	start = offset
	for start > 0 && isWord(text[start-1]) {
		start--
	}
	end := offset
	for end < len(text) && isWord(text[end]) {
		end++
	}
	return start, text[start:offset], text[start:end]
}

// classifyMarkup finds the open tag holding offset, if any. A tag opens at
// the last `<` before the cursor and is closed by a `>` outside quotes and
// `{{ }}` actions; attribute values never hold angle brackets (the
// no-escape invariant), so the last `<` is the tag's own.
func classifyMarkup(text string, offset int) Context {
	lt := strings.LastIndexByte(text[:offset], '<')
	if lt < 0 {
		return Context{}
	}
	nameEnd := lt + 1
	for nameEnd < len(text) && isNameByte(text[nameEnd]) {
		nameEnd++
	}
	tag := text[lt+1 : nameEnd]
	if tag == "" || !isTagStart(tag[0]) {
		// `</x>`, `<!-- -->`, `<?`, or a bare `<` in text.
		return Context{}
	}
	if offset <= nameEnd {
		start, prefix, word := wordAt(text, offset, isNameByte)
		return Context{Kind: KindTagName, Tag: tag, Prefix: prefix, Word: word, WordStart: start}
	}
	// Walk the attributes up to the cursor.
	var written []string
	i := nameEnd
	for i < offset {
		c := text[i]
		switch {
		case c == '>':
			return Context{}
		case strings.HasPrefix(text[i:], "{{"):
			end := strings.Index(text[i+2:], "}}")
			if end < 0 || i+2+end+2 > offset {
				return Context{} // the cursor is inside a template action
			}
			i += 2 + end + 2
		case isNameByte(c):
			j := i
			for j < len(text) && isNameByte(text[j]) {
				j++
			}
			name := text[i:j]
			if j >= offset {
				start, prefix, word := wordAt(text, offset, isNameByte)
				return Context{Kind: KindAttrName, Tag: tag, Prefix: prefix, Word: word, WordStart: start, Written: written}
			}
			written = append(written, name)
			i = j
			// `name="…"`: skip the value, or report the cursor inside it.
			k := i
			for k < len(text) && k < offset && (text[k] == ' ' || text[k] == '\t') {
				k++
			}
			if k < len(text) && text[k] == '=' {
				k++
				for k < len(text) && k < offset && (text[k] == ' ' || text[k] == '\t') {
					k++
				}
				if k < len(text) && (text[k] == '"' || text[k] == '\'') {
					q := text[k]
					close := strings.IndexByte(text[k+1:], q)
					if close < 0 || k+1+close >= offset {
						return Context{Kind: KindAttrValue, Tag: tag, Attr: name, Written: written}
					}
					k += 1 + close + 1
				}
				i = k
			}
		default:
			i++
		}
	}
	return Context{Kind: KindAttrName, Tag: tag, WordStart: offset, Written: written}
}

// isTagStart reports a byte that can start an element name.
func isTagStart(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// isKeyByte reports a byte that can be part of a frontmatter key.
func isKeyByte(c byte) bool {
	return c == '-' || c == '_' || c == '.' || c == '~' || c == '/' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// classifyYAML classifies an offset inside the frontmatter body. It reads
// the block structure from indentation (each less-indented `key:` line
// above the cursor is a parent) and the flow structure (`{…}`, `[…]`) of
// the cursor's own line, which covers how brick files are written:
// `props:` blocks of `name: {type: …, default: …}` lines.
func classifyYAML(body string, offset int) Context {
	lineStart := strings.LastIndexByte(body[:offset], '\n') + 1
	lineEnd := strings.IndexByte(body[offset:], '\n')
	if lineEnd < 0 {
		lineEnd = len(body)
	} else {
		lineEnd += offset
	}
	line := strings.TrimRight(body[lineStart:lineEnd], "\r")
	col := min(offset-lineStart, len(line))
	indent, inList := lineIndent(line)
	if inList {
		indent -= len("- ") // the item's siblings sit at its dash, not inside it
	}
	path := blockPath(body[:lineStart], indent)
	if inList {
		path = append(path, ListItem)
	}
	fl := flowScan(line[:col])
	if fl.comment {
		return Context{}
	}
	path = append(path, fl.stack...)
	start, prefix, word := wordAt(line, col, isKeyByte)
	ctx := Context{Prefix: prefix, Word: word, WordStart: lineStart + start}
	if fl.afterColon {
		ctx.Kind = KindFrontmatterValue
		ctx.Path = append(path, fl.key)
		return ctx
	}
	if len(fl.stack) > 0 && fl.stack[len(fl.stack)-1] == ListItem {
		ctx.Kind = KindFrontmatterValue
		ctx.Path = path
		return ctx
	}
	ctx.Kind = KindFrontmatterKey
	ctx.Path = path
	return ctx
}

// lineIndent is a line's indentation, counting a leading `- ` as indent
// (its entries sit one level in), and whether it is a list item.
func lineIndent(line string) (int, bool) {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	if strings.HasPrefix(line[n:], "- ") || line[n:] == "-" {
		return n + 2, true
	}
	return n, false
}

// blockPath returns the keys of the block mappings and lists that hold a
// line with the given indent: walking up, every `key:` line indented less
// than the current level is a parent, and every `- ` item whose dash sits
// left of it holds it.
func blockPath(above string, indent int) []string {
	lines := strings.Split(above, "\n")
	var path []string
	level := indent
	for i := len(lines) - 1; i >= 0 && level > 0; i-- {
		l := strings.TrimRight(lines[i], "\r")
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		in, isItem := lineIndent(l)
		dash := in - len("- ")
		switch {
		case isItem && dash < level:
			if in < level {
				if key, ok := blockKey(l[in:]); ok {
					path = append([]string{key}, path...)
				}
			}
			path = append([]string{ListItem}, path...)
			level = dash
		case !isItem && in < level:
			if key, ok := blockKey(l[in:]); ok {
				path = append([]string{key}, path...)
			}
			level = in
		}
	}
	return path
}

// blockKey is the key of a `key: …` line.
func blockKey(rest string) (string, bool) {
	key, _, ok := strings.Cut(rest, ":")
	if !ok || key == "" || strings.ContainsAny(key, " {[\"'") {
		return "", false
	}
	return key, true
}

// flowState is what a single line says about the cursor at its end.
type flowState struct {
	// stack is the path through the flow mappings and lists open at the
	// cursor (ListItem for a list).
	stack []string
	// key is the key of the entry the cursor is in, and afterColon says the
	// cursor is in its value.
	key        string
	afterColon bool
	comment    bool
}

// flowScan reads one line up to the cursor: the key before each `:`, and
// each `{` or `[` that opens inside it.
func flowScan(s string) flowState {
	var st flowState
	var frames []int // how many path segments each open bracket added
	seg := 0         // start of the current entry
	open := func(segs ...string) {
		st.stack = append(st.stack, segs...)
		frames = append(frames, len(segs))
		st.key, st.afterColon = "", false
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'':
			end := strings.IndexByte(s[i+1:], c)
			if end < 0 {
				return st // inside a quoted scalar: still a value
			}
			i += end + 1
		case '#':
			if i == 0 || s[i-1] == ' ' {
				st.comment = true
				return st
			}
		case ':':
			if i+1 == len(s) || s[i+1] == ' ' {
				st.key = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s[seg:i]), "- "))
				st.afterColon = true
			}
		case '{':
			if st.afterColon && st.key != "" {
				open(st.key)
			} else {
				open() // `- {…}`: the mapping is the list item itself
			}
			seg = i + 1
		case '[':
			if st.afterColon && st.key != "" {
				open(st.key, ListItem)
			} else {
				open(ListItem)
			}
			seg = i + 1
		case '}', ']':
			if n := len(frames); n > 0 {
				st.stack = st.stack[:len(st.stack)-frames[n-1]]
				frames = frames[:n-1]
			}
			st.key, st.afterColon = "", false
		case ',':
			st.key, st.afterColon, seg = "", false, i+1
		}
	}
	return st
}

// OffsetOf turns an LSP position (0-based line, UTF-16 character) into a
// byte offset in text, clamped to the line.
func OffsetOf(text string, line, character int) int {
	off := 0
	for l := 0; l < line; l++ {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	units := 0
	for off < len(text) && text[off] != '\n' && units < character {
		r, size := utf8.DecodeRuneInString(text[off:])
		units += max(1, utf16.RuneLen(r))
		off += size
	}
	return off
}

// PositionOf turns a byte offset into an LSP position.
func PositionOf(text string, offset int) (line, character int) {
	offset = max(0, min(offset, len(text)))
	lineStart := strings.LastIndexByte(text[:offset], '\n') + 1
	line = strings.Count(text[:lineStart], "\n")
	for _, r := range text[lineStart:offset] {
		character += max(1, utf16.RuneLen(r))
	}
	return line, character
}
