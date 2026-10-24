// Command components-errors shows what brick does when something goes
// wrong: a <brick:boundary> that turns a failing loader into its fallback,
// the same page in Dev and Prod mode, an OnError hook that still hears the
// contained error, components chosen from data with <brick:render>, and a
// whole-folder check (Engine.CompileAll, what `brick check` runs) that
// reports a typo'd tag and an unclosed element with file and line.
//
//	go run .          prints the page in both modes, the hook's log, then the check report
//	go test .         checks the exact output
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/ubgo/brick"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// page is the page rendered in both modes.
const page = "content/index.html"

// brokenDir holds pages that are wrong on purpose, kept out of content/
// so the page above still renders.
const brokenDir = "broken"

// errorLog is an ErrorHook: brick calls OnError for every error, including
// one a boundary contained, so a fallback on the page never hides a fault
// from the host. A real host sends it to its error tracker.
// ds:def id=errorlog-x2sk245m
type errorLog struct {
	mu    sync.Mutex
	lines []string
}

// OnError records one error. Concurrent renders may call it at once,
// hence the lock.
// ds:def id=errorlog-onerror-qy4a2wad
func (l *errorLog) OnError(_ context.Context, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, err.Error())
}

// newEngine builds an engine in the given mode with the hook attached.
// ds:def id=newengine-cv2eejr5
func newEngine(dir string, mode brick.Mode, hook *errorLog) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{FS: root.FS(), Scripting: sc, Mode: mode, Hooks: []any{hook}})
}

// check compiles every page under brokenDir and returns the diagnostics,
// one per line, as `brick check` prints them.
// ds:def id=check-8rd5dky7
func check(ctx context.Context, eng *brick.Engine) (string, error) {
	rep, err := eng.CompileAll(ctx, brokenDir)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(rep.Diagnostics))
	for _, d := range rep.Diagnostics {
		lines = append(lines, d.String())
	}
	return strings.Join(lines, "\n"), nil
}

// run renders the page in Prod and then Dev mode into w, each followed by
// what the hook heard, then the check report: what `go run .` prints and
// testdata/output.txt holds.
func run(ctx context.Context, w io.Writer, dir string) error {
	for _, m := range []struct {
		name string
		mode brick.Mode
	}{{"Prod", brick.Prod}, {"Dev", brick.Dev}} {
		hook := &errorLog{}
		eng, err := newEngine(dir, m.mode, hook)
		if err != nil {
			return err
		}
		html, err := eng.Render(ctx, page, nil)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "== %s in %s mode\n%s\n== OnError heard\n%s\n\n", page, m.name, html, strings.Join(hook.lines, "\n")); err != nil {
			return err
		}
		if m.mode == brick.Dev {
			report, err := check(ctx, eng)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "== CompileAll(%q)\n%s\n", brokenDir, report); err != nil {
				return err
			}
		}
	}
	return nil
}

// main prints what run writes. The tests call run with a buffer instead of
// stdout, so the output they pin is this program's real output.
func main() {
	dir := flag.String("theme", "theme", "the project directory")
	flag.Parse()
	if err := run(context.Background(), os.Stdout, *dir); err != nil {
		log.Fatal(err)
	}
}
