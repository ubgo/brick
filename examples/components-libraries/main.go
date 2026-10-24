// Command components-libraries shows the three ways a site gets components
// it did not write: a library folder inside the project, a library a Go
// program offers by name (the embedded UI kit), and a remote library that
// `brick vendor` copies into brick_modules/. All three are listed in one
// brick.yaml `libraries:` and used as plain tags with no import in any page.
// The remote library is served from this folder (remote-src/) instead of
// GitHub, so the program needs no network and no account.
//
//	go run .          vendor, render both pages, print the lock and the pages
//	go test .         check the exact output (testdata/output.txt)
package main

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"strings"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/kit"
	"github.com/ubgo/brick/remote"
)

// themeDir is the project the program copies into a scratch folder, so
// `brick vendor` has somewhere to write brick_modules/ and brick.lock
// without touching the source tree.
const themeDir = "theme"

// configFile is the project's engine settings, whose `libraries:` lists
// every library the pages use.
const configFile = "brick.yaml"

// The one remote repository the pretend fetcher serves; remoteSrc holds
// its files.
const (
	fakeHost  = "github.com"
	fakeOwner = "acme"
	fakeRepo  = "ui"
	fakeTag   = "v1.2.0"
)

//go:embed remote-src
var remoteSrc embed.FS

// source maps a remote file onto remote-src/, refusing any repository or
// version it does not hold, so a typo in a spec fails instead of fetching
// something else.
func source(r brick.Remote) (string, error) {
	if r.Host != fakeHost || r.Owner != fakeOwner || r.Repo != fakeRepo || r.Version != fakeTag {
		return "", fmt.Errorf("remote-src only serves %s/%s/%s@%s, not %s", fakeHost, fakeOwner, fakeRepo, fakeTag, r)
	}
	return path.Join("remote-src", r.Owner, r.Repo), nil
}

// fetch is remote.Fetch over remote-src. A missing file wraps
// fs.ErrNotExist, which Vendor needs to tell "no manifest here" from a
// failed download.
// ds:def id=fetch-6mus9qqw
func fetch(_ context.Context, r brick.Remote) ([]byte, error) {
	base, err := source(r)
	if err != nil {
		return nil, err
	}
	return remoteSrc.ReadFile(path.Join(base, r.Path))
}

// list is remote.List over remote-src: every file under the folder, as a
// repository-relative path, like GitHub's tree listing.
// ds:def id=list-t2zdw492
func list(_ context.Context, dir brick.Remote) ([]string, error) {
	base, err := source(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	err = fs.WalkDir(remoteSrc, path.Join(base, dir.Path), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		out = append(out, strings.TrimPrefix(p, base+"/"))
		return nil
	})
	return out, err
}

// setup copies the theme into work and vendors its remote libraries and
// `use:` imports: what `brick vendor` does, with the pretend GitHub.
// ds:def id=setup-rjv29uqg
func setup(ctx context.Context, work string) (remote.Report, error) {
	if err := os.CopyFS(work, os.DirFS(themeDir)); err != nil {
		return remote.Report{}, err
	}
	return remote.Vendor(ctx, remote.Options{Dir: work, Fetch: fetch, List: list})
}

// newEngine reads brick.yaml and applies it. The kit is offered by name:
// `from: brick-kit` finds nothing else, so a config can never reach a
// library the Go program did not hand over.
// ds:def id=newengine-fz42jpaf
func newEngine(work string) (*brick.Engine, error) {
	root, err := os.OpenRoot(work)
	if err != nil {
		return nil, err
	}
	cfg, err := brick.LoadConfig(root.FS(), configFile)
	if err != nil {
		return nil, err
	}
	opts := brick.Options{FS: root.FS()}
	if err := cfg.Apply(&opts, kit.Library()); err != nil {
		return nil, err
	}
	return brick.New(opts)
}

// pages are rendered in this order.
var pages = []string{"content/index.html", "content/remote-use.html"}

// run vendors, renders the pages and prints what the guide shows.
func run(ctx context.Context, w io.Writer, work string) error {
	rep, err := setup(ctx, work)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "== vendored %d files (%d fetched)\n%s\n", len(rep.Files), rep.Fetched, strings.Join(rep.Files, "\n")); err != nil {
		return err
	}
	eng, err := newEngine(work)
	if err != nil {
		return err
	}
	for _, p := range pages {
		html, err := eng.Render(ctx, p, nil)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "\n== %s\n%s\n", p, html); err != nil {
			return err
		}
	}
	return nil
}

// main renders into a throwaway work directory, which it removes afterwards so
// the example leaves nothing behind. The tests call run with a buffer instead of
// stdout, so the output they pin is this program's real output.
func main() {
	work, err := os.MkdirTemp("", "components-libraries-")
	if err != nil {
		log.Fatal(err)
	}
	err = run(context.Background(), os.Stdout, work)
	_ = os.RemoveAll(work)
	if err != nil {
		log.Fatal(err)
	}
}
