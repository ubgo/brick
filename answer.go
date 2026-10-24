package brick

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/ubgo/brick/internal/weburl"
)

// A loader's answer is what it says instead of data: this record does not
// exist (ErrNotFound), go elsewhere (*Redirect), or you may not see this
// (*HTTPError). Each fails the render as an error, unwrappable with
// errors.Is / errors.As, so a Go host with its own router acts on it the
// same way the site layer does. An answer is not a fault: it is never
// reported to OnError hooks, and an error boundary never contains it,
// because a fallback in place of a page the visitor may not see would
// still send that page's surroundings with status 200.
//
// Where the answer comes from decides who it is for. A page's own render
// sees no request in the site layer (its Result is cached and shared), so
// a page or layout loader's answer is the same for every visitor: a moved
// slug, a removed post. An answer that depends on the visitor — signed out,
// not an admin — can only come from an island's loader, which Engine.Fill
// runs per response; the site sends that answer with Cache-Control:
// no-store and never caches it. A failed render is never cached either, so
// no answer of either kind is ever served from the page cache.

// Redirect statuses a loader may answer with. 301 is left out on purpose:
// browsers cache it with no expiry, and a POST-to-GET rewrite is what 303
// says explicitly; 308 is the permanent one.
const (
	// DefaultRedirectStatus is 303 See Other: the safe default for "go and
	// sign in", since the next request is always a GET.
	DefaultRedirectStatus = http.StatusSeeOther
)

// redirectStatuses are the statuses NewRedirect accepts.
var redirectStatuses = []int{http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect}

// Redirect is a loader's answer that sends the visitor to URL with Status
// (Starlark: brick.redirect(url, status=303)). URL is a path on this site
// (/login) or an absolute http(s) URL; which hosts an absolute URL may name
// is decided when the answer becomes a response (Engine.RedirectLocation),
// because only then is the request's own host known.
type Redirect struct {
	URL    string
	Status int
}

// NewRedirect checks a redirect's shape: status is 302, 303, 307 or 308
// (0 means DefaultRedirectStatus), and target is a path that is not
// protocol-relative or an http(s) URL, with no backslash or control
// character. A script backend calls it, so a bad redirect fails where the
// loader wrote it.
func NewRedirect(target string, status int) (*Redirect, error) {
	if status == 0 {
		status = DefaultRedirectStatus
	}
	if !slices.Contains(redirectStatuses, status) {
		return nil, fmt.Errorf("redirect status %d: use 302, 303, 307 or 308", status)
	}
	if err := checkRedirectURL(target, "", nil, true, true); err != nil {
		return nil, fmt.Errorf("redirect: %w", err)
	}
	return &Redirect{URL: target, Status: status}, nil
}

// Error describes the redirect for logs; the site layer answers it with
// a Location header, never with this text.
func (r *Redirect) Error() string {
	return fmt.Sprintf("brick: redirect (%d) to %s", r.Status, r.URL)
}

// HTTPError is a loader's answer with an error status
// (Starlark: brick.error(status, message="")): 403 for a page the visitor
// may not see, 410 for one that was removed. Message is shown on the
// site's error page; it is author text, so it must not carry data the
// visitor should not read.
type HTTPError struct {
	Status  int
	Message string
}

// The error statuses an HTTPError or an ActionError may carry: any 4xx
// or 5xx. A 1xx-3xx is not an error answer, and a status past 599 is not
// HTTP, so both are refused (NewHTTPError) or replaced (ActionStatus).
const (
	minErrorStatus = 400
	maxErrorStatus = 599
)

// isErrorStatus reports whether status is a 4xx or 5xx.
func isErrorStatus(status int) bool {
	return status >= minErrorStatus && status <= maxErrorStatus
}

// NewHTTPError checks the status: a 4xx or 5xx. 404 is accepted and is
// the same answer as ErrNotFound (errors.Is matches it).
func NewHTTPError(status int, message string) (*HTTPError, error) {
	if !isErrorStatus(status) {
		return nil, fmt.Errorf("error status %d: use a 4xx or 5xx status", status)
	}
	return &HTTPError{Status: status, Message: message}, nil
}

// Error describes the error answer for logs: the status, its standard
// text and Message when one was given.
func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("brick: %d %s: %s", e.Status, http.StatusText(e.Status), e.Message)
	}
	return fmt.Sprintf("brick: %d %s", e.Status, http.StatusText(e.Status))
}

// Is makes brick.error(404) the same answer as brick.not_found().
func (e *HTTPError) Is(target error) bool {
	return target == ErrNotFound && e.Status == http.StatusNotFound
}

// IsAnswer reports whether err is a loader's answer (ErrNotFound,
// *Redirect, *HTTPError) rather than a fault.
func IsAnswer(err error) bool {
	if err == nil {
		return false
	}
	var rd *Redirect
	var he *HTTPError
	return errors.Is(err, ErrNotFound) || errors.As(err, &rd) || errors.As(err, &he)
}

// RedirectLocation is the open-redirect lock for a loader's redirect: a
// path on this site always passes; an absolute URL passes when it names
// the request's own host or one of Options.RedirectHosts, the typed opt-in
// for external targets (an OAuth provider). r may be nil (a static
// export), and then only paths and RedirectHosts pass. The site layer
// calls it; a Go host with its own router calls it before writing
// Location, so loader data can never send a visitor off-site.
func (e *Engine) RedirectLocation(r *http.Request, rd *Redirect) (string, error) {
	host := ""
	if r != nil {
		host = r.Host
	}
	if err := checkRedirectURL(rd.URL, host, e.opts.RedirectHosts, true, false); err != nil {
		return "", fmt.Errorf("brick: redirect: %w", err)
	}
	return rd.URL, nil
}

// checkRedirectURL is the shared open-redirect check
// behind action effects and loader redirects: a path on this site, or an
// absolute http(s) URL on host — or, when others is set, on one of hosts.
// javascript:, data:, protocol-relative //host and backslash tricks all
// fail. shapeOnly accepts any http(s) host: the check a loader's redirect
// gets when it is made, before the request's host is known.
func checkRedirectURL(raw, host string, hosts []string, others, shapeOnly bool) error {
	if raw == "" {
		return errors.New("the URL is empty")
	}
	if strings.ContainsAny(raw, "\\\x00\t\r\n") {
		return fmt.Errorf("URL %q has a backslash or a control character", raw)
	}
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || !weburl.Is(u) || u.User != nil {
		return fmt.Errorf("URL %q must be a path (/x) or an http(s) URL", raw)
	}
	if shapeOnly {
		return nil
	}
	h := strings.ToLower(u.Host)
	if host != "" && h == strings.ToLower(host) {
		return nil
	}
	if others && slices.ContainsFunc(hosts, func(o string) bool { return strings.EqualFold(o, h) }) {
		return nil
	}
	if others {
		return fmt.Errorf("URL %q names host %q, which is neither this site nor in Options.RedirectHosts", raw, u.Host)
	}
	return fmt.Errorf("URL %q is not on this site (history URLs are same-origin only)", raw)
}

// LoaderHeaders lists the response headers a loader may set
// (Starlark: brick.header(name, value)): caching and indexing hints that
// describe the page. Everything else is refused, notably Set-Cookie (a
// page's render is cached and shared, so a cookie set there would reach
// every visitor; sessions belong to the host, before the page renders),
// the hop-by-hop headers (Connection, Keep-Alive, Transfer-Encoding,
// Upgrade, TE, Trailer, Proxy-*), which only the server may set,
// Content-Type and Content-Length, which the engine owns, and Location,
// which is brick.redirect's job.
func LoaderHeaders() []string { return slices.Clone(loaderHeaders) }

// loaderHeaders is LoaderHeaders' list, in canonical form.
var loaderHeaders = []string{"Cache-Control", "Content-Language", "Expires", "Last-Modified", "Link", "Vary", "X-Robots-Tag"}

// maxHeaderValue bounds one header value: a hint, never a payload.
const maxHeaderValue = 4096

// headerSinkKey finds the render's header collector on a context.
type headerSinkKey struct{}

// headerSink collects a render's loader headers. Islands may render on one
// response from several goroutines in the future, so it locks.
type headerSink struct {
	mu sync.Mutex
	h  http.Header
}

// WithResponseHeaders gives a render the header map its loaders' brick.header
// calls land in. Engine.Render collects a page's own headers into
// Result.Headers by itself; pass this to Engine.Fill so an island's loader
// headers reach this response (the site layer does). h is written while
// Fill runs and must not be read until it returns.
func WithResponseHeaders(ctx context.Context, h http.Header) context.Context {
	return context.WithValue(ctx, headerSinkKey{}, &headerSink{h: h})
}

// SetHeader is what a script backend calls for a loader's
// brick.header(name, value). The name must be one of LoaderHeaders and the
// value a single line; a refused header fails the loader wherever it runs.
// Outside a render that collects headers — an action's re-render, whose
// response is JSON — a valid header is dropped, because there is no page
// response to carry it.
func SetHeader(ctx context.Context, name, value string) error {
	canon := textproto.CanonicalMIMEHeaderKey(name)
	if !slices.Contains(loaderHeaders, canon) {
		return fmt.Errorf("header %q is not one a loader may set (brick.LoaderHeaders: %s)", name, strings.Join(loaderHeaders, ", "))
	}
	if len(value) > maxHeaderValue {
		return fmt.Errorf("header %s: the value is %d bytes, over %d", canon, len(value), maxHeaderValue)
	}
	for _, c := range []byte(value) {
		if (c < 0x20 && c != '\t') || c == 0x7f {
			return fmt.Errorf("header %s: the value has a control character", canon)
		}
	}
	s, _ := ctx.Value(headerSinkKey{}).(*headerSink)
	if s == nil || s.h == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.h.Set(canon, value)
	return nil
}
