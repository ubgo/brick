package brick

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Library is a set of components a project uses by name, with no import in
// each page: a kit, a team's UI library, or one
// shipped as a Go module. A library is a folder with a brick-library.yaml
// manifest at its root; its components register after the project's own
// roots, so the project always wins a name.
//
// Exactly one of Dir and FS says where the folder is:
//
//   - Dir: a folder inside Options.FS — a local library ("ui") or a
//     vendored remote one ("brick_modules/github.com/o/r@v1.2.0/kit"; see
//     RemoteLibrary).
//   - FS: an embedded library (a Go package's embed.FS), mounted read-only
//     at brick_libs/<Name>/ in the engine's filesystem.
type Library struct {
	// Name identifies the library in diagnostics and in brick.yaml's
	// `libraries:` (a bare name picks a library the host offers). Required
	// for an FS library, whose mount it names; defaults to the manifest's
	// name otherwise.
	Name string
	// Dir is the library's root folder inside Options.FS.
	Dir string
	// FS is an embedded library's root.
	FS fs.FS
	// Prefix is put in front of every component name: prefix "Acme"
	// registers DatePicker.html as <AcmeDatePicker>, so two libraries'
	// components can coexist.
	Prefix string
	// Only limits the library to these components, named as the library
	// names them (before Prefix). Empty means all.
	Only []string
}

// LibraryManifest is the file at a library's root that makes a folder a
// library and sets where its components are.
const LibraryManifest = "brick-library.yaml"

// LibraryMountDir is where embedded (FS) libraries are mounted in the
// engine's filesystem; a project cannot have a folder of that name.
const LibraryMountDir = "brick_libs"

// defaultLibraryComponents is the manifest's `components:` default.
const defaultLibraryComponents = "components"

// libraryNameRE is what a library name and an FS mount may be: a path
// segment that can never climb or hide.
var libraryNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// libraryManifest is brick-library.yaml.
type libraryManifest struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Components  string   `yaml:"components"`
	Global      []string `yaml:"global"`
}

// library is a Library resolved for the engine: its root inside the
// engine's filesystem and its manifest.
type library struct {
	name     string
	root     string // folder with the manifest, inside the engine FS
	compDir  string // root + manifest.components
	prefix   string
	only     []string
	global   []string // component file paths whose assets every user of the library gets
	manifest libraryManifest
}

// RemoteLibrary is a Library for a vendored remote library, written the
// way brick.yaml writes it: "github.com/acme/ui@v1.2.0" (the repository
// root) or "github.com/acme/mono/ui@v1.2.0" (a folder in it). `brick
// vendor` fetches it into brick_modules/.
func RemoteLibrary(spec string) (Library, error) {
	r, ok := parseRemoteDir(spec)
	if !ok {
		return Library{}, fmt.Errorf("brick: %q is not a remote library (host/owner/repo[/path]@version)", spec)
	}
	return Library{Name: spec, Dir: r.VendorPath()}, nil
}

// parseRemoteDir parses a remote library spec, whose path may be empty (a
// repository root) — unlike a remote file, which ParseRemote requires to
// have one.
func parseRemoteDir(spec string) (Remote, bool) {
	if r, ok := ParseRemote(spec); ok {
		return r, true
	}
	m := remoteRootRE.FindStringSubmatch(spec)
	if m == nil {
		return Remote{}, false
	}
	return Remote{Host: m[1], Owner: m[2], Repo: m[3], Version: m[4]}, true
}

// remoteRootRE is a remote repository root: host/owner/repo@version.
var remoteRootRE = regexp.MustCompile(`^([a-z0-9.-]+\.[a-z]{2,})/([\w.-]+)/([\w.-]+)@([\w.+-]+)$`)

// prepareLibraries validates Options.Libraries and mounts the embedded ones
// into the engine's filesystem. It reads nothing: manifests are read when
// the registry is first built.
func prepareLibraries(opts *Options) error {
	var mounts map[string]fs.FS
	seen := map[string]bool{}
	for i := range opts.Libraries {
		l := &opts.Libraries[i]
		switch {
		case l.FS != nil && l.Dir != "":
			return fmt.Errorf("brick: Options.Libraries[%d]: set Dir or FS, not both", i)
		case l.FS != nil:
			if !libraryNameRE.MatchString(l.Name) {
				return fmt.Errorf("brick: Options.Libraries[%d]: an embedded library needs a Name of lowercase letters, digits, '.', '_' and '-' (got %q)", i, l.Name)
			}
			if mounts == nil {
				mounts = map[string]fs.FS{}
			}
			if _, dup := mounts[l.Name]; dup {
				return fmt.Errorf("brick: Options.Libraries: two embedded libraries are named %q", l.Name)
			}
			mounts[l.Name] = l.FS
			l.Dir = path.Join(LibraryMountDir, l.Name)
			l.FS = nil
		case l.Dir == "":
			return fmt.Errorf("brick: Options.Libraries[%d]: set Dir or FS", i)
		default:
			if err := checkDir(l.Dir); err != nil {
				return fmt.Errorf("brick: Options.Libraries[%d]: %w", i, err)
			}
			l.Dir = fsDir(l.Dir)
		}
		if seen[l.Dir] {
			return fmt.Errorf("brick: Options.Libraries: %s is listed twice", displayPath(l.Dir))
		}
		seen[l.Dir] = true
		if l.Prefix != "" && !isComponentStem(l.Prefix) {
			return fmt.Errorf("brick: Options.Libraries[%d]: Prefix %q must be a component-name start (uppercase letter, then letters and digits)", i, l.Prefix)
		}
		l.Only = slices.Clone(l.Only)
	}
	if mounts != nil {
		base := opts.FS
		if base == nil {
			base = emptyFS{}
		}
		opts.FS = &mountFS{base: base, mounts: mounts}
	}
	return nil
}

// loadLibraries reads each library's manifest, in declared order.
func (e *Engine) loadLibraries() ([]*library, error) {
	var out []*library
	for _, l := range e.opts.Libraries {
		label := cmpOrStr(l.Name, displayPath(l.Dir))
		b, err := fs.ReadFile(e.opts.FS, path.Join(l.Dir, LibraryManifest))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("library %s: no %s in %s — a library is a folder with a manifest (or run `brick vendor` for a remote library)", label, LibraryManifest, displayPath(l.Dir))
			}
			return nil, fmt.Errorf("library %s: %w", label, err)
		}
		var m libraryManifest
		dec := yaml.NewDecoder(strings.NewReader(string(b)))
		dec.KnownFields(true)
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("library %s: %s: %w", label, LibraryManifest, err)
		}
		if m.Components == "" {
			m.Components = defaultLibraryComponents
		}
		if err := checkDir(m.Components); err != nil {
			return nil, fmt.Errorf("library %s: components: %w", label, err)
		}
		lib := &library{name: cmpOrStr(l.Name, cmpOrStr(m.Name, label)), root: l.Dir, compDir: path.Join(l.Dir, fsDir(m.Components)), prefix: l.Prefix, only: l.Only, manifest: m}
		for _, g := range m.Global {
			if !isComponentStem(g) {
				return nil, fmt.Errorf("library %s: global: %q is not a component name", lib.name, g)
			}
			lib.global = append(lib.global, path.Join(lib.compDir, g+htmlExt))
		}
		out = append(out, lib)
	}
	return out, nil
}

// libraries returns the resolved libraries, reading the manifests once.
func (e *Engine) libraries() ([]*library, error) {
	e.libsOnce.Do(func() { e.libs, e.libErr = e.loadLibraries() })
	return e.libs, e.libErr
}

// libraryOf returns the declared library a file belongs to, or nil. Only
// roots are compared, and a library whose manifest failed to load is not
// matched — that failure is reported when the registry is built.
func (e *Engine) libraryOf(file string) *library {
	libs, _ := e.libraries()
	var best *library
	for _, l := range libs {
		if (file == l.root || strings.HasPrefix(file, l.root+"/")) && (best == nil || len(l.root) > len(best.root)) {
			best = l
		}
	}
	return best
}

// baseOf is what `~/` means inside file: the root of the library it belongs
// to (the folder holding brick-library.yaml), else, in a vendored module,
// the nearest folder above it holding a manifest or the module root, else
// the project root (""). A library can therefore never reach into the
// project that uses it, wherever it sits.
func (e *Engine) baseOf(file string) string {
	if l := e.libraryOf(file); l != nil {
		return l.root
	}
	mod := moduleRoot(file)
	if mod == "" {
		return ""
	}
	for dir := path.Dir(file); dir != mod && strings.HasPrefix(dir, mod+"/"); dir = path.Dir(dir) {
		if e.hasManifest(dir) {
			return dir
		}
	}
	return mod
}

// hasManifest reports whether dir holds a library manifest, cached.
func (e *Engine) hasManifest(dir string) bool {
	e.mu.Lock()
	v, ok := e.manifests[dir]
	e.mu.Unlock()
	if ok {
		return v
	}
	ok, _ = e.exists(path.Join(dir, LibraryManifest))
	e.mu.Lock()
	if e.manifests == nil {
		e.manifests = map[string]bool{}
	}
	e.manifests[dir] = ok
	e.mu.Unlock()
	return ok
}

// withGlobals adds, for every asset-carrying file that belongs to a
// library, that library's global components — the manifest's `global:`
// list — so a kit's design tokens reach every page that uses it without a
// <Theme /> in the layout. Returned sorted, each once.
func (e *Engine) withGlobals(paths []string) []string {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
		if l := e.libraryOf(p); l != nil {
			for _, g := range l.global {
				set[g] = true
			}
		}
	}
	return sortedKeys(set)
}

// cmpOrStr is the first non-empty string.
func cmpOrStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// mountFS is the engine's filesystem when embedded libraries are in use:
// the project's FS, with each library's FS mounted read-only at
// brick_libs/<name>/. Paths stay plain strings inside one tree, so path
// confinement (X1, X2, X7) is unchanged.
type mountFS struct {
	base   fs.FS
	mounts map[string]fs.FS
}

// Open routes a name under brick_libs/<name>/ to that library's FS, lists
// the mounts for brick_libs itself, and passes everything else to the
// project's FS.
func (m *mountFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name == LibraryMountDir {
		return &mountDir{name: LibraryMountDir, entries: m.entries()}, nil
	}
	if rest, ok := strings.CutPrefix(name, LibraryMountDir+"/"); ok {
		lib, sub, _ := strings.Cut(rest, "/")
		lfs, ok := m.mounts[lib]
		if !ok {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
		}
		if sub == "" {
			sub = "."
		}
		return lfs.Open(sub)
	}
	return m.base.Open(name)
}

// entries lists the mounted libraries as directories.
func (m *mountFS) entries() []fs.DirEntry {
	names := make([]string, 0, len(m.mounts))
	for n := range m.mounts {
		names = append(names, n)
	}
	slices.Sort(names)
	out := make([]fs.DirEntry, len(names))
	for i, n := range names {
		out[i] = fs.FileInfoToDirEntry(dirInfo{name: n})
	}
	return out
}

// mountDir is the synthetic brick_libs directory.
type mountDir struct {
	name    string
	entries []fs.DirEntry
	read    bool
}

func (d *mountDir) Stat() (fs.FileInfo, error) { return dirInfo{name: d.name}, nil }
func (d *mountDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: errors.New("is a directory")}
}
func (d *mountDir) Close() error { return nil }

// ReadDir returns the mounts (all of them at n <= 0, once).
func (d *mountDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.read {
		if n > 0 {
			return nil, io.EOF
		}
		return nil, nil
	}
	d.read = true
	return d.entries, nil
}

// dirInfo describes a synthetic directory.
type dirInfo struct{ name string }

func (i dirInfo) Name() string       { return i.name }
func (i dirInfo) Size() int64        { return 0 }
func (i dirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (i dirInfo) ModTime() time.Time { return time.Time{} }
func (i dirInfo) IsDir() bool        { return true }
func (i dirInfo) Sys() any           { return nil }

// emptyFS is the base when a host passes only embedded libraries.
type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}
