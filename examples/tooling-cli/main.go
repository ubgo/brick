// Command tooling-cli is the brick command used the way a CI job uses it:
// as a gate on the theme's formatting and as a record of which build ran.
// It shells out to a brick binary, since the command is its own module and
// is not meant to be imported.
//
//	go run ./tooling-cli                       the gate, with `brick` on PATH
//	go run ./tooling-cli -brick /path/to/brick -theme tooling-cli/theme
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Version is what `brick version -json` prints. The fields are the build's
// own, so a log line can say which binary formatted or checked a theme.
type Version struct {
	Version  string `json:"version"`
	Revision string `json:"revision"`
	Time     string `json:"time"`
	Modified bool   `json:"modified"`
	Go       string `json:"go"`
}

// run executes bin with args and returns its standard output. A nonzero
// exit is an error that carries the command's standard error, because
// brick prints the reason there (for example `brick fmt: 1:4: unclosed {{`).
// ds:def id=run-7ud3w9q6
func run(bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// unformatted lists the files in dir that `brick fmt` would change. The
// command's own exit status is 0 when files differ, so the gate must read
// the list: `brick fmt -l` fails only for a file it cannot read or parse.
// ds:def id=unformatted-wr6hqwd5
func unformatted(bin, dir string) ([]string, error) {
	out, err := run(bin, "fmt", "-l", dir)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// formatDiff is the unified diff `brick fmt -d` prints for one file, the
// text a CI log shows next to "this file is not formatted".
// ds:def id=formatdiff-9ckqaedk
func formatDiff(bin, file string) (string, error) {
	return run(bin, "fmt", "-d", file)
}

// binaryVersion asks the binary which build it is (`brick version -json`).
// ds:def id=binaryversion-v8vhcnpd
func binaryVersion(bin string) (Version, error) {
	out, err := run(bin, "version", "-json")
	if err != nil {
		return Version{}, err
	}
	var v Version
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return Version{}, fmt.Errorf("brick version -json: %w", err)
	}
	return v, nil
}

// errUnformatted is the gate's failure: the theme has files `brick fmt -w`
// would rewrite.
var errUnformatted = errors.New("files need `brick fmt -w`")

// main reports the binary's version and exits non-zero when any theme file is
// unformatted, so the program works as a CI gate. The tests drive the same
// binaryVersion and unformatted helpers against a real brick binary.
func main() {
	bin := flag.String("brick", "brick", "the brick binary")
	theme := flag.String("theme", "tooling-cli/theme", "the theme folder to check")
	flag.Parse()
	v, err := binaryVersion(*bin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("brick", v.Version)
	files, err := unformatted(*bin, *theme)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, f := range files {
		fmt.Println("not formatted:", f)
	}
	if len(files) > 0 {
		fmt.Fprintln(os.Stderr, errUnformatted)
		os.Exit(1)
	}
	fmt.Println("formatted")
}
