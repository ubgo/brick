// Command examples serves, checks and exports the demo site in ./site, which
// uses every brick feature, wired the way a real host would: an os.Root
// confined project, the site layer (routes, .Site.Pages, markdown pages,
// collections, cache, live reload), Starlark with one host capability,
// goldmark, bluemonday, islands filled per request, actions and navigation.
//
//	go run . -serve :8080      serve with live reload (try /?as=Sara)
//	go run . -out build        write the static site to build/ (needs BRICK_SECRET: stateful pages carry sealed state the action server opens)
//	go run . -check            report every problem in the site
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/examples/demosite"
	"github.com/ubgo/brick/site"
)

func main() {
	siteDir := flag.String("site", "site", "the project directory")
	out := flag.String("out", "", "write the static site into this directory")
	serve := flag.String("serve", "", "serve the site on this address, e.g. :8080")
	check := flag.Bool("check", false, "report every problem in the site")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *siteDir, *out, *serve, *check); err != nil {
		log.Fatal(err)
	}
}

// newSite builds the demo exactly as the tests do (package demosite).
func newSite(ctx context.Context, siteDir string, dev bool) (*site.Site, error) {
	keys, err := demosite.StateKeys(os.LookupEnv, dev, log.Printf)
	if err != nil {
		return nil, err
	}
	return demosite.New(ctx, demosite.Options{Dir: siteDir, Dev: dev, StateKeys: keys})
}

func run(ctx context.Context, siteDir, out, serve string, check bool) error {
	s, err := newSite(ctx, siteDir, serve != "")
	if err != nil {
		return err
	}
	switch {
	case check:
		diags, err := s.Check(ctx)
		if err != nil {
			return err
		}
		failed := false
		for _, d := range diags {
			fmt.Println(d)
			failed = failed || d.Severity == brick.SeverityError
		}
		if failed {
			return errors.New("the site has errors")
		}
		fmt.Printf("ok: %d pages\n", len(s.Routes()))
		return nil
	case out != "":
		rep, err := s.Export(ctx, out)
		if errors.Is(err, site.ErrExportNeedsKeys) {
			return fmt.Errorf("%w; set %s to the key the action server uses (brick keygen prints one)", err, demosite.SecretEnv)
		}
		if err != nil {
			return err
		}
		fmt.Printf("wrote %d pages, %d static files and %d assets to %s\n", rep.Pages, rep.Static, rep.Assets, out)
		return nil
	case serve != "":
		log.Printf("serving on %s", serve)
		return s.ListenAndServe(ctx, serve)
	}
	return errors.New("pass -serve ADDR, -out DIR or -check")
}
