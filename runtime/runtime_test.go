package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRuntime runs runtime_test.mjs, which drives the runtime against a
// small fake DOM. Skipped when node is not installed.
func TestRuntime(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	out, err := exec.Command(node, "runtime_test.mjs").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "runtime ok") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestEmbedded(t *testing.T) {
	if !strings.Contains(string(JS), "window.brick") {
		t.Fatal("runtime.js is not embedded")
	}
}

// browserTestTimeout bounds one jsdom test file.
const browserTestTimeout = 60 * time.Second

// TestBrowser runs every test/*_test.mjs in jsdom (navigation, actions).
// It needs the test-only dependencies (task js:deps); without them it is
// skipped, never faked.
func TestBrowser(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	if _, err := os.Stat("test/node_modules/jsdom"); err != nil {
		t.Skip("jsdom is not installed: run `task js:deps`")
	}
	files, err := filepath.Glob("test/*_test.mjs")
	if err != nil || len(files) == 0 {
		t.Fatalf("no browser tests found: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			// A hung test (an open timer) must fail, never stall the suite.
			ctx, cancel := context.WithTimeout(context.Background(), browserTestTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, node, filepath.Base(f))
			cmd.Dir = "test"
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), " ok") {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}

func TestNavEmbedded(t *testing.T) {
	if !strings.Contains(string(Nav), "brick.navigate") {
		t.Fatal("nav.js is not embedded")
	}
}
