package scriptstarlark

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Schemes an allowlist entry may name. https is the default; http must be
// listed explicitly per host (D9).
const (
	schemeHTTPS = "https"
	schemeHTTP  = "http"
)

// maxRedirects bounds redirect chains, each hop re-checked.
const maxRedirects = 5

// origin is one allowlist entry: scheme + host + port.
type origin struct {
	scheme string
	host   string
	port   string
}

// parseAllow validates Options.HTTPAllow. Wildcards are refused: they would
// let DNS rebinding reach hosts nobody approved (D9).
func parseAllow(list []string) ([]origin, error) {
	var out []origin
	for _, s := range list {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" || (u.Path != "" && u.Path != "/") || u.User != nil || u.RawQuery != "" {
			return nil, fmt.Errorf("scriptstarlark: HTTPAllow %q: write an origin such as https://api.example.com", s)
		}
		if u.Scheme != schemeHTTPS && u.Scheme != schemeHTTP {
			return nil, fmt.Errorf("scriptstarlark: HTTPAllow %q: scheme must be https or http", s)
		}
		if strings.Contains(u.Host, "*") {
			return nil, fmt.Errorf("scriptstarlark: HTTPAllow %q: wildcards are not allowed", s)
		}
		o, err := originOf(u)
		if err != nil {
			return nil, fmt.Errorf("scriptstarlark: HTTPAllow %q: %w", s, err)
		}
		out = append(out, o)
	}
	return out, nil
}

// originOf canonicalizes a URL's origin: lowercase host, no trailing dot,
// explicit port. Non-ASCII hosts are refused rather than IDNA-mapped, so a
// look-alike host can never match an allowlist entry.
func originOf(u *url.URL) (origin, error) {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	for _, r := range host {
		if r >= utf8.RuneSelf {
			return origin{}, fmt.Errorf("host %q is not ASCII; write its punycode form", u.Hostname())
		}
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{schemeHTTPS: "443", schemeHTTP: "80"}[u.Scheme]
	}
	return origin{scheme: u.Scheme, host: host, port: port}, nil
}

// allowed checks a URL against the allowlist.
func (e *Engine) allowed(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.User != nil {
		return nil, errors.New("credentials in URLs are refused")
	}
	o, err := originOf(u)
	if err != nil {
		return nil, err
	}
	for _, a := range e.allow {
		if a == o {
			return u, nil
		}
	}
	return nil, fmt.Errorf("%s://%s is not in HTTPAllow", u.Scheme, net.JoinHostPort(o.host, o.port))
}

// The builtin names of the `http` capability. One handler serves both and
// branches on the name, so the name that registers a builtin is the name
// the handler compares.
const (
	httpGetName     = "http.get"
	httpGetJSONName = "http.get_json"
)

// newHTTP builds the `http` capability:
//
//	http.get(url)       → {status, headers, text}
//	http.get_json(url)  → the parsed JSON
func newHTTP(e *Engine) starlark.Value {
	return starlarkstruct.FromStringDict(starlark.String("http"), starlark.StringDict{
		"get":      starlark.NewBuiltin(httpGetName, e.httpGet),
		"get_json": starlark.NewBuiltin(httpGetJSONName, e.httpGet),
	})
}

// client returns the configured client with every redirect re-checked.
func (e *Engine) client() *http.Client {
	c := &http.Client{}
	if e.opts.HTTPClient != nil {
		cp := *e.opts.HTTPClient
		c = &cp
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("more than %d redirects", maxRedirects)
		}
		_, err := e.allowed(req.URL.String())
		return err
	}
	return c
}

func (e *Engine) httpGet(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var raw string
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &raw); err != nil {
		return nil, err
	}
	u, err := e.allowed(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	req, err := http.NewRequestWithContext(threadCtx(t), http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, e.opts.HTTPMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	if int64(len(body)) > e.opts.HTTPMaxBytes {
		return nil, fmt.Errorf("%s: the response is over HTTPMaxBytes (%d)", b.Name(), e.opts.HTTPMaxBytes)
	}
	if b.Name() == httpGetJSONName {
		if resp.StatusCode >= http.StatusBadRequest {
			return nil, fmt.Errorf("%s: %s returned %s", b.Name(), u, resp.Status)
		}
		dec := json.NewDecoder(strings.NewReader(string(body)))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		return toStarlark(normalizeJSON(v), 0, nil)
	}
	headers := starlark.NewDict(len(resp.Header))
	names := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if err := headers.SetKey(starlark.String(strings.ToLower(k)), starlark.String(resp.Header.Get(k))); err != nil {
			return nil, err
		}
	}
	d := starlark.NewDict(3)
	for k, v := range map[string]starlark.Value{"status": starlark.MakeInt(resp.StatusCode), "headers": headers, "text": starlark.String(string(body))} {
		if err := d.SetKey(starlark.String(k), v); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// normalizeJSON turns json.Number into int64 or float64 (D1).
func normalizeJSON(v any) any {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		for k, val := range x {
			x[k] = normalizeJSON(val)
		}
	case []any:
		for i, val := range x {
			x[i] = normalizeJSON(val)
		}
	}
	return v
}
