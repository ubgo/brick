// Package disk is the reference brick.UploadSink: it
// writes each uploaded file into one directory the host names, under a
// random id, and serves it back through a handler the host mounts. It uses
// the standard library only, and doubles as the test double for hosts.
//
// Uploads are temporary until the app keeps them: a script never touches
// the filesystem, so "keeping" is a host capability (ScriptGlobals) that
// reads the file with Open and stores it wherever the app keeps records.
// Sweep removes files older than the TTL, so a file uploaded and never
// kept does not stay forever. The sink starts no goroutine; the host calls
// Sweep on its own timer.
//
//	sink, _ := disk.New(disk.Options{Dir: "/var/tmp/uploads", URLPrefix: "/uploads/"})
//	mux.Handle("/uploads/", http.StripPrefix("/uploads/", sink.Handler()))
//	eng, _ := brick.New(brick.Options{Uploads: sink, …})
package disk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ubgo/brick"
)

// DefaultTTL is how long an upload stays before Sweep removes it, when
// Options.TTL is zero.
const DefaultTTL = 24 * time.Hour

// File names inside Options.Dir: <id> is the data, <id>.json its ref, and
// <id>.part a file still being written.
const (
	metaExt = ".json"
	partExt = ".part"
	// idBytes is the size of a file id: 128 random bits, so ids cannot be
	// guessed from one another.
	idBytes = 16
	// dirPerm and filePerm keep uploads private to the server's user.
	dirPerm  = 0o700
	filePerm = 0o600
)

// idRE is the only id shape the sink accepts back, so an id from a
// request can never name a path outside Dir.
var idRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ErrNotFound means no upload has that id (unknown, swept or removed).
var ErrNotFound = errors.New("disk: no such upload")

// Options configure a Sink.
type Options struct {
	// Dir is the directory uploads are written to; created if missing.
	Dir string
	// URLPrefix is joined with the id to make UploadRef.URL ("/uploads/"
	// → "/uploads/<id>"); it is where the host mounts Handler. Empty means
	// refs carry no URL.
	URLPrefix string
	// TTL is how old a file is when Sweep removes it; zero means
	// DefaultTTL.
	TTL time.Duration
	// Rand replaces crypto/rand for ids, and Clock time.Now — tests only.
	Rand  io.Reader
	Clock func() time.Time
}

// Sink is an UploadSink, UploadRemover and file server over one
// directory. It is safe for concurrent use: each file has its own name,
// and a file appears under its id only once complete (written as .part,
// renamed on Close).
type Sink struct {
	opts Options
}

var (
	_ brick.UploadSink    = (*Sink)(nil)
	_ brick.UploadRemover = (*Sink)(nil)
)

// New returns a sink writing to opts.Dir.
func New(opts Options) (*Sink, error) {
	if opts.Dir == "" {
		return nil, errors.New("disk: Options.Dir is required")
	}
	if err := os.MkdirAll(opts.Dir, dirPerm); err != nil {
		return nil, fmt.Errorf("disk: %w", err)
	}
	if opts.TTL <= 0 {
		opts.TTL = DefaultTTL
	}
	if opts.Rand == nil {
		opts.Rand = rand.Reader
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &Sink{opts: opts}, nil
}

// Open starts one file under a fresh random id.
func (s *Sink) Open(_ context.Context, m brick.UploadMeta) (brick.UploadWriter, error) {
	b := make([]byte, idBytes)
	if _, err := io.ReadFull(s.opts.Rand, b); err != nil {
		return nil, fmt.Errorf("disk: id: %w", err)
	}
	id := hex.EncodeToString(b)
	f, err := os.OpenFile(s.path(id)+partExt, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePerm)
	if err != nil {
		return nil, fmt.Errorf("disk: %w", err)
	}
	return &writer{s: s, f: f, id: id, m: m}, nil
}

// writer is one file being written.
type writer struct {
	s  *Sink
	f  *os.File
	id string
	m  brick.UploadMeta
	n  int64
}

func (w *writer) Write(p []byte) (int, error) {
	n, err := w.f.Write(p)
	w.n += int64(n)
	return n, err
}

// Close commits: the ref is written beside the data, then the data is
// renamed into place, so a file under its id always has its ref.
func (w *writer) Close() (brick.UploadRef, error) {
	ref := brick.UploadRef{ID: w.id, Name: filepath.Base(w.m.Name), Type: w.m.Type, Size: w.n}
	if ref.Name == "." || ref.Name == string(filepath.Separator) {
		ref.Name = ""
	}
	if w.s.opts.URLPrefix != "" {
		ref.URL = w.s.opts.URLPrefix + w.id
	}
	fail := func(err error) (brick.UploadRef, error) {
		_ = w.f.Close()
		_ = os.Remove(w.f.Name())
		_ = os.Remove(w.s.path(w.id) + metaExt)
		return brick.UploadRef{}, fmt.Errorf("disk: %w", err)
	}
	if err := w.f.Sync(); err != nil {
		return fail(err)
	}
	b, err := json.Marshal(ref)
	if err != nil {
		return fail(err)
	}
	if err := os.WriteFile(w.s.path(w.id)+metaExt, b, filePerm); err != nil {
		return fail(err)
	}
	if err := w.f.Close(); err != nil {
		return fail(err)
	}
	if err := os.Rename(w.f.Name(), w.s.path(w.id)); err != nil {
		return fail(err)
	}
	return ref, nil
}

// Abort discards the partial file.
func (w *writer) Abort(error) error {
	_ = w.f.Close()
	if err := os.Remove(w.f.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("disk: %w", err)
	}
	return nil
}

// Remove deletes one upload (brick's $removeUpload). An unknown id is not
// an error: the file is gone either way.
func (s *Sink) Remove(_ context.Context, ref brick.UploadRef) error {
	if !idRE.MatchString(ref.ID) {
		return ErrNotFound
	}
	for _, p := range []string{s.path(ref.ID), s.path(ref.ID) + metaExt} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("disk: %w", err)
		}
	}
	return nil
}

// OpenFile returns one upload's bytes and ref, for the host capability that
// keeps it (copies it to permanent storage). The caller closes the file.
func (s *Sink) OpenFile(id string) (*os.File, brick.UploadRef, error) {
	if !idRE.MatchString(id) {
		return nil, brick.UploadRef{}, ErrNotFound
	}
	b, err := os.ReadFile(s.path(id) + metaExt)
	if errors.Is(err, os.ErrNotExist) {
		return nil, brick.UploadRef{}, ErrNotFound
	}
	if err != nil {
		return nil, brick.UploadRef{}, fmt.Errorf("disk: %w", err)
	}
	var ref brick.UploadRef
	if err := json.Unmarshal(b, &ref); err != nil {
		return nil, brick.UploadRef{}, fmt.Errorf("disk: %s: %w", id, err)
	}
	f, err := os.Open(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, brick.UploadRef{}, ErrNotFound
	}
	if err != nil {
		return nil, brick.UploadRef{}, fmt.Errorf("disk: %w", err)
	}
	return f, ref, nil
}

// Sweep removes every file in Dir older than the TTL — kept uploads must
// have been copied out by then — including partial ones a crash left. It
// returns how many uploads it removed.
func (s *Sink) Sweep() (int, error) {
	entries, err := os.ReadDir(s.opts.Dir)
	if err != nil {
		return 0, fmt.Errorf("disk: %w", err)
	}
	cutoff := s.opts.Clock().Add(-s.opts.TTL)
	removed := 0
	for _, e := range entries {
		name := e.Name()
		id := strings.TrimSuffix(strings.TrimSuffix(name, metaExt), partExt)
		if e.IsDir() || !idRE.MatchString(id) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(s.opts.Dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("disk: %w", err)
		}
		if name == id {
			removed++
		}
	}
	return removed, nil
}

// Handler serves uploads by id, for the host to mount at URLPrefix
// (behind http.StripPrefix). It sends the sniffed type the engine
// recorded, with nosniff; only images are shown inline, everything else
// downloads, so an uploaded file is never rendered as a page of this
// origin.
func (s *Sink) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		f, ref, err := s.OpenFile(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ref.Type)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		disposition := "attachment"
		if strings.HasPrefix(ref.Type, "image/") && ref.Type != "image/svg+xml" {
			disposition = "inline"
		}
		w.Header().Set("Content-Disposition", disposition)
		http.ServeContent(w, r, "", info.ModTime(), f)
	})
}

// path is an id's data file. Ids are checked against idRE before they
// reach here from outside.
func (s *Sink) path(id string) string { return filepath.Join(s.opts.Dir, id) }
