package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/internal/format"
	"github.com/ubgo/brick/internal/frontmatter"
	"github.com/ubgo/brick/internal/lex"
	"github.com/ubgo/brick/kit"
)

// brick fmt lays brick files out one way, like gofmt. What
// it may change, and the guarantee that rendering does not, are
// internal/format's; this file finds the files and the raw components.

const fmtUsage = `brick fmt — format brick .html files

Usage:
  brick fmt [-l] [-w] [-d] [-line-length n] [-attr-order list] [-inline list] [path ...]

With no path it formats the current directory. A directory is walked for
.html files, skipping hidden folders, brick_modules/ and node_modules/.
"-" reads standard input and writes the result to standard output.

By default it prints each formatted file to standard output. It indents
nested elements and component tags, drops trailing spaces, tidies
attribute spacing and frontmatter fences, and never changes {{ }} actions,
<pre>, <textarea>, <script>, <style>, raw bodies or brick: bodies, so a page
renders the same before and after. Turn it off for a region with
{{/* brick:no-format */}} … {{/* brick:format */}} (or for a whole file
by starting it with the first). When a file's tags cannot be followed with
certainty, its indentation is left as written and -v says why.

A site can set more in brick.yaml (the nearest one above each path), all
off when absent:

  fmt:
    lineLength: 100                  # wrap longer lines, one attribute per line
    attrOrder: ["brick:*", id, class] # these attributes first, in this order
    inline: [a, em, strong, Icon]     # their content keeps the parent's indent
    formatters:                       # hand these bodies to a command
      brick:client: prettier --stdin-filepath client.js
      brick:style: prettier --stdin-filepath style.css

Wrapping changes only whitespace inside tags. Ordering changes the order of
attributes in the output, never their values; a tag holding {{ }} between
attributes, repeating one, or spread over lines keeps its order. A
formatter gets the body on standard input, without its indentation, and
prints the result; a body holding {{ is left alone. It runs in the site's
folder, without a shell, and a failure leaves the file untouched.

Flags:
  -l   list the files whose formatting differs, instead of printing them
  -w   write the result back to each file
  -d   print a unified diff instead of the file
  -v   with -l, -w or -d: say why a file's indentation was left alone
  -line-length n   wrap lines longer than n (overrides fmt.lineLength; 0 off)
  -attr-order list comma-separated attributes to put first (overrides
                   fmt.attrOrder)
  -inline list     comma-separated elements whose content is not indented
                   (overrides fmt.inline)

Examples:
  brick fmt -d                 what would change in this site
  brick fmt -l components      which components are not formatted
  brick fmt -w content/about.html
  brick fmt - < card.html      format standard input
  brick fmt -d -line-length 100 -attr-order 'brick:*,id,class'

Exit status is 1 when a file cannot be read or parsed, 0 otherwise.
`

// Folders brick fmt never walks into: vendored and installed code is
// someone else's layout.
var fmtSkipDirs = map[string]bool{"brick_modules": true, "node_modules": true}

// fmtExt is the extension of the files brick fmt formats.
const fmtExt = ".html"

// markdownComponent is raw when a Markdown renderer is set, which the CLI
// always sets (open).
const markdownComponent = "Markdown"

// rawKey is the frontmatter key that makes a component's body raw.
const rawKey = "raw"

// useKey is the frontmatter key that imports components under a new name.
const useKey = "use"

// fmtFlags are what brick fmt was asked to do.
type fmtFlags struct{ list, write, diff, verbose bool }

// fmtFormatterTimeout bounds one external body formatter run, so a
// formatter waiting on a terminal or a network cannot hang brick fmt or
// the editor's format-on-save.
const fmtFormatterTimeout = 30 * time.Second

// lineLengthUnset is -line-length's default: no override of brick.yaml.
const lineLengthUnset = -1

// fmtOverrides are the flags that override brick.yaml's fmt: section.
type fmtOverrides struct {
	lineLength int
	attrOrder  []string
	order      bool // attrOrder was given
	inline     []string
	hasInline  bool // inline was given
}

// fmtOptions are the format options for files of the site at root:
// its brick.yaml fmt: section, then the flags. A site with no brick.yaml
// gets the flags alone.
func fmtOptions(root string, ov fmtOverrides) (format.Options, error) {
	var opts format.Options
	if _, err := os.Stat(filepath.Join(root, configFile)); err == nil {
		cfg, err := brick.LoadConfig(os.DirFS(root), configFile)
		if err != nil {
			return opts, err
		}
		if c := cfg.Fmt; c != nil {
			opts.LineLength = c.LineLength
			opts.AttrOrder = c.AttrOrder
			opts.Inline = c.Inline
			if len(c.Formatters) > 0 {
				opts.TagFormatters = map[string]func([]byte) ([]byte, error){}
				for tag, cmd := range c.Formatters {
					if len(strings.Fields(cmd)) == 0 {
						return opts, fmt.Errorf("%s: fmt.formatters.%s: empty command", configFile, tag)
					}
					opts.TagFormatters[tag] = externalFormatter(root, cmd)
				}
			}
		}
	}
	if ov.lineLength != lineLengthUnset {
		opts.LineLength = ov.lineLength
	}
	if ov.order {
		opts.AttrOrder = ov.attrOrder
	}
	if ov.hasInline {
		opts.Inline = ov.inline
	}
	return opts, nil
}

// externalFormatter runs cmd (split on spaces, no shell) in dir with a
// body on standard input and takes its standard output as the new body.
func externalFormatter(dir, cmd string) func([]byte) ([]byte, error) {
	args := strings.Fields(cmd)
	return func(body []byte) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), fmtFormatterTimeout)
		defer cancel()
		c := exec.CommandContext(ctx, args[0], args[1:]...)
		c.Dir = dir
		c.Stdin = bytes.NewReader(body)
		var out, errb bytes.Buffer
		c.Stdout, c.Stderr = &out, &errb
		if err := c.Run(); err != nil {
			if msg := strings.TrimSpace(errb.String()); msg != "" {
				return nil, fmt.Errorf("%s: %w: %s", cmd, err, msg)
			}
			return nil, fmt.Errorf("%s: %w", cmd, err)
		}
		return out.Bytes(), nil
	}
}

// splitList is a comma-separated flag value, blanks dropped.
func splitList(s string) []string {
	var out []string
	for v := range strings.SplitSeq(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// fmtCmd is brick fmt.
func fmtCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var f fmtFlags
	fl := flag.NewFlagSet("brick fmt", flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.Usage = func() { fmt.Fprint(stderr, fmtUsage) }
	fl.BoolVar(&f.list, "l", false, "list files whose formatting differs")
	fl.BoolVar(&f.write, "w", false, "write the result back to the file")
	fl.BoolVar(&f.diff, "d", false, "print a diff instead of the file")
	fl.BoolVar(&f.verbose, "v", false, "say why indentation was left alone")
	ov := fmtOverrides{}
	fl.IntVar(&ov.lineLength, "line-length", lineLengthUnset, "wrap lines longer than this")
	order := fl.String("attr-order", "", "attributes to put first, comma-separated")
	inline := fl.String("inline", "", "elements whose content is not indented, comma-separated")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	fl.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "attr-order":
			ov.attrOrder, ov.order = splitList(*order), true
		case "inline":
			ov.inline, ov.hasInline = splitList(*inline), true
		}
	})
	if ov.lineLength < lineLengthUnset {
		fmt.Fprintf(stderr, "brick fmt: -line-length must be 0 or more\n")
		return exitUsage
	}
	paths := fl.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	raws := newRawSet()
	if err := raws.addFS(kit.FS(), ""); err != nil {
		fmt.Fprintln(stderr, "brick fmt:", err)
		return exitFail
	}
	failed := false
	for _, p := range paths {
		if p == "-" {
			src, err := io.ReadAll(stdin)
			var opts format.Options
			if err == nil {
				opts, err = fmtOptions(siteRoot("."), ov)
			}
			if err == nil {
				err = fmtOne("<stdin>", src, raws, opts, fmtFlags{diff: f.diff, verbose: f.verbose}, stdout, stderr, nil)
			}
			if err != nil {
				fmt.Fprintln(stderr, "brick fmt:", err)
				failed = true
			}
			continue
		}
		files, err := fmtFiles(p)
		if err != nil {
			fmt.Fprintln(stderr, "brick fmt:", err)
			failed = true
			continue
		}
		if err := raws.addSite(p); err != nil {
			fmt.Fprintln(stderr, "brick fmt:", err)
			failed = true
			continue
		}
		opts, err := fmtOptions(siteRoot(p), ov)
		if err != nil {
			fmt.Fprintln(stderr, "brick fmt:", err)
			failed = true
			continue
		}
		for _, file := range files {
			src, err := os.ReadFile(file)
			if err == nil {
				err = fmtOne(file, src, raws, opts, f, stdout, stderr, func(out []byte) error {
					info, err := os.Stat(file)
					if err != nil {
						return err
					}
					return os.WriteFile(file, out, info.Mode().Perm())
				})
			}
			if err != nil {
				fmt.Fprintf(stderr, "brick fmt: %s: %v\n", file, err)
				failed = true
			}
		}
	}
	if failed {
		return exitFail
	}
	return exitOK
}

// formatDocument formats one file's text as brick fmt path would, raw
// components of the kit and of the file's site included, with the site's
// brick.yaml fmt: options, without touching the file: the language
// server's formatting request (brick lsp).
func formatDocument(path string, src []byte) ([]byte, error) {
	raws := newRawSet()
	if err := raws.addFS(kit.FS(), ""); err != nil {
		return nil, err
	}
	if err := raws.addSite(path); err != nil {
		return nil, err
	}
	opts, err := fmtOptions(siteRoot(path), fmtOverrides{lineLength: lineLengthUnset})
	if err != nil {
		return nil, err
	}
	opts.IsRaw = raws.forFile(src)
	res, err := format.Format(src, opts)
	if err != nil {
		return nil, err
	}
	return res.Src, nil
}

// fmtOne formats one file with opts (plus the file's raw components) and
// reports it the way the flags ask.
func fmtOne(name string, src []byte, raws *rawSet, opts format.Options, f fmtFlags, stdout, stderr io.Writer, write func([]byte) error) error {
	opts.IsRaw = raws.forFile(src)
	res, err := format.Format(src, opts)
	if err != nil {
		return err
	}
	changed := !bytes.Equal(src, res.Src)
	if f.verbose && res.IndentSkipped != "" {
		fmt.Fprintf(stderr, "%s: indentation left as written: %s\n", name, res.IndentSkipped)
	}
	if f.list && changed {
		fmt.Fprintln(stdout, name)
	}
	if f.diff {
		stdout.Write(format.Diff(name, src, res.Src))
	}
	if f.write && changed && write != nil {
		if err := write(res.Src); err != nil {
			return err
		}
	}
	if !f.list && !f.diff && !f.write {
		stdout.Write(res.Src)
	}
	return nil
}

// fmtFiles is p itself when it is a file, or every .html file under it.
func fmtFiles(p string) ([]string, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{p}, nil
	}
	var files []string
	err = filepath.WalkDir(p, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if file != p && (strings.HasPrefix(d.Name(), ".") || fmtSkipDirs[d.Name()]) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(file, fmtExt) {
			files = append(files, file)
		}
		return nil
	})
	return files, err
}

// rawSet is every component known to have a raw body. The formatter must
// know them, since a raw body taken for markup would be re-indented; the
// engine knows from the callee's frontmatter, and so does this, by
// reading every component it can see: the kit, and each formatted path's
// site (the nearest folder up with brick.yaml), vendored libraries
// included.
type rawSet struct {
	names map[string]bool
	seen  map[string]bool
}

func newRawSet() *rawSet {
	return &rawSet{names: map[string]bool{markdownComponent: true}, seen: map[string]bool{}}
}

// siteRoot is the site p belongs to: the nearest folder up holding
// brick.yaml, else p's own folder.
func siteRoot(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	root := abs
	// A file, or a path not on disk yet (an editor's unsaved document),
	// belongs to the folder holding it.
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		root = filepath.Dir(abs)
	}
	for d := root; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, configFile)); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return root
		}
	}
}

// addSite reads the components of the site p belongs to.
func (r *rawSet) addSite(p string) error {
	root := siteRoot(p)
	if r.seen[root] {
		return nil
	}
	r.seen[root] = true
	return r.addFS(os.DirFS(root), root)
}

// addFS records the raw components in fsys.
func (r *rawSet) addFS(fsys fs.FS, label string) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, fmtExt) {
			return nil
		}
		src, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		doc, err := frontmatter.Parse(src)
		if err != nil {
			// brick fmt reports this file's error when it formats it; a
			// component it only reads for raw: is not its business.
			return nil
		}
		if raw, _ := doc.Params[rawKey].(bool); raw {
			r.names[strings.TrimSuffix(path.Base(p), fmtExt)] = true
		}
		return nil
	})
}

// forFile is the raw test for one file: a known raw component under any
// library prefix (KitCodeBlock) or as x-code-block, or a name the file's
// use: imports from a raw component.
func (r *rawSet) forFile(src []byte) func(string, []lex.Attr) bool {
	aliases := map[string]bool{}
	if doc, err := frontmatter.Parse(src); err == nil {
		if use, ok := doc.Params[useKey].(map[string]any); ok {
			for k, v := range use {
				if s, ok := v.(string); ok && r.isRaw(strings.TrimSuffix(path.Base(s), fmtExt)) {
					aliases[k] = true
				}
			}
		}
	}
	return func(name string, _ []lex.Attr) bool { return aliases[name] || r.isRaw(name) }
}

func (r *rawSet) isRaw(name string) bool {
	if strings.HasPrefix(name, "x-") {
		name = pascal(name[2:])
	}
	for n := range r.names {
		if name == n || strings.HasSuffix(name, n) {
			return true
		}
	}
	return false
}

// pascal turns code-block into CodeBlock, the file name <x-code-block>
// resolves to.
func pascal(kebab string) string {
	var b strings.Builder
	for part := range strings.SplitSeq(kebab, "-") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}
