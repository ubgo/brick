package brick

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
)

// rootPrefix starts every path a file writes (X7): `~/` is the root of
// Options.FS, never $HOME.
const rootPrefix = "~/"

// errNoFS is returned for any import when Options.FS is nil (A1).
var errNoFS = errors.New("no Options.FS is set, so no file can be read")

// resolvePath turns an author-written path into an FS path (X2, X7).
//
// Invariant: the result is a valid fs.FS path inside the root. `./`, `../`,
// absolute paths, backslashes and anything climbing out are errors, so there
// is exactly one way to write a path and nothing to trick.
func resolvePath(p string) (string, error) {
	switch {
	case p == "":
		return "", errors.New("empty path")
	case strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || p == "." || p == "..":
		return "", fmt.Errorf("path %q: start from the project root, e.g. ~/components/Card.html (X7)", p)
	case !strings.HasPrefix(p, rootPrefix):
		return "", fmt.Errorf("path %q must start with ~/ — the project root (X7)", p)
	case strings.Contains(p, `\`):
		return "", fmt.Errorf("path %q: use forward slashes", p)
	}
	rest := strings.TrimPrefix(p, rootPrefix)
	for seg := range strings.SplitSeq(rest, "/") {
		if seg == ".." || seg == "." {
			return "", fmt.Errorf("path %q: %q segments are not allowed (X7)", p, seg)
		}
	}
	clean := path.Clean(rest)
	if !fs.ValidPath(clean) || clean == "." {
		return "", fmt.Errorf("path %q is not a valid path inside the project", p)
	}
	return clean, nil
}

// checkDir validates a configured directory (a root or the layout dir),
// which may be written with or without `~/`.
func checkDir(d string) error {
	d = strings.TrimPrefix(d, rootPrefix)
	d = strings.TrimSuffix(d, "/")
	if d == "" || !fs.ValidPath(d) {
		return fmt.Errorf("%q is not a directory path inside Options.FS", d)
	}
	return nil
}

// fsDir returns a configured directory as an FS path.
func fsDir(d string) string {
	return strings.TrimSuffix(strings.TrimPrefix(d, rootPrefix), "/")
}

// displayPath is how a path appears in messages: from the project root.
func displayPath(p string) string { return rootPrefix + p }

// readFile reads a file, enforcing MaxFileBytes (E4) and checking the name
// case-exactly against its directory listing (S6), so macOS and Linux agree.
func (e *Engine) readFile(p string) ([]byte, error) {
	if e.opts.FS == nil {
		return nil, errNoFS
	}
	if ok, err := e.exists(p); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("%s: %w", displayPath(p), fs.ErrNotExist)
	}
	b, err := fs.ReadFile(e.opts.FS, p)
	if err != nil {
		return nil, err
	}
	if len(b) > e.opts.MaxFileBytes {
		return nil, fmt.Errorf("%s is %d bytes, over MaxFileBytes (%d)", displayPath(p), len(b), e.opts.MaxFileBytes)
	}
	return b, nil
}

// exists reports whether p is a regular file, by exact name in the listing.
func (e *Engine) exists(p string) (bool, error) {
	if e.opts.FS == nil {
		return false, errNoFS
	}
	entries, err := fs.ReadDir(e.opts.FS, path.Dir(p))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	base := path.Base(p)
	for _, en := range entries {
		if en.Name() == base {
			return !en.IsDir(), nil
		}
	}
	return false, nil
}

// variant picks the language variant of p for locale (C16): the exact tag
// (`about.pt-BR.html`), then the language (`about.pt.html`), then p itself.
func (e *Engine) variant(p, locale string) string {
	if locale == "" || e.opts.FS == nil {
		return p
	}
	ext := path.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	for _, tag := range localeChain(locale) {
		cand := stem + "." + tag + ext
		if ok, _ := e.exists(cand); ok {
			return cand
		}
	}
	return p
}

// ModulesDir holds vendored remote imports. Not `vendor/`,
// which is special to Go modules.
const ModulesDir = "brick_modules"

// remoteRE matches host/owner/repo/path@version.
var remoteRE = regexp.MustCompile(`^([a-z0-9-]+(?:\.[a-z0-9-]+)+)/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/([A-Za-z0-9_./-]+)@([A-Za-z0-9_.+-]+)$`)

// Remote is a remote import: github.com/acme/kit/components/Card.html@v1.2.0.
type Remote struct {
	Host, Owner, Repo, Path, Version string
}

// ParseRemote reads a remote import; ok is false for anything else,
// including every ~/ path.
func ParseRemote(spec string) (Remote, bool) {
	m := remoteRE.FindStringSubmatch(spec)
	if m == nil || strings.Contains(m[4], "..") {
		return Remote{}, false
	}
	return Remote{Host: m[1], Owner: m[2], Repo: m[3], Path: m[4], Version: m[5]}, true
}

// String is the spec as written.
func (r Remote) String() string {
	return r.Host + "/" + r.Owner + "/" + r.Repo + "/" + r.Path + "@" + r.Version
}

// ModuleRoot is the vendored module's directory: brick_modules/host/owner/repo@version.
func (r Remote) ModuleRoot() string {
	return path.Join(ModulesDir, r.Host, r.Owner, r.Repo+"@"+r.Version)
}

// VendorPath is where the file is vendored.
func (r Remote) VendorPath() string { return path.Join(r.ModuleRoot(), r.Path) }

// moduleRoot returns the vendored module a file belongs to, or "" for a
// project file.
func moduleRoot(file string) string {
	rest, ok := strings.CutPrefix(file, ModulesDir+"/")
	if !ok {
		return ""
	}
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 4 || !strings.Contains(parts[2], "@") {
		return ""
	}
	return path.Join(ModulesDir, parts[0], parts[1], parts[2])
}

// resolveFrom resolves a path written in file: a remote import maps to its vendored copy; `~/`
// means the project root, except inside a library or a vendored module,
// where it means that library's root (baseOf) — so a library is
// self-contained and can never reach into the project that uses it.
func (e *Engine) resolveFrom(file, p string) (string, error) {
	if r, ok := ParseRemote(p); ok {
		return r.VendorPath(), nil
	}
	rp, err := resolvePath(p)
	if err != nil {
		return "", err
	}
	if base := e.baseOf(file); base != "" {
		return path.Join(base, rp), nil
	}
	return rp, nil
}
