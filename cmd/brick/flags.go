package main

import (
	"flag"
	"fmt"
	"io"
)

// The commands that open a site each get their own flag set: a flag one
// command takes and another ignores (build -addr, serve -check) is a
// usage error, so a typo or a misplaced flag never runs silently with the
// default, and "brick <command> -h" lists only what that command reads.

// siteArgs are the flags of the site commands; each command registers
// only the ones it reads.
type siteArgs struct {
	asJSON, drafts, checkOnly bool
	out, addr, locales, base  string
	keep                      string
}

// The flags the site commands share, by name.
const (
	flagJSON    = "json"
	flagDrafts  = "drafts"
	flagLocales = "locales"
	flagBase    = "base"
	flagKeep    = "keep"
	flagOut     = "o"
	flagAddr    = "addr"
	flagCheck   = "check"
)

// siteCommand is one command that opens a site: its synopsis, what it
// does, and the flags it takes, in usage order.
type siteCommand struct {
	synopsis, summary string
	flags             []string
}

// siteCommands are the commands siteFlagSet knows.
var siteCommands = map[string]siteCommand{
	"check": {"brick check [-json] [-drafts] [-locales en,fr] [dir]",
		"Report every problem in the site at once; exit 1 when any is an error.",
		[]string{flagJSON, flagDrafts, flagLocales}},
	"routes": {"brick routes [-json] [-drafts] [-locales en,fr] [dir]",
		"List every URL and the file behind it, then the dynamic pages with their parameters.",
		[]string{flagJSON, flagDrafts, flagLocales}},
	"build": {"brick build [-json] [-drafts] [-o dir] [-base /path] [-keep /a/,/b/] [-locales en,fr] [dir]",
		"Write the static site. A page with a stateful component needs BRICK_SECRET.",
		[]string{flagJSON, flagDrafts, flagOut, flagBase, flagKeep, flagLocales}},
	"serve": {"brick serve [-addr :8080] [-base /path] [-keep /a/,/b/] [-locales en,fr] [dir]",
		"Run the dev server: drafts, live reload and /_brick/play/<Component>.",
		[]string{flagAddr, flagBase, flagKeep, flagLocales}},
	"vendor": {"brick vendor [-check] [-json] [dir]",
		"Fetch remote use: imports and libraries into brick_modules/ and pin them in brick.lock.",
		[]string{flagCheck, flagJSON}},
	"docs": {"brick docs [-json] [dir]", "", []string{flagJSON}},
}

// siteFlagSet returns the flag set of cmd, bound to a, or false when cmd
// is not a site command.
func siteFlagSet(cmd string, a *siteArgs, stderr io.Writer) (*flag.FlagSet, bool) {
	sc, ok := siteCommands[cmd]
	if !ok {
		return nil, false
	}
	fl := flag.NewFlagSet("brick "+cmd, flag.ContinueOnError)
	fl.SetOutput(stderr)
	for _, name := range sc.flags {
		switch name {
		case flagJSON:
			fl.BoolVar(&a.asJSON, flagJSON, false, "print JSON")
		case flagDrafts:
			fl.BoolVar(&a.drafts, flagDrafts, false, "include draft pages")
		case flagLocales:
			fl.StringVar(&a.locales, flagLocales, "", "comma-separated locales with their own URLs, default first (e.g. en,fr)")
		case flagBase:
			fl.StringVar(&a.base, flagBase, "", "host the site under this path, e.g. /docs (links and assets are rewritten)")
		case flagKeep:
			fl.StringVar(&a.keep, flagKeep, "", "comma-separated root-absolute path prefixes the -base rewrite leaves alone, e.g. /engine/ (links to a sibling site)")
		case flagOut:
			fl.StringVar(&a.out, flagOut, defaultOut, "the output directory")
		case flagAddr:
			fl.StringVar(&a.addr, flagAddr, defaultAddr, "the listen address")
		case flagCheck:
			fl.BoolVar(&a.checkOnly, flagCheck, false, "verify brick_modules/ against brick.lock without fetching")
		default:
			panic("brick: siteCommands names an unknown flag " + name)
		}
	}
	if cmd == "docs" {
		fl.Usage = func() { fmt.Fprint(stderr, docsUsage) }
		return fl, true
	}
	fl.Usage = func() {
		fmt.Fprintf(stderr, "Usage:\n  %s\n\n%s\ndir defaults to the current directory.\n\nFlags:\n", sc.synopsis, sc.summary)
		fl.PrintDefaults()
	}
	return fl, true
}
