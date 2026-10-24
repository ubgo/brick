// Command components-options tours the engine settings that decide how a
// theme's components are found and written: more than one component root
// (Options.Roots), per-file tag spelling (`syntax:`), tags left as plain
// HTML (`passthrough:` and `unknown:`), a data format brick does not read
// by itself (Options.DataFormats), `dedent:`, an element whose name is data
// (`tag` and `endtag`), component docs (`doc:`, `examples:` and
// Engine.ComponentDocs), the script step limit, the html-balance warning,
// `data-brick-loc`, hooks, Engine.InheritAssets and WithStats.
//
//	go run .          print every section the guide shows
//	go test .         check the exact output (testdata/output.txt)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/ubgo/brick"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// Component roots, earlier wins a name: the project's own components first,
// then a shared folder a second team maintains.
var roots = []string{"components", "shared"}

// scriptSteps is the per-call step limit the example lowers from the
// default (a million) so the runaway loop in lib/spin.star stops at once.
const scriptSteps = 10_000

// parseKV reads the example's own data format, .kv: `name = "value"` lines
// and `#` comments. A real host calls a library here (a CSV or TOML
// parser); the shape is the same: bytes in, plain Go values out, and brick
// normalizes them like it does YAML.
// ds:def id=parsetoml-jtxkffq5
func parseKV(src []byte) (any, error) {
	out := map[string]any{}
	for i, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, " = ")
		if !ok {
			return nil, fmt.Errorf("line %d: want `name = value`", i+1)
		}
		out[k] = strings.Trim(v, `"`)
	}
	return out, nil
}

// counter is a CompileHook and an AfterRenderHook: it counts what the
// engine compiled and rendered, the way a host feeds a metrics system.
// Hooks run on concurrent renders, hence the lock.
// ds:def id=counter-abndry84
type counter struct {
	mu       sync.Mutex
	compiled []string
	rendered []string
}

// OnCompile records each file once per snapshot.
// ds:def id=counter-oncompile-n9naz422
func (c *counter) OnCompile(_ context.Context, f *brick.CompiledFile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.compiled = append(c.compiled, f.Path)
}

// AfterRender records each finished page.
// ds:def id=counter-afterrender-utdqgruk
func (c *counter) AfterRender(_ context.Context, p *brick.Page, _ *brick.Result) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rendered = append(c.rendered, p.Path)
	return nil
}

// newEngine builds the engine for dir with every setting this example
// shows. Starlark gets the lowered step limit; mode picks Dev or Prod
// error display and Dev's data-brick-loc.
// ds:def id=newengine-ntjvm83k
func newEngine(dir string, mode brick.Mode, hooks ...any) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{Steps: scriptSteps})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{
		FS:          root.FS(),
		Roots:       roots,
		Scripting:   sc,
		DataFormats: map[string]brick.DataFormat{".kv": parseKV},
		Mode:        mode,
		Hooks:       hooks,
	})
}

// renderAll renders each page and prints the bytes, or the error a page
// fails with, in a labelled block.
func renderAll(ctx context.Context, w io.Writer, eng *brick.Engine, pages ...string) error {
	for _, p := range pages {
		out, err := eng.Render(ctx, p, nil)
		body := strings.TrimRight(string(out), "\n")
		if err != nil {
			body = "error: " + err.Error()
		}
		if _, err := fmt.Fprintf(w, "== %s\n%s\n\n", p, body); err != nil {
			return err
		}
	}
	return nil
}

// docs prints ComponentDocs for one component as JSON, what `brick docs
// -json` prints per component.
func docs(w io.Writer, eng *brick.Engine, name string) error {
	all, err := eng.ComponentDocs()
	if err != nil {
		return err
	}
	for _, d := range all {
		if d.Name != name {
			continue
		}
		b, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "== ComponentDocs(%q)\n%s\n\n", name, b)
		return err
	}
	return fmt.Errorf("no component %q", name)
}

// check compiles the broken folder and prints each diagnostic with its
// code, so html-balance shows by name.
// ds:def id=check-rfdkhfjq
func check(ctx context.Context, w io.Writer, eng *brick.Engine) error {
	rep, err := eng.CompileAll(ctx, "broken")
	if err != nil {
		return err
	}
	for _, d := range rep.Diagnostics {
		if _, err := fmt.Fprintf(w, "== %s [%s]\n", d, d.Code); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(w)
	return err
}

// inherit shows a rebuild: a new engine serves only the stylesheets of the
// pages it has rendered, so a page cached before the rebuild would lose its
// CSS. After InheritAssets it serves the old engine's files too. It prints
// the asset files the new engine serves before and after.
// ds:def id=inherit-4dryrmmk
func inherit(ctx context.Context, w io.Writer, dir string) error {
	oldEng, err := newEngine(dir, brick.Prod)
	if err != nil {
		return err
	}
	if _, err := oldEng.Render(ctx, "content/island.html", nil); err != nil {
		return err
	}
	// A rebuild after a theme edit: a fresh snapshot that has rendered
	// nothing yet, while a cache still holds pages the old one made.
	newEng, err := newEngine(dir, brick.Prod)
	if err != nil {
		return err
	}
	before := assetNames(newEng)
	newEng.InheritAssets(oldEng)
	after := assetNames(newEng)
	_, err = fmt.Fprintf(w, "== InheritAssets\nbefore: %v\nafter:  %v\n\n", before, after)
	return err
}

// assetNames lists the asset file names an engine serves, sorted by fs.
func assetNames(eng *brick.Engine) []string {
	var out []string
	// A walk error just shortens the list; the demo prints what it could see.
	_ = fs.WalkDir(eng.Assets(), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// stats shows what a render did: the page render counts the components it
// drew and the islands it captured, and Fill adds each island's own when
// given WithStats. A copy of Result.Stats is passed, because a cached
// Result is shared and Fill never changes it.
// ds:def id=stats-5hymhkv8
func stats(ctx context.Context, w io.Writer, eng *brick.Engine) error {
	res, err := eng.RenderChunks(ctx, "content/island.html", nil)
	if err != nil {
		return err
	}
	st := res.Stats
	if _, err := eng.Fill(ctx, res, brick.WithStats(&st)); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "== WithStats\npage render: %d components, %d islands\nafter Fill:  %d components, top %v\n\n", res.Stats.Components, res.Stats.Islands, st.Components, st.Top(1))
	return err
}

// run prints every section; testdata/output.txt holds the result.
func run(ctx context.Context, w io.Writer, dir string) error {
	ct := &counter{}
	prod, err := newEngine(dir, brick.Prod, ct)
	if err != nil {
		return err
	}
	if err := renderAll(ctx, w, prod, "content/index.html", "content/hyphen.html", "content/hyphen-bad.html", "content/widget.html", "content/typo.html", "content/typo-ok.html", "content/spin.html"); err != nil {
		return err
	}
	if err := docs(w, prod, "Card"); err != nil {
		return err
	}
	dev, err := newEngine(dir, brick.Dev)
	if err != nil {
		return err
	}
	if err := check(ctx, w, dev); err != nil {
		return err
	}
	if err := renderAll(ctx, w, dev, "content/loc.html"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "== hooks\ncompiled %d files, rendered %d pages\n\n", len(ct.compiled), len(ct.rendered)); err != nil {
		return err
	}
	if err := inherit(ctx, w, dir); err != nil {
		return err
	}
	return stats(ctx, w, prod)
}

// main prints what run writes. The tests call run with a buffer instead of
// stdout, so the output they pin is this program's real output.
func main() {
	dir := flag.String("theme", "theme", "the project directory")
	flag.Parse()
	if err := run(context.Background(), os.Stdout, *dir); err != nil && !errors.Is(err, io.EOF) {
		log.Fatal(err)
	}
}
