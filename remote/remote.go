// Package remote vendors brick's remote imports and remote
// component libraries. It finds every `use:` value like
// github.com/acme/kit/components/Card.html@v1.2.0 in a project and every
// remote entry of brick.yaml's `libraries:` (a whole folder with a
// brick-library.yaml), fetches each file — and the files those import —
// into brick_modules/, and pins every file's content hash in brick.lock.
// A single imported component also brings the nearest library manifest
// above it, so its ~/ imports resolve from its library root.
//
// Fetching happens only here, at install time. The engine renders from the
// vendored copies and never touches the network.
package remote

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/internal/frontmatter"
)

// LockFile is the lock's name at the project root.
const LockFile = "brick.lock"

// ConfigFile is the project's brick.yaml, whose `libraries:` Vendor reads.
const ConfigFile = "brick.yaml"

// maxFileBytes bounds one fetched file.
const maxFileBytes = 8 << 20

// Fetch returns one remote file's bytes. The default fetches GitHub files
// from raw.githubusercontent.com; a host passes its own for other hosts or
// a mirror — the package's one escape hatch. A file that does not exist is
// an error wrapping ErrNotFound (or fs.ErrNotExist), which Vendor needs to
// tell "no manifest here" from a failed download.
type Fetch func(ctx context.Context, r brick.Remote) ([]byte, error)

// List returns every file under a folder of a repository at a version, as
// repository-relative paths ("kit/components/Button.html"). dir.Path is the
// folder, "" for the repository root. Vendor calls it for a remote library
// (brick.yaml `libraries:`), which is a whole folder; the default lists
// GitHub repositories through the git-trees API (GitHubList).
type List func(ctx context.Context, dir brick.Remote) ([]string, error)

// Options configures Vendor and Check.
type Options struct {
	// Dir is the project directory.
	Dir string
	// Fetch fetches a file; nil uses GitHub.
	Fetch Fetch
	// List lists a remote library's files; nil uses GitHubList.
	List List
	// Client is used by the default fetcher and lister; nil uses
	// http.DefaultClient.
	Client *http.Client
}

// Report says what Vendor did.
type Report struct {
	// Files are every vendored file, by spec, sorted.
	Files []string
	// Fetched counts files downloaded this run (the rest were current).
	Fetched int
}

// ErrMismatch is returned by Check when the project and the lock differ.
var ErrMismatch = errors.New("remote: brick_modules does not match brick.lock; run `brick vendor`")

// ErrNotFound is what the built-in fetcher wraps for a file the host does
// not have (HTTP 404). Vendor treats it as "absent" only where absence is
// allowed: a library manifest probed above a single imported file.
var ErrNotFound = errors.New("remote: not found")

// Vendor fetches every remote import the project needs, transitively — the
// `use:` imports of its files and every remote library brick.yaml lists —
// and rewrites brick.lock. A file already vendored with the locked hash is
// not fetched again, and a library whose files are all locked and current
// is not listed again, so re-vendoring an unchanged project makes no
// requests.
func Vendor(ctx context.Context, o Options) (Report, error) {
	var rep Report
	lock, err := readLock(o.Dir)
	if err != nil {
		return rep, err
	}
	fetch := o.Fetch
	if fetch == nil {
		fetch = GitHub(o.Client)
	}
	list := o.List
	if list == nil {
		list = GitHubList(o.Client)
	}
	// A module (repository at a version) with files in the old lock was
	// probed for manifests when it was vendored: versions are immutable, so
	// a manifest it has is locked and one it lacks does not exist.
	lockedModules := map[string]bool{}
	for spec := range lock {
		if r, ok := brick.ParseRemote(spec); ok {
			lockedModules[r.ModuleRoot()] = true
		}
	}
	next := map[string]string{}
	current := func(r brick.Remote) ([]byte, bool) {
		want, ok := lock[r.String()]
		if !ok {
			return nil, false
		}
		b, err := os.ReadFile(filepath.Join(o.Dir, filepath.FromSlash(r.VendorPath())))
		return b, err == nil && sum(b) == want
	}
	w := newWalker(func(r brick.Remote, optional bool) ([]byte, error) {
		if b, ok := current(r); ok {
			next[r.String()] = sum(b)
			return b, nil
		}
		if optional && lockedModules[r.ModuleRoot()] {
			return nil, nil
		}
		b, err := fetch(ctx, r)
		if err != nil {
			if optional && (errors.Is(err, ErrNotFound) || errors.Is(err, fs.ErrNotExist)) {
				return nil, nil
			}
			return nil, fmt.Errorf("remote: fetch %s: %w", r, err)
		}
		dst := filepath.Join(o.Dir, filepath.FromSlash(r.VendorPath()))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return nil, err
		}
		rep.Fetched++
		next[r.String()] = sum(b)
		return b, nil
	})
	libs, err := libraries(o.Dir)
	if err != nil {
		return rep, err
	}
	for _, lib := range libs {
		files, complete := lockedFiles(lock, lib)
		for _, f := range files {
			if _, ok := current(f); !ok {
				complete = false
				break
			}
		}
		if !complete {
			paths, err := list(ctx, lib)
			if err != nil {
				return rep, fmt.Errorf("remote: list library %s: %w", libSpec(lib), err)
			}
			files = files[:0]
			for _, p := range paths {
				if within(p, lib.Path) && !strings.Contains(p, "..") && vendorable(p) {
					files = append(files, brick.Remote{Host: lib.Host, Owner: lib.Owner, Repo: lib.Repo, Path: p, Version: lib.Version})
				}
			}
		}
		if err := w.addLibrary(lib, files); err != nil {
			return rep, err
		}
	}
	if err := w.addProject(o.Dir); err != nil {
		return rep, err
	}
	if err := w.run(); err != nil {
		return rep, err
	}
	rep.Files = sortedKeys(next)
	return rep, writeLock(o.Dir, next)
}

// Check verifies, offline, that every remote import the project needs is
// vendored with the hash brick.lock records, that every remote library in
// brick.yaml is vendored whole (no file missing, changed or added), and
// that the lock lists nothing else. It is the CI gate.
func Check(o Options) error {
	lock, err := readLock(o.Dir)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var problems []string
	w := newWalker(func(r brick.Remote, optional bool) ([]byte, error) {
		want, locked := lock[r.String()]
		if optional && !locked {
			return nil, nil // a manifest the repository does not have
		}
		seen[r.String()] = true
		b, err := os.ReadFile(filepath.Join(o.Dir, filepath.FromSlash(r.VendorPath())))
		switch {
		case err != nil:
			problems = append(problems, r.String()+": not vendored")
			return nil, nil
		case !locked:
			problems = append(problems, r.String()+": not in brick.lock")
		case sum(b) != want:
			problems = append(problems, r.String()+": vendored file does not match brick.lock")
		}
		return b, nil
	})
	libs, err := libraries(o.Dir)
	if err != nil {
		return err
	}
	for _, lib := range libs {
		files, _ := lockedFiles(lock, lib)
		manifest := libManifest(lib)
		if _, ok := lock[manifest.String()]; !ok {
			problems = append(problems, libSpec(lib)+": library not vendored")
			continue
		}
		// A file added under a vendored library, not in the lock.
		root := filepath.Join(o.Dir, filepath.FromSlash(manifest.ModuleRoot()))
		_ = filepath.WalkDir(filepath.Join(root, filepath.FromSlash(lib.Path)), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return nil
			}
			f := brick.Remote{Host: lib.Host, Owner: lib.Owner, Repo: lib.Repo, Path: filepath.ToSlash(rel), Version: lib.Version}
			if _, ok := lock[f.String()]; !ok {
				problems = append(problems, f.String()+": in a vendored library but not in brick.lock")
			}
			return nil
		})
		if err := w.addLibrary(lib, files); err != nil {
			return err
		}
	}
	if err := w.addProject(o.Dir); err != nil {
		return err
	}
	if err := w.run(); err != nil {
		return err
	}
	for spec := range lock {
		if !seen[spec] {
			problems = append(problems, spec+": in brick.lock but no longer imported")
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%w:\n  %s", ErrMismatch, strings.Join(problems, "\n  "))
	}
	return nil
}

// libraries returns brick.yaml's remote libraries (read with
// brick.LoadConfig, the engine's own reader), each as a Remote whose Path
// is the library folder ("" for a repository root). ~/ folders and names a
// host offers are not remote and are skipped. No brick.yaml means none.
func libraries(dir string) ([]brick.Remote, error) {
	if _, err := os.Stat(filepath.Join(dir, ConfigFile)); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	cfg, err := brick.LoadConfig(os.DirFS(dir), ConfigFile)
	if err != nil {
		return nil, err
	}
	var out []brick.Remote
	for _, l := range cfg.Libraries {
		if strings.HasPrefix(l.From, "~/") || !strings.Contains(l.From, "@") {
			continue
		}
		r, ok := libraryRoot(l.From)
		if !ok {
			return nil, fmt.Errorf("remote: %s: libraries: %q is not a remote library (host/owner/repo[/path]@version)", ConfigFile, l.From)
		}
		out = append(out, r)
	}
	return out, nil
}

// libraryRoot parses a remote library spec — a repository root
// "github.com/acme/ui@v1.2.0" or a folder "github.com/acme/mono/ui@v1.2.0".
// It parses the spec of the library's manifest with brick.ParseRemote, so
// the grammar stays the engine's, then keeps the folder.
func libraryRoot(spec string) (brick.Remote, bool) {
	at := strings.LastIndex(spec, "@")
	if at < 0 {
		return brick.Remote{}, false
	}
	r, ok := brick.ParseRemote(spec[:at] + "/" + brick.LibraryManifest + spec[at:])
	if !ok {
		return brick.Remote{}, false
	}
	r.Path = strings.TrimSuffix(path.Dir(r.Path), ".")
	return r, true
}

// libManifest is a library folder's manifest file.
func libManifest(lib brick.Remote) brick.Remote {
	lib.Path = path.Join(lib.Path, brick.LibraryManifest)
	return lib
}

// libSpec is a library as brick.yaml writes it.
func libSpec(lib brick.Remote) string {
	return strings.TrimSuffix(lib.Host+"/"+lib.Owner+"/"+lib.Repo+"/"+lib.Path, "/") + "@" + lib.Version
}

// lockedFiles returns the lock's files under a library folder, and whether
// the lock holds the library at all (its manifest).
func lockedFiles(lock map[string]string, lib brick.Remote) ([]brick.Remote, bool) {
	var out []brick.Remote
	for spec := range lock {
		if r, ok := brick.ParseRemote(spec); ok && r.ModuleRoot() == lib.ModuleRoot() && within(r.Path, lib.Path) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	_, ok := lock[libManifest(lib).String()]
	return out, ok
}

// within reports whether a repository path is inside folder dir ("" is
// the repository root).
// goSourceExt marks Go source files, which a library folder may hold for
// its other distribution path (a package that embeds the folder and
// exports Library()).
const goSourceExt = ".go"

// vendorable reports whether a listed library file is copied into
// brick_modules. Go sources are left out: brick never reads them, and a
// copy under brick_modules/<repo>@<version>/ would sit inside the host's
// Go module in a directory that is not a valid import path, breaking its
// `go build ./...`.
func vendorable(p string) bool {
	return path.Ext(p) != goSourceExt
}

func within(p, dir string) bool {
	return dir == "" || strings.HasPrefix(p, dir+"/")
}

// walker visits every remote file the project needs: its libraries'
// files, its own imports, and — transitively — each visited file's remote
// imports and ~/ paths. It resolves ~/ exactly as the engine does
// (brick's baseOf): the root of the library a file belongs to, else the
// nearest folder above it holding a brick-library.yaml, else the
// repository root. Finding that nearest manifest for a single imported
// file is what vendors it, so the component's ~/ imports resolve.
type walker struct {
	// get returns a file's bytes (fetched or read). optional marks a
	// manifest probe, for which a missing file is (nil, nil).
	get func(r brick.Remote, optional bool) ([]byte, error)
	// bases are the known library roots per module (ModuleRoot).
	bases map[string][]string
	// probed caches manifest probes by module and folder.
	probed map[string]bool
	done   map[string]bool
	queue  []brick.Remote
}

func newWalker(get func(brick.Remote, bool) ([]byte, error)) *walker {
	return &walker{get: get, bases: map[string][]string{}, probed: map[string]bool{}, done: map[string]bool{}}
}

// addLibrary queues a library's files and records its root.
func (w *walker) addLibrary(lib brick.Remote, files []brick.Remote) error {
	manifest := libManifest(lib).String()
	if !slices.ContainsFunc(files, func(f brick.Remote) bool { return f.String() == manifest }) {
		return fmt.Errorf("remote: library %s has no %s at its root", libSpec(lib), brick.LibraryManifest)
	}
	w.bases[lib.ModuleRoot()] = append(w.bases[lib.ModuleRoot()], lib.Path)
	w.queue = append(w.queue, files...)
	return nil
}

// addProject queues the remote imports in the project's own files.
func (w *walker) addProject(dir string) error {
	return fs.WalkDir(os.DirFS(dir), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && (p == brick.ModulesDir || strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		ext := path.Ext(p)
		if ext != ".html" && ext != ".star" {
			return nil
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			return err
		}
		for _, spec := range refs(b, ext) {
			if r, ok := brick.ParseRemote(spec); ok {
				w.queue = append(w.queue, r)
			}
		}
		return nil
	})
}

// run visits the queue until it is empty.
func (w *walker) run() error {
	for len(w.queue) > 0 {
		r := w.queue[0]
		w.queue = w.queue[1:]
		if w.done[r.String()] {
			continue
		}
		w.done[r.String()] = true
		b, err := w.get(r, false)
		if err != nil {
			return err
		}
		ext := path.Ext(r.Path)
		if b == nil || (ext != ".html" && ext != ".star") {
			continue
		}
		base, err := w.base(r)
		if err != nil {
			return err
		}
		for _, spec := range refs(b, ext) {
			if sub, ok := brick.ParseRemote(spec); ok {
				w.queue = append(w.queue, sub)
				continue
			}
			// A ~/ path inside a module is a file of the same module, from
			// the file's library root.
			if rest, ok := strings.CutPrefix(spec, "~/"); ok && !strings.Contains(rest, "..") {
				w.queue = append(w.queue, brick.Remote{Host: r.Host, Owner: r.Owner, Repo: r.Repo, Path: path.Join(base, rest), Version: r.Version})
			}
		}
	}
	return nil
}

// base is what ~/ means inside remote file r (see walker).
func (w *walker) base(r brick.Remote) (string, error) {
	best, found := "", false
	for _, d := range w.bases[r.ModuleRoot()] {
		if within(r.Path, d) && (!found || len(d) > len(best)) {
			best, found = d, true
		}
	}
	if found {
		return best, nil
	}
	for dir := path.Dir(r.Path); ; dir = path.Dir(dir) {
		dir = strings.TrimSuffix(dir, ".")
		ok, err := w.probe(r, dir)
		if err != nil || ok {
			return dir, err
		}
		if dir == "" {
			return "", nil
		}
	}
}

// probe reports whether folder dir of r's module holds a library manifest,
// fetching (or reading) it once; a manifest found is a library root.
func (w *walker) probe(r brick.Remote, dir string) (bool, error) {
	key := r.ModuleRoot() + "\x00" + dir
	if v, ok := w.probed[key]; ok {
		return v, nil
	}
	m := brick.Remote{Host: r.Host, Owner: r.Owner, Repo: r.Repo, Path: path.Join(dir, brick.LibraryManifest), Version: r.Version}
	b, err := w.get(m, true)
	if err != nil {
		return false, err
	}
	found := b != nil
	w.probed[key] = found
	if found {
		w.done[m.String()] = true
		w.bases[r.ModuleRoot()] = append(w.bases[r.ModuleRoot()], dir)
	}
	return found, nil
}

// Patterns for the paths a file imports.
var (
	loadRE = regexp.MustCompile(`\bload\(\s*"([^"]+)"`)
	srcRE  = regexp.MustCompile(`<(?:[A-Z][A-Za-z0-9]*|x-[a-z0-9-]+)\b[^>]*?\ssrc="([^"]+)"`)
	// commentRE is a template comment, {{/* … */}} (with optional trim
	// markers). A component written inside one is text to the engine, so
	// its src= is not an import — kit doc comments show usage examples.
	commentRE = regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)
)

// refs lists the paths a file imports: use: values and component src= for
// .html; load() paths for .star.
func refs(b []byte, ext string) []string {
	var out []string
	if ext == ".star" {
		for _, m := range loadRE.FindAllSubmatch(b, -1) {
			out = append(out, string(m[1]))
		}
		return out
	}
	doc, err := frontmatter.Parse(b)
	if err == nil {
		if use, ok := doc.Params["use"].(map[string]any); ok {
			for _, v := range use {
				if s, ok := v.(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	for _, m := range srcRE.FindAllSubmatch(commentRE.ReplaceAll(b, nil), -1) {
		out = append(out, string(m[1]))
	}
	slices.Sort(out)
	return out
}

// GitHub fetches from raw.githubusercontent.com.
func GitHub(c *http.Client) Fetch {
	if c == nil {
		c = http.DefaultClient
	}
	return func(ctx context.Context, r brick.Remote) ([]byte, error) {
		if r.Host != githubHost {
			return nil, fmt.Errorf("only github.com is built in; pass remote.Options.Fetch for %s", r.Host)
		}
		url := "https://raw.githubusercontent.com/" + r.Owner + "/" + r.Repo + "/" + r.Version + "/" + r.Path
		return Get(ctx, c, url)
	}
}

// githubAPI is the GitHub REST API root the lister calls.
const githubAPI = "https://api.github.com"

// githubHost is the one host whose fetch and listing are built in; any
// other host needs Options.Fetch or Options.List.
const githubHost = "github.com"

// GitHubList lists a folder of a GitHub repository with the git-trees API
// (GET /repos/{owner}/{repo}/git/trees/{version}?recursive=1), keeping the
// files under the folder. Unauthenticated requests are rate-limited by
// GitHub; a host that needs more passes a Client whose transport adds a
// token, or its own List. A truncated tree (a very large repository) is an
// error rather than a partial library.
func GitHubList(c *http.Client) List {
	return gitHubList(c, githubAPI)
}

// gitHubList is GitHubList against an API root (a test server).
func gitHubList(c *http.Client, api string) List {
	if c == nil {
		c = http.DefaultClient
	}
	return func(ctx context.Context, dir brick.Remote) ([]string, error) {
		if dir.Host != githubHost {
			return nil, fmt.Errorf("only github.com listing is built in; pass remote.Options.List for %s", dir.Host)
		}
		url := api + "/repos/" + dir.Owner + "/" + dir.Repo + "/git/trees/" + dir.Version + "?recursive=1"
		b, err := Get(ctx, c, url)
		if err != nil {
			return nil, fmt.Errorf("GitHub trees API (a rate limit or a missing tag?): %w", err)
		}
		var tree struct {
			Tree []struct {
				Path string `json:"path"`
				Type string `json:"type"`
			} `json:"tree"`
			Truncated bool `json:"truncated"`
		}
		if err := json.Unmarshal(b, &tree); err != nil {
			return nil, fmt.Errorf("GitHub trees API: %s: %w", url, err)
		}
		if tree.Truncated {
			return nil, fmt.Errorf("GitHub trees API: the tree of %s/%s@%s is too large to list in one response; pass remote.Options.List", dir.Owner, dir.Repo, dir.Version)
		}
		var out []string
		for _, e := range tree.Tree {
			if e.Type == gitBlob && within(e.Path, dir.Path) {
				out = append(out, e.Path)
			}
		}
		return out, nil
	}
}

// gitBlob is the git-trees entry type of a file (folders are "tree").
const gitBlob = "blob"

// Get downloads url, bounded to 8 MB. A 404 wraps ErrNotFound.
func Get(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", url, ErrNotFound)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	if len(b) > maxFileBytes {
		return nil, fmt.Errorf("%s is over %d bytes", url, maxFileBytes)
	}
	return b, nil
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256-" + hex.EncodeToString(h[:])
}

// readLock reads brick.lock: "<spec> <hash>" per line. Missing is empty.
func readLock(dir string) (map[string]string, error) {
	out := map[string]string{}
	b, err := os.ReadFile(filepath.Join(dir, LockFile))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		spec, hash, ok := strings.Cut(line, " ")
		if !ok || !strings.HasPrefix(hash, "sha256-") {
			return nil, fmt.Errorf("remote: %s:%d: want \"<import> sha256-<hex>\"", LockFile, n)
		}
		out[spec] = strings.TrimSpace(hash)
	}
	return out, sc.Err()
}

// writeLock writes the lock sorted, so it diffs cleanly.
func writeLock(dir string, m map[string]string) error {
	var b strings.Builder
	b.WriteString("# brick.lock — written by `brick vendor`; commit it with brick_modules/.\n")
	for _, k := range sortedKeys(m) {
		b.WriteString(k + " " + m[k] + "\n")
	}
	return os.WriteFile(filepath.Join(dir, LockFile), []byte(b.String()), 0o644)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
