package site

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"sort"
	"time"

	"github.com/ubgo/brick"
)

// instancesEndpoint is the Dev-only instance inspector,
// under the site's base like the live-reload stream.
const instancesEndpoint = "_brick/instances"

// inspectMax bounds how many instances one inspector page lists: a shared
// store (Redis, SQL) may hold every visitor's instances, and the page is a
// glance for the developer, not an export.
const inspectMax = 500

// sessionPrefix is how many hex digits of the session hash the inspector
// shows: enough to tell sessions apart on a dev machine, too few to be the
// hash a store is keyed on. The session id itself is never stored at all
// (Instance.Session is already its SHA-256).
const sessionPrefix = 8

// instanceRow is one inspector line. It deliberately carries no state,
// props or handle: the page shows what is alive and how big it is, never
// what it holds, so leaving it open on a shared screen leaks nothing.
type instanceRow struct {
	Store   string
	Name    string
	Path    string
	Page    string
	Age     time.Duration
	Size    int
	Version int64
	Session string
}

// stores names every state store of the site: the default one (the
// host's, or the site's own when the host gave none) and the named ones.
func (s *Site) stores() map[string]brick.StateStore {
	out := map[string]brick.StateStore{}
	if st := s.opts.Engine.States; st != nil {
		out[brick.DefaultStoreName] = st
	} else if s.states != nil {
		out[brick.DefaultStoreName] = s.states
	}
	for name, st := range s.opts.Engine.StateStores {
		out[name] = st
	}
	return out
}

// listInstances walks every store that is a brick.Lister. ok is false when
// none is (a sealed store keeps nothing to list), which the handler turns
// into a 404 rather than an empty table that would read as "no instances".
func (s *Site) listInstances(ctx context.Context, now time.Time) (rows []instanceRow, truncated, ok bool, err error) {
	stores := s.stores()
	names := make([]string, 0, len(stores))
	for name := range stores {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []error
	for _, name := range names {
		l, isLister := stores[name].(brick.Lister)
		if !isLister {
			continue
		}
		e := l.Each(brick.WithStateScope(ctx, brick.StateScope{Store: name}), func(inst *brick.Instance) bool {
			if len(rows) == inspectMax {
				truncated = true
				return false
			}
			rows = append(rows, rowOf(name, inst, now))
			return true
		})
		// brick.Bytes is always a Lister and reports ErrUnsupported over a
		// byte store that cannot list (Prune does the same).
		if errors.Is(e, errors.ErrUnsupported) {
			continue
		}
		ok = true
		if e != nil {
			errs = append(errs, e)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Age < rows[j].Age })
	return rows, truncated, ok, errors.Join(errs...)
}

// rowOf reduces an instance to what the inspector may show.
func rowOf(store string, inst *brick.Instance, now time.Time) instanceRow {
	sess := inst.Session
	if len(sess) > sessionPrefix {
		sess = sess[:sessionPrefix]
	}
	age := time.Duration(0)
	if !inst.Issued.IsZero() {
		age = now.Sub(inst.Issued).Truncate(time.Second)
	}
	return instanceRow{
		Store:   store,
		Name:    inst.Name,
		Path:    inst.Path,
		Page:    inst.PagePath,
		Age:     age,
		Size:    approxSize(inst),
		Version: inst.Version,
		Session: sess,
	}
}

// approxSize is the JSON size of what the instance carries between
// actions (state, props, context, slots): close to what a store writes,
// without depending on the store's own codec.
func approxSize(inst *brick.Instance) int {
	n := 0
	for _, v := range []any{inst.State, inst.Props, inst.Ctx, inst.Slots} {
		if b, err := json.Marshal(v); err == nil {
			n += len(b)
		}
	}
	return n
}

var inspectPage = template.Must(template.New("inspect").Parse(`<!doctype html><html><head><meta charset="utf-8"><title>Instances · brick</title>
<style>body{font:13px system-ui,sans-serif;margin:16px;color:#222}table{border-collapse:collapse}th,td{padding:3px 10px;border-bottom:1px solid #ddd;text-align:left}td.n{text-align:right;font-variant-numeric:tabular-nums}code{font-size:12px}p{color:#666}</style></head><body>
<h1>Live instances</h1>
<p>{{ len .Rows }} instance(s){{ if .Truncated }}, the first {{ .Max }} shown{{ end }}. Dev only: state values and session ids are never shown.</p>
{{ with .Err }}<p role="alert">Store error: {{ . }}</p>{{ end }}
<table><thead><tr><th>Store</th><th>Component</th><th>File</th><th>Page</th><th>Age</th><th>Size (bytes)</th><th>Version</th><th>Session</th></tr></thead><tbody>
{{ range .Rows }}<tr><td>{{ .Store }}</td><td>{{ .Name }}</td><td><code>{{ .Path }}</code></td><td><code>{{ .Page }}</code></td><td class="n">{{ .Age }}</td><td class="n">{{ .Size }}</td><td class="n">{{ .Version }}</td><td><code>{{ if .Session }}{{ .Session }}…{{ else }}none{{ end }}</code></td></tr>
{{ end }}</tbody></table></body></html>`))

// inspect serves the instance inspector. It is reached only in Dev (the
// Handler's switch), and answers 404 when no store can list.
func (s *Site) inspect(w http.ResponseWriter, r *http.Request) {
	rows, truncated, ok, err := s.listInstances(r.Context(), time.Now())
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", htmlContentType)
	w.Header().Set("Cache-Control", "no-store")
	data := map[string]any{"Rows": rows, "Truncated": truncated, "Max": inspectMax, "Err": err}
	if e := inspectPage.Execute(w, data); e != nil {
		s.opts.Logger.Error("site: instances page", "err", e)
	}
}
