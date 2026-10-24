package brick

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Messages are translation catalogs: locale tag → nested message map, as
// loaded from `i18n/<locale>.yaml`. A message is a string with `{name}`
// placeholders, or a plural map with CLDR forms (zero one two few many other).
type Messages map[string]map[string]any

// has reports whether a catalog exists for a tag.
func (m Messages) has(tag string) bool {
	_, ok := m[tag]
	return ok
}

// pluralForms are the CLDR plural categories.
var pluralForms = []string{"zero", "one", "two", "few", "many", "other"}

// formOther is the plural form every plural message must have (E9).
const formOther = "other"

// ctxLocale is the well-known context key t, dateFormat and numberFormat
// read; a theme provides it like any other value.
const ctxLocale = "locale"

// LoadMessages reads every `<locale>.yaml|.yml|.json` file in dir (E9, A6).
// Nothing is discovered: the host names the directory.
func LoadMessages(fsys fs.FS, dir string, opts ...LoadOption) (Messages, error) {
	var lo loadOptions
	for _, o := range opts {
		o(&lo)
	}
	dir = fsDir(dir)
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("brick: LoadMessages: %w", err)
	}
	out := Messages{}
	for _, en := range entries {
		if en.IsDir() || strings.HasPrefix(en.Name(), ".") {
			continue
		}
		ext := pathExt(en.Name())
		tag, ok := normalizeLocale(strings.TrimSuffix(en.Name(), ext))
		if !ok {
			return nil, fmt.Errorf("brick: LoadMessages: %s: the file name must be a locale tag, e.g. en.yaml", en.Name())
		}
		p := path.Join(dir, en.Name())
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, fmt.Errorf("brick: LoadMessages: %w", err)
		}
		v, err := parseData(p, b, lo.formats)
		if err != nil {
			return nil, fmt.Errorf("brick: LoadMessages: %w", err)
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("brick: LoadMessages: %s must be a map of keys to messages", p)
		}
		if _, dup := out[tag]; dup {
			return nil, fmt.Errorf("brick: LoadMessages: two files for locale %s", tag)
		}
		out[tag] = m
	}
	if err := out.validate(); err != nil {
		return nil, err
	}
	return out, nil
}

// validate enforces E9: a plural map has `other`, and a key is the same
// shape (string or plural) in every locale.
func (m Messages) validate() error {
	shapes := map[string]string{}
	for _, tag := range sortedKeys(m) {
		var walk func(prefix string, v map[string]any) error
		walk = func(prefix string, v map[string]any) error {
			for _, k := range sortedKeys(v) {
				key := prefix + k
				shape := ""
				switch x := v[k].(type) {
				case map[string]any:
					if isPlural(x) {
						if _, ok := x[formOther]; !ok {
							return fmt.Errorf("brick: messages %s: %s is a plural message without %q (E9)", tag, key, formOther)
						}
						shape = "plural"
					} else {
						if err := walk(key+".", x); err != nil {
							return err
						}
						continue
					}
				default:
					shape = "string"
				}
				if prev, ok := shapes[key]; ok && prev != shape {
					return fmt.Errorf("brick: messages: %s is a %s in one locale and a %s in %s (E9)", key, prev, shape, tag)
				}
				shapes[key] = shape
			}
			return nil
		}
		if err := walk("", m[tag]); err != nil {
			return err
		}
	}
	return nil
}

// isPlural reports whether every key of a map is a plural form.
func isPlural(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	for k := range m {
		if !slices.Contains(pluralForms, k) {
			return false
		}
	}
	return true
}

// lookup finds a dotted key in one catalog.
func (m Messages) lookup(tag, key string) (any, bool) {
	cur := any(m[tag])
	for part := range strings.SplitSeq(key, ".") {
		mp, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = mp[part]
		if !ok {
			return nil, false
		}
	}
	return cur, cur != nil
}

// activeLocale is the locale for t, dateFormat and numberFormat: the nearest
// provided `locale`, else the render's locale, else DefaultLocale.
func (st *renderState) activeLocale(s *Scope) string {
	if v, ok := s.Ctx()[ctxLocale]; ok {
		if tag, ok := normalizeLocale(fmt.Sprint(v)); ok {
			return tag
		}
	}
	if st.locale != "" {
		return st.locale
	}
	return st.e.opts.DefaultLocale
}

// placeholderRE matches `{name}` in a message.
var placeholderRE = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// translate is `t "key"` / `t "key" (dict …)`. The chain is the exact
// locale, its language, then DefaultLocale; a key missing everywhere renders
// as itself with a warning — visible, never blank.
func (st *renderState) translate(s *Scope, key string, args ...any) (string, error) {
	if len(args) > 1 {
		return "", errors.New("t takes a key and at most one dict")
	}
	if st.dry {
		return key, nil
	}
	vars := map[string]any{}
	if len(args) == 1 && args[0] != nil {
		m, err := toStringMap(args[0])
		if err != nil {
			return "", fmt.Errorf("t %q: %w", key, err)
		}
		vars = m
	}
	loc := st.activeLocale(s)
	chain := localeChain(loc)
	if d := st.e.opts.DefaultLocale; d != "" && !slices.Contains(chain, d) {
		chain = append(chain, d)
	}
	msgs := st.e.opts.Messages
	var msg any
	var found string
	for _, tag := range chain {
		if v, ok := msgs.lookup(tag, key); ok {
			msg, found = v, tag
			break
		}
	}
	if msg == nil {
		where := strings.Join(chain, ", ")
		if len(chain) == 0 {
			where = "every locale: no locale is set (Options.DefaultLocale, brick.WithLocale, or {{ provide \"locale\" … }})"
		}
		st.warn(Diagnostic{File: s.file.path, Code: CodeI18n, Message: fmt.Sprintf("translation %q is missing for %s", key, where)})
		return key, nil
	}
	text, err := pickForm(msg, vars, found, key)
	if err != nil {
		return "", err
	}
	return placeholderRE.ReplaceAllStringFunc(text, func(ph string) string {
		name := ph[1 : len(ph)-1]
		v, ok := vars[name]
		if !ok {
			st.warn(Diagnostic{File: s.file.path, Code: CodeI18n, Message: fmt.Sprintf("translation %q: no value for %s", key, ph)})
			return ph
		}
		if v == nil {
			return "" // a missing value renders empty, like a missing key
		}
		return fmt.Sprint(v)
	}), nil
}

// pickForm resolves a plural message by `count` (E9: count is coerced).
func pickForm(msg any, vars map[string]any, tag, key string) (string, error) {
	switch m := msg.(type) {
	case string:
		return m, nil
	case map[string]any:
		if !isPlural(m) {
			return "", fmt.Errorf("t %q names a group of messages, not one message", key)
		}
		form := formOther
		if c, ok := vars["count"]; ok {
			n, err := toFloat(c)
			if err != nil {
				return "", fmt.Errorf("t %q: count: %w", key, err)
			}
			form = pluralCategory(tag, n)
		}
		v, ok := m[form]
		if !ok {
			v = m[formOther]
		}
		return fmt.Sprint(v), nil
	}
	return fmt.Sprint(msg), nil
}

// pluralCategory is the CLDR cardinal rule for integers (and a fallback for
// fractions: "other"), for the languages whose rules differ from English.
func pluralCategory(tag string, n float64) string {
	if n != float64(int64(n)) {
		return formOther
	}
	i := int64(n)
	if i < 0 {
		i = -i
	}
	lang, _, _ := strings.Cut(tag, "-")
	mod10, mod100 := i%10, i%100
	switch lang {
	case "ja", "zh", "ko", "th", "vi", "id", "ms", "tr":
		return formOther
	case "fr", "hi", "fa", "bn":
		if i == 0 || i == 1 {
			return "one"
		}
	case "pt":
		if tag == "pt-PT" {
			if i == 1 {
				return "one"
			}
			return formOther
		}
		if i == 0 || i == 1 {
			return "one"
		}
	case "ru", "uk", "be":
		switch {
		case mod10 == 1 && mod100 != 11:
			return "one"
		case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
			return "few"
		default:
			return "many"
		}
	case "pl":
		switch {
		case i == 1:
			return "one"
		case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
			return "few"
		default:
			return "many"
		}
	case "cs", "sk":
		switch {
		case i == 1:
			return "one"
		case i >= 2 && i <= 4:
			return "few"
		}
	case "ar":
		switch {
		case i == 0:
			return "zero"
		case i == 1:
			return "one"
		case i == 2:
			return "two"
		case mod100 >= 3 && mod100 <= 10:
			return "few"
		case mod100 >= 11:
			return "many"
		}
	case "he":
		switch i {
		case 1:
			return "one"
		case 2:
			return "two"
		}
	default:
		if i == 1 {
			return "one"
		}
	}
	return formOther
}
