package brick

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ubgo/brick/internal/frontmatter"
)

// The schema must describe every key parseMeta reads: a key added to a list
// in meta.go (or slots.go) and not to the schema's tables fails here.
func TestFrontmatterSchemaCoversEveryKey(t *testing.T) {
	levels := []struct {
		name  string
		keys  []string
		table map[string]keySchema
	}{
		{"top level (engineKeys)", engineKeys, topLevelSchemas},
		{"brick: (brickKeys)", brickKeys, brickSchemas},
		{"props: (propSettings)", propSettings, propSettingSchemas},
		{"brick: {slots:} (slotSettings)", slotSettings, slotSettingSchemas},
	}
	for _, l := range levels {
		for _, k := range l.keys {
			ks, ok := l.table[k]
			if !ok {
				t.Errorf("%s: %q has no schema entry; describe it in frontmatterschema.go", l.name, k)
				continue
			}
			if strings.TrimSpace(ks.doc) == "" {
				t.Errorf("%s: %q has no description", l.name, k)
			}
		}
		for k := range l.table {
			if !slices.Contains(l.keys, k) {
				t.Errorf("%s: schema describes %q, which the engine does not read", l.name, k)
			}
		}
	}

	// The generated document carries the same key sets.
	s := decodeSchema(t)
	props := s["properties"].(map[string]any)
	if got := slices.Sorted(maps.Keys(props)); !slices.Equal(got, slices.Sorted(slices.Values(engineKeys))) {
		t.Errorf("top-level properties = %v, want engineKeys %v", got, engineKeys)
	}
	brickProps := props[keyBrick].(map[string]any)["properties"].(map[string]any)
	if got := slices.Sorted(maps.Keys(brickProps)); !slices.Equal(got, slices.Sorted(slices.Values(brickKeys))) {
		t.Errorf("brick: properties = %v, want brickKeys %v", got, brickKeys)
	}
}

func TestFrontmatterSchemaIsDraft202012(t *testing.T) {
	s := decodeSchema(t)
	if s["$schema"] != jsonSchemaDialect || s["$id"] != FrontmatterSchemaID {
		t.Fatalf("$schema/$id = %v / %v", s["$schema"], s["$id"])
	}
	// Same input, same bytes.
	if a, b := FrontmatterSchema(), FrontmatterSchema(); string(a) != string(b) {
		t.Fatal("FrontmatterSchema is not deterministic")
	}
}

// Every frontmatter block in kit/ and examples/ validates: the schema has
// no false errors on real files.
func TestFrontmatterSchemaAcceptsRepoFiles(t *testing.T) {
	schema := decodeSchema(t)
	checked := 0
	for _, root := range []string{"kit", "examples"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if ext := filepath.Ext(path); ext != ".html" && ext != ".md" {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			doc, err := frontmatter.Parse(src)
			if err != nil || !doc.HasFrontmatter {
				return nil // brick check reports a block that does not parse
			}
			checked++
			for _, e := range validateSchema(schema, schema, asJSON(t, doc.Params), "") {
				t.Errorf("%s: %s", path, e)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Guard against a walk that silently finds nothing.
	if checked < 300 {
		t.Fatalf("validated only %d frontmatter blocks; expected the kit and examples", checked)
	}
	t.Logf("validated %d frontmatter blocks", checked)
}

// The schema agrees with parseMeta on shapes at the edges: what parseMeta
// accepts validates, what it rejects does not.
func TestFrontmatterSchemaMatchesParseMeta(t *testing.T) {
	schema := decodeSchema(t)
	cases := []struct {
		yaml string
		ok   bool
		// site marks keys only the site layer reads, which parseMeta passes.
		site bool
	}{
		{"title: Hi\nanything: [1, 2]", true, false},
		{"layout: none", true, false},
		{"layout: 3", false, false},
		{"props:\n  title:\n  count: int\n  size: {type: string, oneof: [sm, lg], default: sm, doc: Size, examples: [sm, 3, true]}", true, false},
		{"props:\n  children: string", false, false},
		{"props:\n  key: string", false, false},
		{"props:\n  title: {kind: string}", false, false},
		{"props:\n  title: {oneof: []}", false, false},
		{"props:\n  title: {required: yes please}", false, false},
		{"props:\n  title: 3", false, false},
		{"load: posts.recent", true, false},
		{"load: \"\"", false, false},
		{"inject:\n  theme:\n  user: {required: true, default: x}", true, false},
		{"inject:\n  theme: dark", false, false},
		{"raw: true\ndedent: false\nstrictParams: true\nstrictProps: false", true, false},
		{"raw: yes please", false, false},
		{"cache: {tags: [a, b]}", true, false},
		{"cache: {ttl: 3}", false, false},
		{"unknown: passthrough\nsyntax: hyphenated\npassthrough: [my-el]", true, false},
		{"unknown: ignore", false, false},
		{"syntax: lower", false, false},
		{"use: {__x: ~/a.html}", false, false},
		{"state: {page: 1}\nbrick:\n  island: true\n  lazy: visible\n  store: redis\n  on: {saved: $refresh}\n  init: posts.seed\n  updated: fix\n  remember: [page]\n  temporary: [page]\n  query: [page]", true, false},
		{"brick: {lazy: sometimes}", false, false},
		{"brick: {island: maybe}", false, false},
		{"brick: {store: \"\"}", false, false},
		{"brick: {on: {saved: 3}}", false, false},
		{"brick: {init: $set}", false, false},
		{"brick: {init: a.b.c}", false, false},
		{"state: {email: '', tags: []}\nbrick:\n  rules: {email: [required, email, {max: 80}], tags: required}\n  messages: {email.required: Needed}", true, false},
		{"brick: {rules: {email: [{min: 1, max: 2}]}}", false, false},
		{"brick: {rules: {email: [3]}}", false, false},
		{"brick: {messages: {email.required: 3}}", false, false},
		{"state: {a: '', b: []}\nbrick:\n  uploads: {a: {accept: image/png, max_size: 2MB}, b: {accept: [IMAGE/*], max_size: 1024, max_files: 3}}", true, false},
		{"brick: {uploads: {a: {accept: [png]}}}", false, false},
		{"brick: {uploads: {a: {max_size: big}}}", false, false},
		{"brick: {uploads: {a: {max_files: 0}}}", false, false},
		{"brick: {uploads: {a: {quota: 3}}}", false, false},
		{"brick: {uploads: {a: 3}}", false, false},
		{"state: {page: 1, q: ''}\nbrick: {query: {page: {as: p, history: push, keep: true}, q:}}", true, false},
		{"brick: {query: {page: {history: back}}}", false, false},
		{"brick: {query: {page: {alias: p}}}", false, false},
		{"brick: {query: page}", false, false},
		{"brick: {remember: view}", false, false},
		{"brick:\n  slots:\n    children: {required: true, doc: Body, examples: ['<p>x</p>']}\n    footer:", true, false},
		{"brick: {slots: {footer: {optional: true}}}", false, false},
		{"brick: {slots: {footer: 3}}", false, false},
		{"brick: {load_many: users.cards}", true, false},
		{"brick: {url: /about/, paths: posts.slugs}", true, true},
		{"brick: {url: about}", false, true},
		{"brick: 3", false, false},
	}
	for _, c := range cases {
		src := []byte("---\n" + c.yaml + "\n---\n<p>x</p>\n")
		doc, err := frontmatter.Parse(src)
		if err != nil {
			t.Fatalf("%q: %v", c.yaml, err)
		}
		errs := validateSchema(schema, schema, asJSON(t, doc.Params), "")
		if got := len(errs) == 0; got != c.ok {
			t.Errorf("schema on %q: valid=%v, want %v (%v)", c.yaml, got, c.ok, errs)
		}
		if c.site {
			continue
		}
		_, perr := parseMeta(doc, "x.html", func(_, p string) (string, error) { return p, nil }, nil, nil)
		if got := perr == nil; got != c.ok {
			t.Errorf("parseMeta on %q: ok=%v, want %v (%v) — the case table is wrong", c.yaml, got, c.ok, perr)
		}
	}
}

// An unknown brick: key is only a warning for parseMeta (ignored), but the
// schema flags it, because in an editor it is almost always a typo.
func TestFrontmatterSchemaFlagsUnknownBrickKey(t *testing.T) {
	doc, err := frontmatter.Parse([]byte("---\nbrick: {iland: true}\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := parseMeta(doc, "x.html", func(_, p string) (string, error) { return p, nil }, nil, nil)
	if err != nil || len(m.warnings) == 0 {
		t.Fatalf("parseMeta: err=%v warnings=%v, want a warning", err, m.warnings)
	}
	schema := decodeSchema(t)
	if errs := validateSchema(schema, schema, asJSON(t, doc.Params), ""); len(errs) == 0 {
		t.Fatal("schema accepted brick: {iland:}")
	}
}

func decodeSchema(t *testing.T) map[string]any {
	t.Helper()
	var s map[string]any
	if err := json.Unmarshal(FrontmatterSchema(), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// asJSON turns decoded YAML into JSON values, what an editor validates.
func asJSON(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// validateSchema is a small JSON Schema validator for the keywords
// FrontmatterSchema uses, kept in the test so the root module adds no
// dependency. It fails on a keyword it does not know, so the schema cannot
// grow one this check silently ignores.
func validateSchema(root, s map[string]any, v any, at string) []string {
	var errs []string
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Sprintf("%s: %s", where(at), fmt.Sprintf(format, args...)))
	}
	for k, kv := range s {
		switch k {
		case "$schema", "$id", "$defs", "title", "description":
		case "$ref":
			name := strings.TrimPrefix(kv.(string), "#/$defs/")
			errs = append(errs, validateSchema(root, root["$defs"].(map[string]any)[name].(map[string]any), v, at)...)
		case "type":
			var types []any
			if list, ok := kv.([]any); ok {
				types = list
			} else {
				types = []any{kv}
			}
			if !slices.ContainsFunc(types, func(t any) bool { return jsonTypeIs(v, t.(string)) }) {
				fail("%v is not %v", v, kv)
			}
		case "enum":
			if !slices.Contains(kv.([]any), v) {
				fail("%v is not one of %v", v, kv)
			}
		case "anyOf":
			ok := false
			for _, sub := range kv.([]any) {
				if len(validateSchema(root, sub.(map[string]any), v, at)) == 0 {
					ok = true
					break
				}
			}
			if !ok {
				fail("%v matches none of anyOf", v)
			}
		case "not":
			if len(validateSchema(root, kv.(map[string]any), v, at)) == 0 {
				fail("%v matches a forbidden shape", v)
			}
		case "pattern":
			if str, ok := v.(string); ok && !regexp.MustCompile(kv.(string)).MatchString(str) {
				fail("%q does not match %s", str, kv)
			}
		case "minLength":
			if str, ok := v.(string); ok && float64(len(str)) < kv.(float64) {
				fail("%q is too short", str)
			}
		case "minimum":
			if n, ok := v.(float64); ok && n < kv.(float64) {
				fail("%v is below %v", n, kv)
			}
		case "minItems":
			if list, ok := v.([]any); ok && float64(len(list)) < kv.(float64) {
				fail("needs at least %v items", kv)
			}
		case "items":
			if list, ok := v.([]any); ok {
				for i, item := range list {
					errs = append(errs, validateSchema(root, kv.(map[string]any), item, fmt.Sprintf("%s[%d]", at, i))...)
				}
			}
		case "minProperties", "maxProperties":
			if m, ok := v.(map[string]any); ok {
				n := float64(len(m))
				if (k == "minProperties" && n < kv.(float64)) || (k == "maxProperties" && n > kv.(float64)) {
					fail("has %v keys (%s %v)", n, k, kv)
				}
			}
		case "propertyNames":
			if m, ok := v.(map[string]any); ok {
				for name := range m {
					errs = append(errs, validateSchema(root, kv.(map[string]any), name, at+"."+name)...)
				}
			}
		case "properties":
			if m, ok := v.(map[string]any); ok {
				for name, sub := range kv.(map[string]any) {
					if pv, ok := m[name]; ok {
						errs = append(errs, validateSchema(root, sub.(map[string]any), pv, at+"."+name)...)
					}
				}
			}
		case "additionalProperties":
			m, ok := v.(map[string]any)
			if !ok {
				continue
			}
			known, _ := s["properties"].(map[string]any)
			for name, pv := range m {
				if _, ok := known[name]; ok {
					continue
				}
				switch ap := kv.(type) {
				case bool:
					if !ap {
						fail("unknown key %q", name)
					}
				case map[string]any:
					errs = append(errs, validateSchema(root, ap, pv, at+"."+name)...)
				}
			}
		default:
			panic("validateSchema: keyword " + k + " is not supported; teach the test validator")
		}
	}
	return errs
}

func where(at string) string {
	if at == "" {
		return "(root)"
	}
	return strings.TrimPrefix(at, ".")
}

func jsonTypeIs(v any, t string) bool {
	switch t {
	case "null":
		return v == nil
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		n, ok := v.(float64)
		return ok && n == math.Trunc(n)
	case "array":
		_, ok := v.([]any)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	}
	return false
}
