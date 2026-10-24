// Package kit ships the brick UI kit as an embedded component library
// , so a Go program uses it with no copy and no
// vendoring:
//
//	brick.Options{Libraries: []brick.Library{kit.Library()}}
//
// or offers it to brick.yaml's `libraries: [brick-kit]` through
// Config.Apply(&opts, kit.Library()). The same folder is also a library
// on its own (brick-library.yaml), for `brick vendor` and for copying.
package kit

import (
	"embed"
	"io/fs"

	"github.com/ubgo/brick"
)

// Name is the kit's library name: its mount (brick_libs/brick-kit/) and the
// bare name brick.yaml picks it by. It matches the manifest's name.
const Name = "brick-kit"

// files is the library folder as shipped: the manifest, the components and
// the data they read. Nothing else (README, Go files) is part of it, so the
// engine sees exactly what a vendored copy would.
//
//go:embed brick-library.yaml components data
var files embed.FS

// Library is the kit as an embedded library. Each call returns a fresh
// value, so a caller setting Prefix or Only never changes another's.
func Library() brick.Library {
	return brick.Library{Name: Name, FS: FS()}
}

// FS is the kit's library folder (brick-library.yaml at its root), for a
// host that mounts or inspects the files itself.
func FS() fs.FS { return files }
