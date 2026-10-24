package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

// brick version reports what the binary was built from, read from the
// build info Go embeds (debug.ReadBuildInfo), so it cannot drift from a
// hand-kept constant: the module version for `go install …@v1.2.3`, and the
// VCS revision for a build from a checkout.

const versionUsage = `brick version — the version this binary was built from

Usage:
  brick version [-json]

It prints the module version (a pseudo-version for a build from a
checkout, "(devel)" when Go could not stamp one), the
VCS revision and its time when the build recorded them, "modified" when
the checkout had uncommitted changes, and the Go version.

Flags:
  -json   print the same as JSON
`

// The build settings that carry the VCS state (go help buildvcs).
const (
	settingRevision = "vcs.revision"
	settingTime     = "vcs.time"
	settingModified = "vcs.modified"
	// shortRevision is how much of a revision the text form shows, as git
	// abbreviates a hash.
	shortRevision = 12
	// unknownVersion is the version of a binary built without module
	// information (go build of a single file, or a stripped build).
	unknownVersion = "unknown"
)

// versionInfo is brick version -json.
type versionInfo struct {
	// Version is the module version: v1.2.3, a pseudo-version, or (devel).
	Version string `json:"version"`
	// Revision, Time and Modified are the VCS state, empty when the build
	// did not record it (go install of a module, -buildvcs=false).
	Revision string `json:"revision,omitempty"`
	Time     string `json:"time,omitempty"`
	Modified bool   `json:"modified,omitempty"`
	// Go is the toolchain that built the binary.
	Go string `json:"go"`
}

// buildVersion reads the binary's build info.
func buildVersion() versionInfo {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return versionInfo{Version: unknownVersion, Go: unknownVersion}
	}
	v := versionInfo{Version: bi.Main.Version, Go: bi.GoVersion}
	if v.Version == "" {
		v.Version = unknownVersion
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case settingRevision:
			v.Revision = s.Value
		case settingTime:
			v.Time = s.Value
		case settingModified:
			v.Modified = s.Value == "true"
		}
	}
	return v
}

// String is the text form: brick v1.2.3 (rev 0123456789ab, modified,
// 2026-10-12T…) go1.25.0. Go's own version for a dirty checkout already
// ends in +dirty, so the revision is not marked a second time.
func (v versionInfo) String() string {
	var b strings.Builder
	b.WriteString("brick " + v.Version)
	if v.Revision != "" {
		rev := v.Revision
		if len(rev) > shortRevision {
			rev = rev[:shortRevision]
		}
		b.WriteString(" (rev " + rev)
		if v.Modified {
			b.WriteString(", modified")
		}
		if v.Time != "" {
			b.WriteString(", " + v.Time)
		}
		b.WriteString(")")
	}
	b.WriteString(" " + v.Go)
	return b.String()
}

// versionCmd is brick version.
func versionCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("brick version", flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.Usage = func() { fmt.Fprint(stderr, versionUsage) }
	asJSON := fl.Bool(flagJSON, false, "print JSON")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "brick version: takes no arguments\n\n%s", versionUsage)
		return exitUsage
	}
	v := buildVersion()
	if *asJSON {
		return writeJSON(stdout, stderr, v)
	}
	fmt.Fprintln(stdout, v)
	return exitOK
}
