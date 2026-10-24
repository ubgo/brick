package site

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ubgo/brick"
)

// errorStem is the page the site renders for a loader's error status
// (brick.error): content/error.html or .md. Like 404.html it is not a page
// of the site. It reads the answer as .Ctx.error.status and
// .Ctx.error.message.
const errorStem = "error"

// errorCtxKey is the Provide key the error page reads.
const errorCtxKey = "error"

// noStore is sent with every per-visitor answer (an island's redirect or
// error status), so no shared cache keeps one visitor's answer for another.
const noStore = "private, no-store"

// visitorAnswer marks a loader answer from a per-request page (v22): the
// page's own loader saw this visitor, as an island's does, so answer sends
// it with no-store too. It wraps the answer, so brick.IsAnswer and
// errors.As still find the redirect or status inside.
type visitorAnswer struct{ error }

func (v visitorAnswer) Unwrap() error { return v.error }

// answer writes a loader's answer (brick.IsAnswer) as the response and
// reports true; any other error is left to the caller. perRequest marks an
// answer from Fill — an island, which saw this visitor — and adds
// Cache-Control: no-store. A redirect passes Engine.RedirectLocation (a
// path on this site, this request's host, or Options.RedirectHosts); a
// path target gets the site's BasePath, as every site URL does. A refused
// target is a 500, logged, never followed.
func (s *Site) answer(w http.ResponseWriter, r *http.Request, err error, perRequest bool) bool {
	if !brick.IsAnswer(err) {
		return false
	}
	if perRequest {
		w.Header().Set("Cache-Control", noStore)
	}
	visitorNoStore(w, err)
	var rd *brick.Redirect
	var he *brick.HTTPError
	switch {
	case errors.As(err, &rd):
		loc, lerr := s.current().eng.RedirectLocation(r, rd)
		if lerr != nil {
			s.opts.Logger.Error("site: refused a loader's redirect", "url", r.URL.Path, "err", lerr)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return true
		}
		http.Redirect(w, r, s.basePrefixed(loc), rd.Status)
	case errors.As(err, &he) && he.Status != http.StatusNotFound:
		s.errorPage(w, r, he)
	default:
		s.notFound(w, r)
	}
	return true
}

// visitorNoStore sends Cache-Control: no-store when err is a per-request
// page's loader answer, including its not-found, which callers write
// through the 404 page before answer sees it: one visitor's "not found"
// must not be kept by a shared cache for the next visitor.
func visitorNoStore(w http.ResponseWriter, err error) {
	var va visitorAnswer
	if errors.As(err, &va) {
		w.Header().Set("Cache-Control", noStore)
	}
}

// answerStatus is the HTTP status of a loader's answer.
func answerStatus(err error) int {
	var rd *brick.Redirect
	var he *brick.HTTPError
	switch {
	case errors.As(err, &rd):
		return rd.Status
	case errors.As(err, &he):
		return he.Status
	}
	return http.StatusNotFound
}

// basePrefixed puts the site's BasePath in front of a path target; an
// absolute URL is left alone.
func (s *Site) basePrefixed(loc string) string {
	if strings.HasPrefix(loc, "/") {
		return s.opts.BasePath + loc
	}
	return loc
}

// RenderError renders content/error.html (or .md) for a loader's error
// status, with the answer in .Ctx.error ({status, message}). ok is false
// when the project has no error page.
func (s *Site) RenderError(ctx context.Context, he *brick.HTTPError) (*brick.Result, bool, error) {
	st := s.current()
	for _, ext := range []string{extHTML, extMD} {
		p := path.Join(s.opts.ContentDir, errorStem+ext)
		if exists(s.opts.FS, p) {
			info := map[string]any{"status": he.Status, "message": he.Message}
			res, err := s.renderRoute(ctx, st, Route{URL: "/" + errorStem + "/", Path: p, Markdown: ext == extMD}, brick.Provide(errorCtxKey, info))
			return res, true, err
		}
	}
	return nil, false, nil
}

// errorPage sends a loader's error status through the project's error
// page, or as plain text (the message, else the status text) without one.
func (s *Site) errorPage(w http.ResponseWriter, r *http.Request, he *brick.HTTPError) {
	start := time.Now()
	res, ok, err := s.RenderError(r.Context(), he)
	if !ok {
		msg := he.Message
		if msg == "" {
			msg = http.StatusText(he.Status)
		}
		http.Error(w, msg, he.Status)
		return
	}
	if err != nil && res == nil {
		s.opts.Logger.Error("site: error page", "err", err)
	}
	s.writePage(w, r, res, err, he.Status, pageTiming{start: start, errorPage: true})
}

// setHeaders copies a loader's response headers onto w. They come from
// brick.SetHeader, so only brick.LoaderHeaders() names reach here.
func setHeaders(w http.ResponseWriter, h http.Header) {
	for k, vs := range h {
		w.Header()[k] = append([]string(nil), vs...)
	}
}

// exportRedirect is the file a static export writes for a page whose
// loader redirects: a static host cannot send a 3xx, so the page is a
// meta refresh to the target, with a canonical link and a plain link for
// a browser that ignores the refresh. Nothing of the page itself is
// written, so a protected page's content never reaches the export. Only a
// path or a host in brick.Options.RedirectHosts passes (there is no
// request host at build time).
func (s *Site) exportRedirect(st *state, u string, rd *brick.Redirect) ([]byte, error) {
	loc, err := st.eng.RedirectLocation(nil, rd)
	if err != nil {
		return nil, fmt.Errorf("site: export %s: %w", u, err)
	}
	loc = s.basePrefixed(loc)
	esc := template.HTMLEscapeString(loc)
	return []byte(`<!doctype html><html><head><meta charset="utf-8"><title>Redirecting…</title><meta name="robots" content="noindex"><link rel="canonical" href="` + esc + `"><meta http-equiv="refresh" content="0; url=` + esc + `"></head><body><a href="` + esc + `">` + esc + `</a></body></html>` + "\n"), nil
}
