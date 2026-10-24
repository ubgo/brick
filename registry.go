package brick

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// htmlExt is the extension of every page, layout and component file.
const htmlExt = ".html"

// registry maps component names to files in the configured roots (S6).
//
// Why a listing: names come from walking directories, never from opening a
// guessed file name, so a case-insensitive filesystem (macOS) and a
// case-sensitive one (Linux) resolve exactly the same way.
type registry struct {
	// byName maps an exact stem (`PostList`) to its entry.
	byName map[string]*regEntry
	// byFold maps a folded stem (`postlist`) to the stem that wins it, for
	// the hyphen form (`x-post-list`).
	byFold map[string]string
}

// regEntry is one component name and its shared file. Language variants
// (`Card.fr.html`) are not separate components; they are chosen beside the
// shared file at load time (C16).
type regEntry struct {
	name string
	path string
}

// buildRegistry walks every root in order; the earlier root wins a name.
func (e *Engine) buildRegistry() (*registry, error) {
	r := &registry{byName: map[string]*regEntry{}, byFold: map[string]string{}}
	if e.opts.FS == nil {
		return r, nil
	}
	for _, root := range e.opts.Roots {
		dir := fsDir(root)
		type found struct{ stem, locale, path string }
		var files []found
		err := fs.WalkDir(e.opts.FS, dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, htmlExt) {
				return nil
			}
			stem, locale, ok := splitStem(strings.TrimSuffix(path.Base(p), htmlExt))
			if !ok {
				e.warn(Diagnostic{File: p, Code: CodeShadow, Message: fmt.Sprintf("%s is not a component name (names start with an uppercase letter and use only letters and digits); skipped", path.Base(p))})
				return nil
			}
			files = append(files, found{stem, locale, p})
			return nil
		})
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("walking root %s: %w", displayPath(dir), err)
		}
		// Within one root the lexically first path wins (S6).
		slices.SortFunc(files, func(a, b found) int { return strings.Compare(a.path, b.path) })
		added := map[string]bool{}
		for _, f := range files {
			if f.locale != "" {
				continue
			}
			if en, exists := r.byName[f.stem]; exists {
				where := "an earlier root wins; reach this one with use:"
				if added[f.stem] {
					where = "in the same root, the lexically first path wins"
				}
				e.warn(Diagnostic{File: f.path, Code: CodeShadow, Message: fmt.Sprintf("<%s> is shadowed by %s (%s)", f.stem, displayPath(en.path), where)})
				continue
			}
			r.byName[f.stem] = &regEntry{name: f.stem, path: f.path}
			added[f.stem] = true
		}
	}
	// Libraries register after every project root, in declared order: the
	// project always wins a name, an earlier library wins over a later one,
	// and a library's prefix and only-list shape what it adds.
	libs, err := e.libraries()
	if err != nil {
		return nil, err
	}
	for _, l := range libs {
		if err := e.registerLibrary(r, l); err != nil {
			return nil, err
		}
	}
	names := sortedKeys(r.byName)
	for _, n := range names {
		k := strings.ToLower(n)
		if prev, ok := r.byFold[k]; ok {
			e.warn(Diagnostic{File: r.byName[n].path, Code: CodeShadow, Message: fmt.Sprintf("<%s> and <%s> are the same in the hyphen form; x- tags reach <%s>", prev, n, prev)})
			continue
		}
		r.byFold[k] = n
	}
	return r, nil
}

// registerLibrary adds one library's components to the registry, under
// its prefix and limited to its only-list. A name already taken — by the
// project or an earlier library — stays with its holder, and the shadowing
// is logged. A name in the only-list that the library does not have is an
// error, never a silently smaller library.
func (e *Engine) registerLibrary(r *registry, l *library) error {
	// Like a project root, subfolders do not change a name, and within the
	// library the lexically first path wins (S6); WalkDir visits in that
	// order.
	have := map[string]string{}
	err := fs.WalkDir(e.opts.FS, l.compDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, htmlExt) {
			return nil
		}
		stem, locale, ok := splitStem(strings.TrimSuffix(path.Base(p), htmlExt))
		if !ok || locale != "" {
			return nil
		}
		if _, dup := have[stem]; !dup {
			have[stem] = p
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("library %s: components %s: %w", l.name, displayPath(l.compDir), err)
	}
	for _, want := range l.only {
		if _, ok := have[want]; !ok {
			return fmt.Errorf("library %s: only: it has no component %q", l.name, want)
		}
	}
	for _, stem := range sortedKeys(have) {
		if len(l.only) > 0 && !slices.Contains(l.only, stem) {
			continue
		}
		name := l.prefix + stem
		if en, taken := r.byName[name]; taken {
			e.warn(Diagnostic{File: have[stem], Code: CodeShadow, Message: fmt.Sprintf("<%s> from library %s is shadowed by %s (the project and earlier libraries win; use a prefix to keep both)", name, l.name, displayPath(en.path))})
			continue
		}
		r.byName[name] = &regEntry{name: name, path: have[stem]}
	}
	return nil
}

// registryOnce builds the registry on first use.
func (e *Engine) reg() (*registry, error) {
	e.regOnce.Do(func() { e.registry, e.regErr = e.buildRegistry() })
	return e.registry, e.regErr
}

// lookup resolves a written tag name to a registry entry.
func (r *registry) lookup(tag string) (*regEntry, bool) {
	if rest, ok := strings.CutPrefix(tag, "x-"); ok {
		n, ok := r.byFold[foldHyphen(rest)]
		if !ok {
			return nil, false
		}
		return r.byName[n], true
	}
	en, ok := r.byName[tag]
	return en, ok
}

// foldHyphen turns `post-list` into the fold key `postlist`.
func foldHyphen(s string) string { return strings.ToLower(strings.ReplaceAll(s, "-", "")) }

// canonicalName turns a written tag into its component name: `x-post-list`
// → `PostList`; a capitalized tag is returned as written.
func canonicalName(tag string) string {
	rest, ok := strings.CutPrefix(tag, "x-")
	if !ok {
		return tag
	}
	var b strings.Builder
	for part := range strings.SplitSeq(rest, "-") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// splitStem splits `Card.pt-BR` into ("Card", "pt-BR"); ok is false for a
// stem that is not a component name (S6).
func splitStem(s string) (stem, locale string, ok bool) {
	stem, suffix, hasSuffix := strings.Cut(s, ".")
	if !isComponentStem(stem) {
		return "", "", false
	}
	if !hasSuffix {
		return stem, "", true
	}
	tag, valid := normalizeLocale(suffix)
	if !valid {
		return "", "", false
	}
	return stem, tag, true
}

// isComponentStem matches `[A-Z][A-Za-z0-9]*`.
func isComponentStem(s string) bool {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
