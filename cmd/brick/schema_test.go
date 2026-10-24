package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

func TestSchemaCLI(t *testing.T) {
	code, out, errs := cli(t, "schema")
	if code != exitOK || out != string(brick.FrontmatterSchema()) {
		t.Fatalf("schema: %d %q %s", code, out[:min(len(out), 80)], errs)
	}
	var s map[string]any
	if err := json.Unmarshal([]byte(out), &s); err != nil || s["$id"] != brick.FrontmatterSchemaID {
		t.Fatalf("not the schema: %v", err)
	}

	path := filepath.Join(t.TempDir(), "fm.schema.json")
	if code, out, errs := cli(t, "schema", "-o", path); code != exitOK || !strings.Contains(out, path) {
		t.Fatalf("schema -o: %d %s %s", code, out, errs)
	}
	if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, brick.FrontmatterSchema()) {
		t.Fatalf("written file differs: %v", err)
	}

	if code, _, errs := cli(t, "schema", "-h"); code != exitOK || !strings.Contains(errs, "Examples:") {
		t.Fatalf("-h: %d %s", code, errs)
	}
	if code, _, _ := cli(t, "schema", "extra"); code != exitUsage {
		t.Fatalf("an argument: exit %d, want %d", code, exitUsage)
	}
	if code, _, _ := cli(t, "schema", "-x"); code != exitUsage {
		t.Fatalf("an unknown flag: exit %d, want %d", code, exitUsage)
	}
	if code, _, errs := cli(t, "schema", "-o", filepath.Join(t.TempDir(), "missing", "fm.json")); code != exitFail || errs == "" {
		t.Fatalf("unwritable -o: exit %d %q, want %d", code, errs, exitFail)
	}
}
