// Package watch reports file changes under a directory by polling.
//
// Why polling, and why content hashes: an OS notification API (inotify,
// FSEvents, kqueue) needs a dependency and per-platform care, drops events
// under load, and fires on saves that change nothing. A poller that hashes
// file contents behaves the same on every OS and reports a file as modified
// only when its bytes changed — an editor that rewrites identical content, a
// `touch`, or clock skew cause nothing. For a site of a few thousand files a
// poll costs milliseconds.
//
// The package imports nothing from brick and works on any fs.FS.
package watch

import (
	"context"
	"crypto/sha256"
	"errors"
	"io/fs"
	"slices"
	"time"
)

// DefaultInterval is the poll interval when Options.Interval is zero.
const DefaultInterval = 300 * time.Millisecond

// Op is what happened to a file.
type Op int

// Operations.
const (
	Created Op = iota
	Modified
	Deleted
)

// String names the operation in lowercase, the word dev-server logs print.
func (o Op) String() string {
	switch o {
	case Created:
		return "created"
	case Deleted:
		return "deleted"
	}
	return "modified"
}

// Change is one file's change.
type Change struct {
	Path string
	Op   Op
}

// Options tunes a Watcher.
type Options struct {
	// Interval between polls. Zero means DefaultInterval.
	Interval time.Duration
	// Skip reports whether a path (file or directory) is ignored; a skipped
	// directory is not walked. Nil ignores nothing.
	Skip func(path string, d fs.DirEntry) bool
}

// snapshot maps each file to the hash of its contents.
type snapshot map[string][sha256.Size]byte

// Scan hashes every file under root: the state Diff compares.
func scan(fsys fs.FS, root string, skip func(string, fs.DirEntry) bool) (snapshot, error) {
	s := snapshot{}
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p != root && errors.Is(err, fs.ErrNotExist) {
				return nil // deleted between listing and reading
			}
			return err
		}
		if skip != nil && p != root && skip(p, d) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		s[p] = sha256.Sum256(b)
		return nil
	})
	return s, err
}

// diff lists the changes from old to cur, sorted by path.
func diff(old, cur snapshot) []Change {
	var out []Change
	for p, h := range cur {
		prev, ok := old[p]
		switch {
		case !ok:
			out = append(out, Change{Path: p, Op: Created})
		case prev != h:
			out = append(out, Change{Path: p, Op: Modified})
		}
	}
	for p := range old {
		if _, ok := cur[p]; !ok {
			out = append(out, Change{Path: p, Op: Deleted})
		}
	}
	slices.SortFunc(out, func(a, b Change) int {
		if a.Path < b.Path {
			return -1
		}
		if a.Path > b.Path {
			return 1
		}
		return 0
	})
	return out
}

// Watch polls root until ctx ends and sends one batch per poll that found
// changes; a save that touches three files arrives as one batch. The first
// scan establishes the baseline and sends nothing. A scan error (for
// example the root being removed) is sent on errs and polling continues.
// Both channels close when ctx ends.
func Watch(ctx context.Context, fsys fs.FS, root string, opts Options) (<-chan []Change, <-chan error, error) {
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	base, err := scan(fsys, root, opts.Skip)
	if err != nil {
		return nil, nil, err
	}
	changes := make(chan []Change)
	errs := make(chan error, 1)
	go func() {
		defer close(changes)
		defer close(errs)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			cur, err := scan(fsys, root, opts.Skip)
			if err != nil {
				select {
				case errs <- err:
				default:
				}
				continue
			}
			if batch := diff(base, cur); len(batch) > 0 {
				select {
				case changes <- batch:
				case <-ctx.Done():
					return
				}
			}
			base = cur
		}
	}()
	return changes, errs, nil
}
