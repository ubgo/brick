package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Each command takes only its own flags: one meant for another command is
// a usage error, never silently ignored, and -h lists only the command's
// own. Every invocation the docs and Taskfile use still parses.
func TestCLIFlagsPerCommand(t *testing.T) {
	dir := project(t, "content/index.html", "<p>hi</p>")
	for _, args := range [][]string{
		{"build", "-addr", ":1", dir},
		{"build", "-check", dir},
		{"serve", "-check", dir},
		{"serve", "-json", dir},
		{"serve", "-o", "x", dir},
		{"vendor", "-o", "x", dir},
		{"vendor", "-drafts", dir},
		{"check", "-base", "/x", dir},
		{"check", "-o", "x", dir},
		{"routes", "-addr", ":1", dir},
		{"docs", "-drafts", dir},
		{"docs", "-locales", "en", dir},
	} {
		if code, _, errs := cli(t, args...); code != exitUsage || !strings.Contains(errs, "not defined") {
			t.Errorf("%v: exit %d, want a usage error: %s", args, code, errs)
		}
	}
	// What each command's help lists, and what it must not.
	for cmd, want := range map[string][2][]string{
		"check":  {{"-json", "-drafts", "-locales"}, {"-addr", "-base", "-keep", "-check", "-o "}},
		"routes": {{"-json", "-drafts", "-locales"}, {"-addr", "-base", "-check"}},
		"build":  {{"-json", "-drafts", "-o ", "-base", "-keep", "-locales"}, {"-addr", "-check"}},
		"serve":  {{"-addr", "-base", "-keep", "-locales"}, {"-json", "-drafts", "-check", "-o "}},
		"vendor": {{"-check", "-json"}, {"-addr", "-base", "-drafts", "-o "}},
	} {
		code, _, errs := cli(t, cmd, "-h")
		if code != exitOK || !strings.Contains(errs, "brick "+cmd+" [") {
			t.Fatalf("%s -h: %d %s", cmd, code, errs)
		}
		flags := errs[strings.Index(errs, "Flags:"):]
		for _, f := range want[0] {
			if !strings.Contains(flags, "  "+f) {
				t.Errorf("%s -h lacks %s:\n%s", cmd, f, flags)
			}
		}
		for _, f := range want[1] {
			if strings.Contains(flags, "  "+f) {
				t.Errorf("%s -h lists %s:\n%s", cmd, f, flags)
			}
		}
	}
	// Every flagged invocation in the README, the guide and the Taskfile.
	out := filepath.Join(t.TempDir(), "public")
	for _, args := range [][]string{
		{"check", "-json", dir},
		{"check", "-drafts", "-locales", "en", dir},
		{"routes", "-json", "-drafts", dir},
		{"docs", "-json", dir},
		{"build", "-o", out, "-base", "/docs", "-locales", "en", "-drafts", dir},
		{"vendor", "-check", dir},
	} {
		if code, _, errs := cli(t, args...); code != exitOK {
			t.Errorf("%v: exit %d: %s", args, code, errs)
		}
	}
	// The usage lists every command and every flag.
	_, usageText, _ := cli(t, "-h")
	for _, s := range []string{"check", "routes", "build", "serve", "vendor", "docs", "new", "fmt", "keygen", "schema", "lsp", "version",
		"-json", "-drafts", "-locales", "-base", "-o dir", "-addr", "-check", "-lazy", "-dir", "-state", "-client", "-island"} {
		if !strings.Contains(usageText, s) {
			t.Errorf("usage lacks %q", s)
		}
	}
}

// build -json and vendor -json print one JSON report on stdout alone.
func TestCLIJSONReports(t *testing.T) {
	dir := project(t, "content/index.html", "<p>hi</p>", "static/robots.txt", "ok")
	out := filepath.Join(t.TempDir(), "public")
	code, stdout, errs := cli(t, "build", "-json", "-o", out, dir)
	var br buildReport
	if code != exitOK || json.Unmarshal([]byte(stdout), &br) != nil || br.Pages != 1 || br.Static != 1 || br.Out != out {
		t.Fatalf("build -json: %d %q %s", code, stdout, errs)
	}
	for _, args := range [][]string{{"vendor", "-json", dir}, {"vendor", "-check", "-json", dir}} {
		code, stdout, errs := cli(t, args...)
		var vr vendorReport
		if code != exitOK || json.Unmarshal([]byte(stdout), &vr) != nil || vr.Files == nil || vr.Checked != (len(args) == 4) {
			t.Fatalf("%v: %d %q %s", args, code, stdout, errs)
		}
	}
}

// brick version prints the build's version; -json the same as fields.
func TestVersion(t *testing.T) {
	code, out, _ := cli(t, "version")
	if code != exitOK || !strings.HasPrefix(out, "brick ") || !strings.Contains(out, " go1.") {
		t.Fatalf("version: %d %q", code, out)
	}
	code, out, _ = cli(t, "version", "-json")
	var v versionInfo
	if code != exitOK || json.Unmarshal([]byte(out), &v) != nil || v.Version == "" || !strings.HasPrefix(v.Go, "go1.") {
		t.Fatalf("version -json: %d %q", code, out)
	}
	if code, _, _ := cli(t, "version", "extra"); code != exitUsage {
		t.Fatalf("version extra: %d", code)
	}
	got := versionInfo{Version: "v1.2.3", Revision: "0123456789abcdef", Time: "2026-10-12T00:00:00Z", Modified: true, Go: "go1.25.0"}.String()
	if want := "brick v1.2.3 (rev 0123456789ab, modified, 2026-10-12T00:00:00Z) go1.25.0"; got != want {
		t.Fatalf("text form %q, want %q", got, want)
	}
}

// A per-request page is not written by build, and build says so in both
// outputs instead of leaving a silent hole in the site.
func TestBuildReportsSkipped(t *testing.T) {
	dir := project(t, "content/index.html", "<p>hi</p>", "content/me.html", "---\nbrick: {per_request: true}\n---\n<p>me</p>")
	code, stdout, errs := cli(t, "build", "-o", filepath.Join(t.TempDir(), "public"), dir)
	if code != exitOK || !strings.Contains(stdout, "skipped /me/: brick: {per_request: true}") {
		t.Fatalf("build: %d %q %s", code, stdout, errs)
	}
	code, stdout, errs = cli(t, "build", "-json", "-o", filepath.Join(t.TempDir(), "public"), dir)
	var br buildReport
	if code != exitOK || json.Unmarshal([]byte(stdout), &br) != nil || br.Pages != 1 || !slices.Equal(br.Skipped, []string{"/me/"}) {
		t.Fatalf("build -json: %d %q %s", code, stdout, errs)
	}
}
