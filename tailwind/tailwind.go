// Package tailwind rebuilds a Tailwind stylesheet with the standalone
// `tailwindcss` binary, only when the classes a project uses have changed
// .
//
// Why a class hash: the binary takes about a second, and a dev server that
// ran it on every save would make live reload feel slow. Most saves change
// text, not classes, so hashing the class candidates plus the input CSS
// decides whether a run can change the output at all.
//
// No hidden state: the hash is stamped on the first line of the output file
// itself. Delete the file and the next Build runs the binary.
//
// The package imports nothing from brick; any project can use it.
package tailwind

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// DefaultBinary is looked up on PATH when Options.Binary is empty.
const DefaultBinary = "tailwindcss"

// stampPrefix starts the output's first line: /* brick-tailwind: <hash> */.
const stampPrefix = "/* brick-tailwind: "

// Options configures a build. Paths are relative to Dir.
type Options struct {
	// Dir is the project directory the binary runs in.
	Dir string
	// Input is the source CSS (with `@import "tailwindcss";` for v4).
	Input string
	// Output is the CSS to write, normally under static/.
	Output string
	// Binary is the tailwindcss executable: a name looked up on PATH, or a
	// path, relative to Dir unless absolute. Empty looks up DefaultBinary.
	Binary string
	// Sources are the extensions scanned for classes. Default .html and .md.
	Sources []string
}

// ErrNoBinary is returned when the tailwindcss binary cannot be found.
var ErrNoBinary = errors.New("tailwind: the tailwindcss binary was not found on PATH; install the standalone CLI (https://tailwindcss.com/blog/standalone-cli) or set Binary")

// classAttrRE finds class attribute values in markup.
var classAttrRE = regexp.MustCompile(`\bclass\s*=\s*(?:"([^"]*)"|'([^']*)')`)

// Candidates returns the sorted, de-duplicated class tokens used in the
// project: every whitespace-separated token inside a class attribute.
// Template actions inside a value are kept as tokens; they rarely change
// and only make the hash more conservative.
func Candidates(fsys fs.FS, sources []string) ([]string, error) {
	if len(sources) == 0 {
		sources = []string{".html", ".md"}
	}
	seen := map[string]bool{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !slices.Contains(sources, filepath.Ext(p)) {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		for _, m := range classAttrRE.FindAllSubmatch(b, -1) {
			v := m[1]
			if len(v) == 0 {
				v = m[2]
			}
			for _, tok := range bytes.Fields(v) {
				seen[string(tok)] = true
			}
		}
		return nil
	})
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	slices.Sort(out)
	return out, err
}

// Stamp is the hash that decides whether the output is current.
func Stamp(candidates []string, input []byte) string {
	h := sha256.New()
	for _, c := range candidates {
		h.Write([]byte(c))
		h.Write([]byte{0})
	}
	h.Write([]byte{1})
	h.Write(input)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Build rebuilds the output when the class set or the input changed, and
// reports whether it ran the binary.
func Build(ctx context.Context, o Options) (bool, error) {
	if o.Input == "" || o.Output == "" {
		return false, errors.New("tailwind: Input and Output are required")
	}
	root := o.Dir
	if root == "" {
		root = "."
	}
	input, err := os.ReadFile(filepath.Join(root, o.Input))
	if err != nil {
		return false, fmt.Errorf("tailwind: %w", err)
	}
	cands, err := Candidates(os.DirFS(root), o.Sources)
	if err != nil {
		return false, fmt.Errorf("tailwind: %w", err)
	}
	stamp := Stamp(cands, input)
	outPath := filepath.Join(root, o.Output)
	if cur, err := os.ReadFile(outPath); err == nil {
		first, _, _ := bytes.Cut(cur, []byte("\n"))
		if string(first) == stampPrefix+stamp+" */" {
			return false, nil
		}
	}
	bin := o.Binary
	if bin == "" {
		bin = DefaultBinary
	}
	if strings.ContainsRune(bin, '/') && !filepath.IsAbs(bin) {
		// A relative path in project config means relative to the project.
		bin = filepath.Join(root, bin)
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return false, ErrNoBinary
	}
	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".tailwind-*.css")
	if err != nil {
		return false, fmt.Errorf("tailwind: %w", err)
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpName)
	cmd := exec.CommandContext(ctx, path, "-i", o.Input, "-o", tmpName, "--minify")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("tailwind: %s: %w: %s", bin, err, strings.TrimSpace(stderr.String()))
	}
	css, err := os.ReadFile(tmpName)
	if err != nil {
		return false, fmt.Errorf("tailwind: %w", err)
	}
	out := append([]byte(stampPrefix+stamp+" */\n"), css...)
	// Write beside the target and rename, so a server never reads half a file.
	final, err := os.CreateTemp(filepath.Dir(outPath), ".tailwind-out-*.css")
	if err != nil {
		return false, fmt.Errorf("tailwind: %w", err)
	}
	if _, err := final.Write(out); err != nil {
		_ = final.Close()
		_ = os.Remove(final.Name())
		return false, err
	}
	if err := final.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(final.Name(), outPath); err != nil {
		_ = os.Remove(final.Name())
		return false, err
	}
	return true, nil
}
