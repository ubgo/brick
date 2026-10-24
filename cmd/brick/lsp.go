package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/cmd/brick/internal/lsp"
	"github.com/ubgo/brick/kit"
)

// brick lsp is a language server on stdin/stdout. Its answers come from the
// same engine calls as the CLI's (site.Check for diagnostics, ComponentDocs
// for components, FrontmatterSchema for frontmatter keys), so the editor
// and `brick check` never disagree.

const lspUsage = `brick lsp — a language server for brick .html files, on stdin/stdout

Usage:
  brick lsp

An editor starts it and speaks the Language Server Protocol over stdin and
stdout; it is not run by hand. It answers:

  diagnostics   what brick check reports, per file, after each open and save
  completion    component tags after < (<Card> and <x-card>), a component's
                props inside its tag, values of a oneof prop, and
                frontmatter keys and values inside the --- block
  hover         a component's props and slots, a prop's type and doc, a
                frontmatter key's meaning
  definition    a component tag jumps to the component's file
  references    every call of a component across the site's pages, layouts
                and components (<Card> and <x-card>, open and close tags),
                from a tag or from the component's own file; on a prop,
                its declaration, its uses in the component and every place
                it is passed, from any of those positions
  rename        a component: every tag that calls it, the use: paths that
                name its file, and the file itself (the editor must accept
                file renames; a kit component or a taken name is refused);
                a prop: its declaration, its uses in the component and
                every call site (refused, naming the sites, on a clash or
                a site it cannot follow, such as brick:spread)
  formatting    the document laid out as brick fmt writes it

The site is the nearest folder above the file holding brick.yaml or
site.yaml, else the workspace folder. Editors: editors/vscode in the brick
repository is a VS Code client; any LSP client can run "brick lsp".
`

// lspCmd runs the language server until the client exits.
func lspCmd(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("brick lsp", flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.Usage = func() { fmt.Fprint(stderr, lspUsage) }
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "brick lsp: takes no arguments\n\n%s", lspUsage)
		return exitUsage
	}
	// serverInfo carries the same module version as brick version, read
	// from the build info, so an editor reports the binary it really runs.
	srv, err := lsp.NewServer(lsp.NewConn(stdin, stdout), lsp.Options{
		Project: cliProject{log: stderr}, Schema: brick.FrontmatterSchema(), Version: buildVersion().Version, Log: stderr,
		Format: formatDocument, RawComponents: lspRawComponents(stderr),
	})
	if err != nil {
		fmt.Fprintln(stderr, "brick:", err)
		return exitFail
	}
	if err := srv.Serve(ctx); err != nil {
		fmt.Fprintln(stderr, "brick lsp:", err)
		return exitFail
	}
	return exitOK
}

// lspRawComponents is the raw-body test references and rename scan with:
// the same raw components brick fmt knows (the kit's and the site's,
// vendored libraries included), so a tag inside a raw body is never taken
// for a call.
func lspRawComponents(log io.Writer) func(root string) func([]byte) lsp.RawTest {
	return func(root string) func([]byte) lsp.RawTest {
		raws := newRawSet()
		if err := raws.addFS(kit.FS(), ""); err != nil {
			fmt.Fprintln(log, "brick lsp: kit components:", err)
		}
		if err := raws.addSite(root); err != nil {
			fmt.Fprintln(log, "brick lsp: raw components of", root+":", err)
		}
		return func(src []byte) lsp.RawTest { return raws.forFile(src) }
	}
}

// cliProject opens a site the way `brick check` does. Each call opens the
// site afresh: a call follows a save, and the files changed.
type cliProject struct{ log io.Writer }

// Components lists the site's components (what `brick docs` lists).
func (p cliProject) Components(ctx context.Context, root string) ([]brick.ComponentDoc, error) {
	s, _, err := open(ctx, root, siteFlags{log: p.log})
	if err != nil {
		return nil, err
	}
	return s.Engine().ComponentDocs()
}

// Check reports what `brick check` reports.
func (p cliProject) Check(ctx context.Context, root string) ([]brick.Diagnostic, error) {
	s, _, err := open(ctx, root, siteFlags{log: p.log})
	if err != nil {
		return nil, err
	}
	return s.Check(ctx)
}
