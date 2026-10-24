// The form rewriter: scans rendered HTML for <form> tags and adds the no-JS post machinery (hidden state, CSRF, resume).

package brick

import (
	"bytes"
	"encoding/json"
	"html"
	"slices"
	"strings"
)

// rewriteForms rewrites the brick:submit forms of a rendered island's
// stateful roots (see the file comment). It reads rendered output only, so
// a form a kit component renders from a call site's attributes is found
// like one written in the file. Same input, same bytes.
func rewriteForms(chunks []Chunk) []Chunk {
	if !chunksContain(chunks, "brick:submit") {
		return chunks
	}
	fw := &formRewriter{}
	out := make([]Chunk, len(chunks))
	for i, c := range chunks {
		out[i] = c
		if c.Kind == ChunkBytes {
			out[i].Bytes = fw.rewrite(c.Bytes)
		}
	}
	return out
}

func chunksContain(chunks []Chunk, s string) bool {
	for _, c := range chunks {
		if c.Kind == ChunkBytes && bytes.Contains(c.Bytes, []byte(s)) {
			return true
		}
	}
	return false
}

// formRewriter walks rendered HTML keeping a stack of open elements, the
// stateful roots among them, and the rewritten form being read.
type formRewriter struct {
	stack []string
	roots []formRoot
	form  *formOpen
}

type formRoot struct {
	depth         int // len(stack) once the root is pushed
	handle, token string
	arrays        map[string]bool
}

type formOpen struct {
	depth  int
	call   string
	root   formRoot
	models [][3]string
	seen   map[string]bool
	// uploads maps the form's brick:upload file inputs, part name →
	// field. For such a form the close tag splices the
	// enctype into the start tag and the instance fields right after it,
	// at the offsets recorded in buf when the form opened; a form that
	// closes in another chunk (buf differs) stays as v16 wrote it.
	uploads  map[string]string
	buf      *bytes.Buffer
	inTag    int // where the start tag's added attributes end
	afterTag int // just after the start tag
	// enctype is the author's enctype, if any: a multipart one needs
	// nothing added, another one sends no files, so no upload is wired.
	enctype    string
	hasEnctype bool
}

// htmlAttr is one attribute of a start tag: its lowercased name and its
// unescaped value.
type htmlAttr struct {
	name, value string
	has         bool
}

func (fw *formRewriter) rewrite(src []byte) []byte {
	var out bytes.Buffer
	i := 0
	for i < len(src) {
		lt := bytes.IndexByte(src[i:], '<')
		if lt < 0 {
			out.Write(src[i:])
			break
		}
		out.Write(src[i : i+lt])
		i += lt
		rest := src[i:]
		switch {
		case bytes.HasPrefix(rest, []byte("<!--")):
			end := bytes.Index(rest[4:], []byte("-->"))
			n := len(rest)
			if end >= 0 {
				n = 4 + end + 3
			}
			out.Write(rest[:n])
			i += n
		case len(rest) > 1 && (rest[1] == '!' || rest[1] == '?'):
			n := bytes.IndexByte(rest, '>')
			if n < 0 {
				n = len(rest) - 1
			}
			out.Write(rest[:n+1])
			i += n + 1
		case len(rest) > 2 && rest[1] == '/' && isLetter(rest[2]):
			n := bytes.IndexByte(rest, '>')
			if n < 0 {
				out.Write(rest)
				i = len(src)
				continue
			}
			name := strings.ToLower(tagName(rest[2:n]))
			if name == "form" && fw.form != nil && slices.Index(fw.stack, "form") == fw.form.depth-1 {
				fw.closeForm(&out)
				fw.form = nil
			}
			fw.pop(name)
			out.Write(rest[:n+1])
			i += n + 1
		case len(rest) > 1 && isLetter(rest[1]):
			n, attrs, selfClose, ok := scanStartTag(rest)
			if !ok {
				out.Write(rest)
				i = len(src)
				continue
			}
			name := strings.ToLower(tagName(rest[1:n]))
			tag := rest[:n+1]
			opened := fw.form
			extra := fw.startTag(name, attrs, selfClose)
			if extra != "" {
				// Before the closing > (or />).
				cut := len(tag) - 1
				if selfClose {
					cut--
				}
				out.Write(tag[:cut])
				out.WriteString(extra)
				if fw.form != nil && fw.form != opened {
					fw.form.buf, fw.form.inTag = &out, out.Len()
				}
				out.Write(tag[cut:])
				if fw.form != nil && fw.form != opened {
					fw.form.afterTag = out.Len()
				}
			} else {
				out.Write(tag)
			}
			i += n + 1
			if rawTextTags[name] && !selfClose {
				// Script, style, textarea and title hold text: skip to the
				// close tag, which the next turn handles.
				end := indexFold(src[i:], "</"+name)
				if end < 0 {
					out.Write(src[i:])
					i = len(src)
				} else {
					out.Write(src[i : i+end])
					i += end
				}
			}
		default:
			out.WriteByte('<')
			i++
		}
	}
	return out.Bytes()
}

// startTag records an element and returns the attributes to add to it.
func (fw *formRewriter) startTag(name string, attrs []htmlAttr, selfClose bool) string {
	get := func(n string) htmlAttr {
		for _, a := range attrs {
			if a.name == n {
				return a
			}
		}
		return htmlAttr{}
	}
	// confirmed reports brick:confirm with or without modifiers: a
	// .prompt form needs the visitor's answer as an argument, which no
	// fallback POST can carry, so it opts out like a plain confirm.
	confirmed := slices.ContainsFunc(attrs, func(a htmlAttr) bool {
		return a.name == "brick:confirm" || strings.HasPrefix(a.name, "brick:confirm.")
	})
	pushed := !selfClose && !voidElements[name]
	if pushed {
		fw.stack = append(fw.stack, name)
	}
	if st := get("data-brick-state"); st.has && pushed {
		r := formRoot{depth: len(fw.stack), handle: st.value, token: get("data-brick-token").value, arrays: map[string]bool{}}
		for p := range strings.FieldsSeq(get("data-brick-arrays").value) {
			r.arrays[p] = true
		}
		fw.roots = append(fw.roots, r)
	}
	switch {
	case name == "form" && pushed && fw.form == nil && len(fw.roots) > 0:
		call := get("brick:submit")
		if !call.has || strings.TrimSpace(call.value) == "" || get("action").has || get("method").has || confirmed || strings.Contains(call.value, "{{") {
			return ""
		}
		if m := actionCallRE.FindStringSubmatch(call.value); m == nil || isClientOnlyCall(m[1]) {
			return ""
		}
		root := fw.roots[len(fw.roots)-1]
		if root.handle == "" || root.token == "" {
			return ""
		}
		enc := get("enctype")
		fw.form = &formOpen{depth: len(fw.stack), call: call.value, root: root, seen: map[string]bool{}, enctype: strings.ToLower(strings.TrimSpace(enc.value)), hasEnctype: enc.has}
		return ` method="post"`
	case fw.form != nil && name == "input" && strings.EqualFold(get("type").value, "file") && get("brick:upload").has:
		// A brick:upload file input: the runtime sends its
		// files on change; without the runtime they go with the form.
		field := strings.TrimSpace(get("brick:upload").value)
		if field == "" || get("disabled").has || get("form").has || (fw.form.hasEnctype && fw.form.enctype != formEnctype) {
			return ""
		}
		if fw.form.uploads == nil {
			fw.form.uploads = map[string]string{}
		}
		if own := get("name"); own.has {
			fw.form.uploads[own.value] = field
			return ""
		}
		fw.form.uploads[formUploadPrefix+field] = field
		return ` name="` + html.EscapeString(formUploadPrefix+field) + `"`
	case fw.form != nil && (name == "input" || name == "select" || name == "textarea"):
		var model htmlAttr
		for _, a := range attrs {
			if a.name == modelAttr || strings.HasPrefix(a.name, modelAttr+".") {
				model = a
				break
			}
		}
		path := strings.TrimSpace(model.value)
		if !model.has || path == "" || get("disabled").has || get("form").has {
			return ""
		}
		kind := modelValue
		typ := strings.ToLower(get("type").value)
		switch {
		case name == "input" && typ == "checkbox":
			kind = modelCheck
			if fw.form.root.arrays[path] {
				kind = modelList
			}
		case name == "input" && typ == "radio":
			kind = modelRadio
		case name == "input" && (typ == "file" || typ == "submit" || typ == "button" || typ == "reset" || typ == "image"):
			return ""
		case name == "select" && get("multiple").has:
			kind = modelList
		}
		own := get("name")
		if !fw.form.seen[path] {
			fw.form.seen[path] = true
			n := ""
			if own.has {
				n = own.value
			}
			fw.form.models = append(fw.form.models, [3]string{kind, path, n})
		}
		if own.has {
			return ""
		}
		return ` name="` + html.EscapeString(formModelPrefix+path) + `"`
	}
	return ""
}

// pop closes the innermost open element called name and every element
// opened inside it (an unclosed <p> or <li>), as a parser does; a close tag
// with no open element is ignored.
func (fw *formRewriter) pop(name string) {
	for j := len(fw.stack) - 1; j >= 0; j-- {
		if fw.stack[j] != name {
			continue
		}
		fw.stack = fw.stack[:j]
		for len(fw.roots) > 0 && fw.roots[len(fw.roots)-1].depth > j {
			fw.roots = fw.roots[:len(fw.roots)-1]
		}
		if fw.form != nil && fw.form.depth > j {
			// Closed by an outer element: the browser posts no further
			// fields of it, and it gets no hidden fields here either.
			fw.form = nil
		}
		return
	}
}

// closeForm writes the form's reserved fields before its close tag. For an
// upload form whose start tag is in this buffer, the instance fields and
// the upload map go right after the start tag instead, and the start tag
// gets the multipart enctype, so the server knows the instance before the
// first file part arrives.
func (fw *formRewriter) closeForm(out *bytes.Buffer) {
	f := fw.form
	var lead bytes.Buffer
	field := func(w *bytes.Buffer, name, value string) {
		w.WriteString(`<input type="hidden" name="` + name + `" value="` + html.EscapeString(value) + `">`)
	}
	w := out
	if len(f.uploads) > 0 && f.buf == out {
		w = &lead
	}
	field(w, formFieldState, f.root.handle)
	field(w, formFieldToken, f.root.token)
	field(w, formFieldAction, f.call)
	if w == &lead {
		b, _ := json.Marshal(f.uploads)
		field(w, formFieldUploads, string(b))
		enc := ""
		if !f.hasEnctype {
			enc = ` enctype="` + formEnctype + `"`
		}
		src := out.Bytes()
		spliced := slices.Concat(src[:f.inTag], []byte(enc), src[f.inTag:f.afterTag], lead.Bytes(), src[f.afterTag:])
		out.Reset()
		out.Write(spliced)
	}
	if len(f.models) > 0 {
		b, _ := json.Marshal(f.models)
		field(out, formFieldModels, string(b))
	}
}

// scanStartTag reads a start tag at src[0] == '<': the index of its '>',
// its attributes and whether it ends in "/>". Quoted values may hold '>'.
func scanStartTag(src []byte) (int, []htmlAttr, bool, bool) {
	i := 1
	for i < len(src) && isTagNameByte(src[i]) {
		i++
	}
	var attrs []htmlAttr
	for i < len(src) {
		for i < len(src) && isHTMLSpace(src[i]) {
			i++
		}
		if i >= len(src) {
			return 0, nil, false, false
		}
		switch src[i] {
		case '>':
			return i, attrs, i > 0 && src[i-1] == '/', true
		case '/':
			i++
			continue
		}
		start := i
		for i < len(src) && !isHTMLSpace(src[i]) && src[i] != '=' && src[i] != '>' && !(src[i] == '/' && i+1 < len(src) && src[i+1] == '>') {
			i++
		}
		a := htmlAttr{name: strings.ToLower(string(src[start:i])), has: true}
		j := i
		for j < len(src) && isHTMLSpace(src[j]) {
			j++
		}
		if j < len(src) && src[j] == '=' {
			j++
			for j < len(src) && isHTMLSpace(src[j]) {
				j++
			}
			if j >= len(src) {
				return 0, nil, false, false
			}
			if q := src[j]; q == '"' || q == '\'' {
				end := bytes.IndexByte(src[j+1:], q)
				if end < 0 {
					return 0, nil, false, false
				}
				a.value = html.UnescapeString(string(src[j+1 : j+1+end]))
				i = j + 1 + end + 1
			} else {
				k := j
				for k < len(src) && !isHTMLSpace(src[k]) && src[k] != '>' {
					k++
				}
				a.value = html.UnescapeString(string(src[j:k]))
				i = k
			}
		}
		if a.name != "" {
			attrs = append(attrs, a)
		}
	}
	return 0, nil, false, false
}

// tagName is the name at the start of a tag's text.
func tagName(b []byte) string {
	n := 0
	for n < len(b) && isTagNameByte(b[n]) {
		n++
	}
	return string(b[:n])
}

// isHTMLSpace is HTML's ASCII whitespace, which separates attributes.
func isHTMLSpace(c byte) bool { return isSpace(c) || c == '\f' }

// indexFold is the index of the first case-insensitive match of an ASCII
// needle, or -1.
func indexFold(b []byte, needle string) int {
	n := len(needle)
	for i := 0; i+n <= len(b); i++ {
		if strings.EqualFold(string(b[i:i+n]), needle) {
			return i
		}
	}
	return -1
}
