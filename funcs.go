package brick

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ubgo/brick/internal/transpile"
)

// goBuiltins are text/template's predefined functions; a script function
// or import may not shadow them either (C12).
var goBuiltins = []string{"and", "or", "not", "len", "index", "slice", "print", "printf", "println", "html", "js", "urlquery", "eq", "ne", "lt", "le", "gt", "ge", "call"}

// builtinNames is every author-facing function name (C12).
var builtinNames = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range goBuiltins {
		m[n] = true
	}
	for n := range pureBuiltins() {
		m[n] = true
	}
	m["sanitize"] = true
	return m
}()

// checkHostFuncs vets Options.Funcs once, at New. A host function is part
// of the template language a theme sees, so the C12 rule applies to it as to
// a script: it may not shadow a built-in, and `__` names belong to the
// engine (T5). The value must be a function html/template accepts (one
// result, or a result and an error); checking here turns the panic
// template.Funcs would raise at the first compile into an error from New.
// Host functions are shared by concurrent renders, so they must be safe for
// concurrent use and pure (same input, same output: T8, E6).
func checkHostFuncs(fm template.FuncMap) error {
	for _, name := range slices.Sorted(maps.Keys(fm)) {
		switch {
		case builtinNames[name] || slices.Contains(transpile.ScopedFuncs, name):
			return fmt.Errorf("Options.Funcs: %q shadows a built-in function (C12)", name)
		case strings.HasPrefix(name, "__"):
			return fmt.Errorf("Options.Funcs: %q: names starting with __ are reserved (T5)", name)
		case !isFuncIdent(name):
			return fmt.Errorf("Options.Funcs: %q is not a valid function name (a letter or _, then letters, digits or _)", name)
		}
		t := reflect.TypeOf(fm[name])
		if t == nil || t.Kind() != reflect.Func {
			return fmt.Errorf("Options.Funcs: %q is a %T, not a function", name, fm[name])
		}
		errType := reflect.TypeFor[error]()
		switch {
		case t.NumOut() == 1:
		case t.NumOut() == 2 && t.Out(1) == errType:
		default:
			return fmt.Errorf("Options.Funcs: %q must return one value, or a value and an error", name)
		}
	}
	return nil
}

// isFuncIdent is text/template's rule for a function name.
func isFuncIdent(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && !unicode.IsLetter(r) && (i == 0 || !unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

// funcMap is the engine's static function map (T8): fixed closures shared
// by every render, reaching render state only through `$`.
func (e *Engine) funcMap() template.FuncMap {
	e.funcsOnce.Do(func() { e.funcs = e.buildFuncs() })
	return e.funcs
}

func (e *Engine) buildFuncs() template.FuncMap {
	fm := template.FuncMap{}
	// Host functions go in first; checkHostFuncs has already refused any
	// name a built-in owns, so nothing below overwrites one (C12).
	for k, v := range e.opts.Funcs {
		fm[k] = v
	}
	for k, v := range pureBuiltins() {
		fm[k] = v
	}
	fm["sanitize"] = func(s string) (template.HTML, error) {
		if e.opts.Sanitizer == nil {
			return "", errors.New("sanitize needs Options.Sanitizer (for example contrib/sanitize-bluemonday); there is no unsafe default")
		}
		// The sanitizer is the host's allowlist policy; its output is the one
		// path from data to markup.
		return template.HTML(e.opts.Sanitizer(s)), nil
	}
	fm[transpile.FnRender] = e.render
	fm[transpile.FnList] = func(a ...any) []any { return a }
	fm[transpile.FnOnce] = func(v any) []any { return []any{v} }
	fm[transpile.FnCat] = func(a ...any) string {
		var b strings.Builder
		for _, v := range a {
			if v != nil {
				fmt.Fprint(&b, v)
			}
		}
		return b.String()
	}
	fm[transpile.FnWrap] = func(root any, dot any) (bodyScope, error) {
		s, err := scopeOf(root)
		if err != nil {
			return bodyScope{}, err
		}
		return bodyScope{Root: s, Dot: dot}, nil
	}
	scoped := func(name string) string { return transpile.ScopedName(name) }
	fm[scoped("children")] = func(root any) (string, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		return "", s.st.children(s)
	}
	fm[scoped("node")] = func(root any, name string, slot ...any) (string, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		return "", s.st.node(s, name, slot...)
	}
	fm[scoped("hasNode")] = func(root any, name string) (bool, error) {
		s, err := scopeOf(root)
		if err != nil {
			return false, err
		}
		return hasNode(s, name), nil
	}
	fm[scoped("child")] = func(root any, c Child) (string, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		return "", s.st.child(s, c)
	}
	fm[scoped("provide")] = func(root any, key string, v any) (string, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		s.st.provide(s, key, v)
		return "", nil
	}
	fm[scoped("attrs")] = func(root any, except ...string) (template.HTMLAttr, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		return attrsHTML(s, except...), nil
	}
	fm[scoped("nonce")] = func(root any) (string, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		s.st.out.mark(ChunkNonce)
		return "", nil
	}
	fm[scoped("assets")] = func(root any) (string, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		s.st.out.mark(ChunkAssets)
		return "", nil
	}
	fm[scoped("cacheTag")] = func(root any, tags ...string) (string, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		for _, t := range tags {
			s.st.tags[t] = true
		}
		return "", nil
	}
	fm[scoped("t")] = func(root any, key string, args ...any) (string, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		return s.st.translate(s, key, args...)
	}
	fm[fnClientAttrs] = func(root any) (template.HTMLAttr, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		return s.st.clientAttrs(s)
	}
	fm[scoped("image")] = func(root any, p string, args ...any) (Image, error) {
		s, err := scopeOf(root)
		if err != nil {
			return Image{}, err
		}
		return s.st.image(s, p, args...)
	}
	fm[fnStateAttrs] = func(root any) (template.HTMLAttr, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		return stateAttrs(s)
	}
	fm[fnUpAttrs] = func(root any) (template.HTMLAttr, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		up, err := upAttrs(s)
		if err != nil {
			return "", err
		}
		// The root marker also carries the Dev-only source location.
		return up + locAttr(s), nil
	}
	fm[fnIDAttr] = func(root any) (template.HTMLAttr, error) {
		s, err := scopeOf(root)
		if err != nil || s.ID == "" {
			return "", err
		}
		// An id is hex from instanceID, so it needs no escaping.
		return template.HTMLAttr(`data-brick="` + s.ID + `"`), nil
	}
	fm[scoped("dateFormat")] = func(root any, layout string, v any) (string, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		return dateFormat(layout, v, s.st.e.opts.TimeZone, s.st.activeLocale(s))
	}
	fm[scoped("numberFormat")] = func(root any, v any, decimals ...int) (string, error) {
		s, err := scopeOf(root)
		if err != nil {
			return "", err
		}
		return numberFormat(v, s.st.activeLocale(s), decimals...)
	}
	return fm
}

// jsonValue is what toJSON returns (D13): raw JSON that html/template
// marshals with its own escaping inside <script> and prints as text
// elsewhere.
type jsonValue []byte

// MarshalJSON returns the JSON as is.
func (j jsonValue) MarshalJSON() ([]byte, error) { return j, nil }

func (j jsonValue) String() string { return string(j) }

// pureBuiltins are the curated built-ins that need no render state.
func pureBuiltins() map[string]any {
	return map[string]any{
		"default":  defaultFn,
		"coalesce": coalesce,
		"dict":     dict,
		"list":     func(a ...any) []any { return a },
		"omit":     omit,
		"cx":       cx,
		"cond":     cond,
		"sortBy":   sortBy,
		"pick":     pick,
		"has":      has,
		"keys":     keys,
		"first":    first,
		"last":     last,
		"seq":      seq,
		"toJSON": func(v any) (jsonValue, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("toJSON: %w", err)
			}
			return jsonValue(b), nil
		},
		"fromJSON": func(s string) (any, error) {
			dec := json.NewDecoder(strings.NewReader(s))
			dec.UseNumber()
			var v any
			if err := dec.Decode(&v); err != nil {
				return nil, fmt.Errorf("fromJSON: %w", err)
			}
			return normalizeJSON(v), nil
		},
		"upper":      strings.ToUpper,
		"lower":      strings.ToLower,
		"trim":       strings.TrimSpace,
		"trimPrefix": func(prefix, s string) string { return strings.TrimPrefix(s, prefix) },
		"trimSuffix": func(suffix, s string) string { return strings.TrimSuffix(s, suffix) },
		"replace":    func(old, repl, s string) string { return strings.ReplaceAll(s, old, repl) },
		"split":      func(sep, s string) []string { return strings.Split(s, sep) },
		"join":       join,
		"contains":   func(sub, s string) bool { return strings.Contains(s, sub) },
		"hasPrefix":  func(prefix, s string) bool { return strings.HasPrefix(s, prefix) },
		"hasSuffix":  func(suffix, s string) bool { return strings.HasSuffix(s, suffix) },
		"truncate":   truncate,
		"slugify":    slugify,
		"pluralize":  pluralize,

		// The arithmetic built-ins: each name is its arithOp.
		string(opAdd): arith(opAdd),
		string(opSub): arith(opSub),
		string(opMul): arith(opMul),
		string(opDiv): arith(opDiv),
		string(opMod): arith(opMod),
		string(opMax): arith(opMax),
		string(opMin): arith(opMin),

		"tag":    tagOpen,
		"endtag": tagClose,
	}
}

// tagOpen is the tag built-in: an element whose name is data, such
// as a heading level, written as a whole start tag —
// {{ tag (print "h" .Props.level) "class" "title" }}…{{ endtag … }}.
// It returns markup rather than a name because html/template escapes the
// `<` before a computed name; and since html/template cannot then see the
// attributes, the element must be on writableTags and every attribute name
// must pass tagAttrAllowed, while values are always HTML-escaped. A true
// value writes a bare attribute and false or nil leaves it out, as attrs
// does.
func tagOpen(name string, attrs ...any) (template.HTML, error) {
	el, err := writableTag(name)
	if err != nil {
		return "", err
	}
	if len(attrs)%2 != 0 {
		return "", fmt.Errorf("tag %s: attributes come in name, value pairs", el)
	}
	var b strings.Builder
	b.WriteString("<" + el)
	for i := 0; i < len(attrs); i += 2 {
		n, ok := attrs[i].(string)
		if !ok || !tagAttrAllowed(n) {
			return "", fmt.Errorf("tag %s: %v is not an attribute tag can write (no on* handlers, no URL or style attributes)", el, attrs[i])
		}
		switch v := attrs[i+1].(type) {
		case nil:
		case bool:
			if v {
				b.WriteString(" " + n)
			}
		default:
			fmt.Fprintf(&b, ` %s="%s"`, n, template.HTMLEscapeString(fmt.Sprint(v)))
		}
	}
	b.WriteString(">")
	// Trusted: the element is on writableTags, every name passed
	// tagAttrAllowed and every value is escaped.
	return template.HTML(b.String()), nil
}

// tagClose is endtag: the end tag that matches tag, under the same list.
func tagClose(name string) (template.HTML, error) {
	el, err := writableTag(name)
	if err != nil {
		return "", err
	}
	return template.HTML("</" + el + ">"), nil
}

// writableTag lowercases and checks an element name for tag and endtag.
func writableTag(name string) (string, error) {
	el := strings.ToLower(strings.TrimSpace(name))
	if !writableTags[el] {
		return "", fmt.Errorf("tag: %q is not an element tag can write; it writes only elements without URL, script or raw-text content (h1–h6, p, div, span, section, li, td, …)", name)
	}
	return el, nil
}

// tagAttrAllowed accepts an attribute name tag may write: a safe name
// (validAttrName refuses on* handlers) that carries no URL or CSS. Like
// html/template's own attribute typing, a name containing src, uri or url
// (data-src, data-url) counts as a URL.
func tagAttrAllowed(n string) bool {
	l := strings.ToLower(n)
	if !validAttrName(n) || urlAttrs[l] {
		return false
	}
	return !strings.Contains(l, "src") && !strings.Contains(l, "uri") && !strings.Contains(l, "url")
}

// defaultFn returns def when v is empty: nil, "", zero, false or an empty
// collection. Pipeline order: `.X | default "y"`.
func defaultFn(def any, v ...any) any {
	if len(v) == 0 || isEmpty(v[0]) {
		return def
	}
	return v[0]
}

func coalesce(vs ...any) any {
	for _, v := range vs {
		if !isEmpty(v) {
			return v
		}
	}
	return nil
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// dict builds a map from key/value pairs (used to pass several values into
// a scoped slot or a translation).
func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, errors.New("dict needs key/value pairs")
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %v is not a string", kv[i])
		}
		m[k] = kv[i+1]
	}
	return m, nil
}

// omit returns a copy of a map without the named keys (C13).
func omit(m any, drop ...string) (map[string]any, error) {
	src, err := toStringMap(m)
	if err != nil {
		return nil, fmt.Errorf("omit: %w", err)
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		if !slices.Contains(drop, k) {
			out[k] = v
		}
	}
	return out, nil
}

// cx joins the class names that are set, so a component can merge its own
// classes with conditional ones and the caller's without stray spaces:
// cx "bk-btn" (cond .Props.block "bk-btn--block" "") .Attrs.class. Empty
// strings, nil and false are skipped; a list contributes each of its items.
//
// Its parameters are any because template values arrive untyped through the
// FuncMap; the type switch below narrows each one at once.
func cx(parts ...any) string {
	var out []string
	var add func(v any)
	add = func(v any) {
		switch x := v.(type) {
		case nil:
		case bool:
		case string:
			if f := strings.Fields(x); len(f) > 0 {
				out = append(out, f...)
			}
		case []string:
			for _, e := range x {
				add(e)
			}
		case []any:
			for _, e := range x {
				add(e)
			}
		default:
			add(fmt.Sprint(x))
		}
	}
	for _, p := range parts {
		add(p)
	}
	return strings.Join(out, " ")
}

// cond is the inline if: cond test a b returns a when test is truthy (as
// {{ if }} judges it) and b otherwise. It exists because a class or an
// attribute value often depends on one flag, and a whole {{ if }} block
// inside a quoted attribute is hard to read. It is typed any at the FuncMap
// boundary on purpose: it returns one of its arguments untouched, whatever
// the template passed.
func cond(test, a, b any) any {
	if truthy(test) {
		return a
	}
	return b
}

// sortBy returns a sorted copy of a list of maps or structs, ordered by
// one key or field: sortBy "Title" .Site.Pages, "-Date" for descending,
// or a dotted path into nested maps and structs: "Params.order".
// Numbers compare as numbers, times as times, and anything else as text,
// case-insensitively, so a listing reads the way a person expects. The sort
// is stable, so equal items keep their order and output stays
// deterministic. A missing key sorts first.
//
// list is any because a template passes any list type ([]Page, []any,
// []map[string]any…); reflection narrows it to its items, and the result is
// []any because the items keep their own types for the template.
func sortBy(key string, list any) ([]any, error) {
	desc := strings.HasPrefix(key, sortDescPrefix)
	key = strings.TrimPrefix(key, sortDescPrefix)
	if list == nil {
		return nil, nil
	}
	rv := reflect.ValueOf(list)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("sortBy: want a list, got %T", list)
	}
	items := make([]any, rv.Len())
	for i := range items {
		items[i] = rv.Index(i).Interface()
	}
	vals := make([]any, len(items))
	for i, it := range items {
		v := it
		for part := range strings.SplitSeq(key, sortPathSep) {
			v = fieldOf(v, part)
		}
		vals[i] = v
	}
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		if desc {
			return compareValues(vals[b], vals[a])
		}
		return compareValues(vals[a], vals[b])
	})
	out := make([]any, len(items))
	for i, j := range idx {
		out[i] = items[j]
	}
	return out, nil
}

// sortDescPrefix on a sortBy key reverses the order; sortPathSep separates
// the steps of a dotted key into nested maps and structs.
const (
	sortDescPrefix = "-"
	sortPathSep    = "."
)

// fieldOf reads key from a map or an exported struct field (through
// pointers); nil when there is none.
func fieldOf(v any, key string) any {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil
		}
		mv := rv.MapIndex(reflect.ValueOf(key).Convert(rv.Type().Key()))
		if !mv.IsValid() {
			return nil
		}
		return mv.Interface()
	case reflect.Struct:
		f := rv.FieldByName(key)
		if !f.IsValid() || !f.CanInterface() {
			return nil
		}
		return f.Interface()
	}
	return nil
}

// compareValues orders two values for sortBy: nil first, then numbers,
// times, and text without regard to case.
func compareValues(a, b any) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	if fa, ok := asFloat(a); ok {
		if fb, ok := asFloat(b); ok {
			return cmp.Compare(fa, fb)
		}
	}
	if ta, ok := a.(time.Time); ok {
		if tb, ok := b.(time.Time); ok {
			return ta.Compare(tb)
		}
	}
	sa, sb := fmt.Sprint(a), fmt.Sprint(b)
	if c := strings.Compare(strings.ToLower(sa), strings.ToLower(sb)); c != 0 {
		return c
	}
	return strings.Compare(sa, sb)
}

// asFloat reports a numeric value as a float64.
func asFloat(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// truthy is {{ if }}'s notion of true: the zero value of any type is false.
func truthy(v any) bool {
	if v == nil {
		return false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map, reflect.Array, reflect.String:
		return rv.Len() > 0
	case reflect.Pointer, reflect.Interface:
		return !rv.IsNil()
	}
	return !rv.IsZero()
}

func pick(m any, want ...string) (map[string]any, error) {
	src, err := toStringMap(m)
	if err != nil {
		return nil, fmt.Errorf("pick: %w", err)
	}
	out := map[string]any{}
	for _, k := range want {
		if v, ok := src[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func has(m any, key string) bool {
	src, err := toStringMap(m)
	if err != nil {
		return false
	}
	_, ok := src[key]
	return ok
}

// keys returns a map's keys, sorted (matching how range orders them).
func keys(m any) ([]string, error) {
	src, err := toStringMap(m)
	if err != nil {
		return nil, fmt.Errorf("keys: %w", err)
	}
	return sortedKeys(src), nil
}

func first(v any) any {
	rv := reflect.ValueOf(v)
	if (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) && rv.Len() > 0 {
		return rv.Index(0).Interface()
	}
	return nil
}

func last(v any) any {
	rv := reflect.ValueOf(v)
	if (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) && rv.Len() > 0 {
		return rv.Index(rv.Len() - 1).Interface()
	}
	return nil
}

// maxSeq bounds seq so a data value cannot allocate without limit.
const maxSeq = 100_000

// seq returns 1..n, or from..to.
func seq(a ...int) ([]int, error) {
	from, to := 1, 0
	switch len(a) {
	case 1:
		to = a[0]
	case 2:
		from, to = a[0], a[1]
	default:
		return nil, errors.New("seq takes one or two numbers")
	}
	if to-from+1 > maxSeq {
		return nil, fmt.Errorf("seq: more than %d numbers", maxSeq)
	}
	var out []int
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out, nil
}

func join(sep string, v any) (string, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return "", fmt.Errorf("join: want a list, got %T", v)
	}
	parts := make([]string, rv.Len())
	for i := range rv.Len() {
		parts[i] = fmt.Sprint(rv.Index(i).Interface())
	}
	return strings.Join(parts, sep), nil
}

// truncate shortens s to n runes, adding "…" when it cut.
func truncate(n int, s string) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:max(n, 0)])
	}
	return strings.TrimRightFunc(string(r[:n-1]), unicode.IsSpace) + "…"
}

// slugify lowercases and joins words with hyphens, dropping other symbols.
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// pluralize picks the singular for 1 and the plural otherwise. For real
// translations use `t` with a plural message, which follows CLDR rules.
func pluralize(n any, singular, plural string) (string, error) {
	f, err := toFloat(n)
	if err != nil {
		return "", fmt.Errorf("pluralize: %w", err)
	}
	if f == 1 {
		return singular, nil
	}
	return plural, nil
}

// arithOp is one of the arithmetic built-ins; its value is the function
// name a template calls, so the funcs table and arith cannot disagree.
type arithOp string

const (
	opAdd arithOp = "add"
	opSub arithOp = "sub"
	opMul arithOp = "mul"
	opDiv arithOp = "div"
	opMod arithOp = "mod"
	opMax arithOp = "max"
	opMin arithOp = "min"
)

// arith returns a numeric function that keeps integers integral: two
// integers give an integer (div truncates, as Go does), anything else is
// computed in float64. Division and integer mod by zero are errors rather
// than a panic or an Inf, because the value lands in a page.
func arith(op arithOp) func(a, b any) (any, error) {
	return func(a, b any) (any, error) {
		ai, aInt := toInt(a)
		bi, bInt := toInt(b)
		if aInt && bInt {
			switch op {
			case opAdd:
				return ai + bi, nil
			case opSub:
				return ai - bi, nil
			case opMul:
				return ai * bi, nil
			case opDiv:
				if bi == 0 {
					return nil, errors.New("div: division by zero")
				}
				return ai / bi, nil
			case opMod:
				if bi == 0 {
					return nil, errors.New("mod: division by zero")
				}
				return ai % bi, nil
			case opMax:
				return max(ai, bi), nil
			case opMin:
				return min(ai, bi), nil
			}
		}
		af, err := toFloat(a)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		bf, err := toFloat(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		switch op {
		case opAdd:
			return af + bf, nil
		case opSub:
			return af - bf, nil
		case opMul:
			return af * bf, nil
		case opDiv:
			if bf == 0 {
				return nil, errors.New("div: division by zero")
			}
			return af / bf, nil
		case opMod:
			return math.Mod(af, bf), nil
		case opMax:
			return math.Max(af, bf), nil
		case opMin:
			return math.Min(af, bf), nil
		}
		// Unreachable while every arithOp has a case above: a new op added
		// to the funcs table without one fails here instead of silently
		// computing something else.
		return nil, fmt.Errorf("brick: arithmetic op %q has no implementation", op)
	}
}

func toInt(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case int32:
		return int64(x), true
	}
	return 0, false
}

func toFloat(v any) (float64, error) {
	switch x := v.(type) {
	case int:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case int32:
		return float64(x), nil
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a number", x)
		}
		return f, nil
	case json.Number:
		return x.Float64()
	}
	return 0, fmt.Errorf("%T is not a number", v)
}

// dateFormat formats a time.Time or a date string with a Go layout, in
// Options.TimeZone (E6) — never the process's local zone — with month and
// day names in the active locale.
func dateFormat(layout string, v any, tz *time.Location, locale string) (string, error) {
	var t time.Time
	switch x := v.(type) {
	case time.Time:
		t = x
	case string:
		for _, l := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
			if p, err := time.ParseInLocation(l, x, tz); err == nil {
				t = p
				break
			}
		}
		if t.IsZero() {
			return "", fmt.Errorf("dateFormat: %q is not a date", x)
		}
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("dateFormat: %T is not a date", v)
	}
	return formatLocal(t.In(tz), layout, locale), nil
}

// numberSeparators are grouping and decimal marks by language. A language
// not listed uses the English marks.
var numberSeparators = map[string][2]string{
	"en": {",", "."}, "de": {".", ","}, "fr": {" ", ","}, "es": {".", ","},
	"it": {".", ","}, "pt": {".", ","}, "nl": {".", ","}, "ru": {" ", ","},
	"pl": {" ", ","}, "sv": {" ", ","}, "ja": {",", "."}, "zh": {",", "."},
	"hi": {",", "."}, "ar": {",", "."},
}

// numberFormat groups digits for the locale: 1234567.5 → "1,234,567.5".
func numberFormat(v any, locale string, decimals ...int) (string, error) {
	f, err := toFloat(v)
	if err != nil {
		return "", fmt.Errorf("numberFormat: %w", err)
	}
	prec := -1
	if len(decimals) > 0 {
		prec = decimals[0]
	}
	s := strconv.FormatFloat(math.Abs(f), 'f', prec, 64)
	intPart, frac, _ := strings.Cut(s, ".")
	lang, _, _ := strings.Cut(locale, "-")
	sep, ok := numberSeparators[lang]
	if !ok {
		sep = numberSeparators["en"]
	}
	var b bytes.Buffer
	if f < 0 {
		b.WriteByte('-')
	}
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteString(sep[0])
		}
		b.WriteRune(c)
	}
	if frac != "" {
		b.WriteString(sep[1] + frac)
	}
	return b.String(), nil
}
