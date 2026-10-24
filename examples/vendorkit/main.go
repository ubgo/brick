// Command vendorkit vendors the repository's own kit/ into examples/site as
// if it had been fetched from GitHub, so the demo shows a remote library
// without a network: `task examples:vendor`. It is `brick vendor` with a
// fetch and a list function that read the working tree — the remote
// package's escape hatches for mirrors and local development.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/remote"
)

// repoPrefix is where this repository's files live in a remote spec path.
const (
	repoHost  = "github.com"
	repoOwner = "ubgo"
	repoName  = "brick"
)

func main() {
	site := "site"
	if len(os.Args) > 1 {
		site = os.Args[1]
	}
	repo, err := filepath.Abs("..")
	if err != nil {
		log.Fatal(err)
	}
	fetch := func(_ context.Context, r brick.Remote) ([]byte, error) {
		if err := ours(r); err != nil {
			return nil, err
		}
		return os.ReadFile(filepath.Join(repo, filepath.FromSlash(r.Path)))
	}
	rep, err := remote.Vendor(context.Background(), remote.Options{Dir: site, Fetch: fetch, List: list(repo)})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("vendored %d files (%d fetched)\n", len(rep.Files), rep.Fetched)
}

// ours refuses a remote that is not this repository, or that climbs.
func ours(r brick.Remote) error {
	if r.Host != repoHost || r.Owner != repoOwner || r.Repo != repoName || strings.Contains(r.Path, "..") {
		return fmt.Errorf("vendorkit only serves %s/%s/%s, not %s", repoHost, repoOwner, repoName, r)
	}
	return nil
}

// list is remote.Options.List over the working tree: every file under the
// folder, as a repository-relative path, like GitHub's tree listing does;
// remote.Vendor itself drops the Go sources (kit.go) it lists.
func list(repo string) remote.List {
	return func(_ context.Context, dir brick.Remote) ([]string, error) {
		if err := ours(dir); err != nil {
			return nil, err
		}
		var out []string
		root := filepath.Join(repo, filepath.FromSlash(dir.Path))
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if strings.HasPrefix(d.Name(), ".") && p != root {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(repo, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		return out, err
	}
}
