# State in the URL, the session and the server

## What it is and when you want it

A stateful component's fields live in one instance on the server. Six frontmatter keys under `brick:` change where a field also lives and who may write it: `query:` puts fields in the address bar (a filtered list you can share, and Back undoes the filter), `params:` runs a hook when the URL changes them, `remember:` keeps fields per session after the instance is gone (a column layout), `temporary:` keeps a field out of the store (a page of rows), and `locked:` and `writable:` decide which fields the browser may write at all. You want them when a plain `state:` field is in the wrong place: lost on reload, stored when it should not be, or writable by a client that should not touch it.

## Before you start

- Read [stateful components and actions](stateful-components.md) first: this page builds on `state:`, actions, `$set` and `$reset`.
- `remember:` needs `Options.Remember` (a `Rememberer`) and a session (`Options.Sessions` plus `brick.WithSession` on the render). `query:` needs the request's query on the render: the site layer passes it, a Go host passes `brick.WithQuery(r.URL.Query())`.
- The example is [`examples/interactivity-url-state`](../../../examples/interactivity-url-state/).

## Step 1 — Wire the store, the Rememberer and the session

`brick.NewMemoryRemember()` serves development and tests; in production, a `Rememberer` is your own store (`Load` and `Save` by `RememberKey{Session, Path, Key, PagePath}`). `Sessions` is how brick learns who is asking, for both renders and actions. From `examples/interactivity-url-state/main.go`:

<!-- ds:block id=newapp-cfau8wfz title="examples/interactivity-url-state/main.go" -->
**examples/interactivity-url-state/main.go** · [`examples/interactivity-url-state/main.go:55-75`](../../../examples/interactivity-url-state/main.go#L55-L75)

```go
func newApp(dir string) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		Remember:  brick.NewMemoryRemember(),
		Sessions:  brick.SessionFunc(sessionOf),
	})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng}, nil
}
```
<!-- /ds:block hash=6d97be -->

## Step 2 — Fill the page with the session and the query

The page is rendered once; each request fills its stateful islands with the visitor's session and the URL's query. The query seeds every `query:` field of a new instance through the same checks as `$set`:

<!-- ds:block id=app-index-7uzt29qc title="examples/interactivity-url-state/main.go" -->
**examples/interactivity-url-state/main.go** · [`examples/interactivity-url-state/main.go:89-107`](../../../examples/interactivity-url-state/main.go#L89-L107)

```go
func (a *app) index(w http.ResponseWriter, r *http.Request) {
	a.once.Do(func() { a.page, a.err = a.eng.RenderChunks(r.Context(), page, nil) })
	if a.err != nil {
		http.Error(w, a.err.Error(), http.StatusInternalServerError)
		return
	}
	sid, _ := sessionOf(r)
	if sid == "" {
		sid = rand.Text()
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: sid, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
	html, err := a.eng.Fill(r.Context(), a.page, brick.WithSession(sid), brick.WithQuery(r.URL.Query()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}
```
<!-- /ds:block hash=964031 -->

## Step 3 — Put fields in the URL and respond when it changes

`query:` maps each field to a URL parameter: `q` is written as `search`, `page` uses `history: push` so Back undoes it (the default is `replace`), and `tab: {}` takes the defaults. `params: on_params` runs when the instance is created (after the URL seeded it) and whenever the URL changes the component's parameters; here it clamps the page. `updated: tidy` sends the page back to 1 when the search changes. `remember: [columns]` keeps the column choice per session, and `temporary: [rows]` shows the rows on the render after `fill` and resets them before the save. `components/Catalog.html`:

<!-- snippet: examples/interactivity-url-state/theme/components/Catalog.html -->
```html
---
state:
  q: ""
  tab: all
  page: 1
  columns: [title, price]
  rows: []
brick:
  query:
    q: {as: search}
    tab: {}
    page: {history: push}
  params: on_params
  updated: tidy
  remember: [columns]
  temporary: [rows]
---
<brick:script>
LAST_PAGE = 5

def on_params(state, props, params):
    if state["page"] > LAST_PAGE:
        return dict(state, page = LAST_PAGE)
    return state

def tidy(state, props, paths):
    page = 1 if "q" in paths else state["page"]
    return dict(state, q = state["q"].strip().lower(), page = page)

def fill(state, props):
    rows = [{"id": i, "title": "Item %d" % i} for i in range(1, 4)]
    return dict(state, rows = rows)

def wide(state, props):
    return dict(state, columns = ["title", "price", "stock"])
</brick:script>
<section class="catalog">
  <input id="q" brick:model.debounce.300ms="q" value="{{ .State.q }}">
  <a id="books" href="?tab=books">Books</a>
  <button id="next" brick:click='$set("page", {{ add .State.page 1 }})'>Next</button>
  <button id="clear" brick:click='$reset("q", "page")'>Clear</button>
  <button id="load" brick:click="fill">Load rows</button>
  <button id="wide" brick:click="wide">Wide columns</button>
  <p id="where">q={{ .State.q }} tab={{ .State.tab }} page={{ .State.page }} columns={{ join "," .State.columns }} rows={{ len .State.rows }}</p>
</section>
```

What happens, in order:

```text
open /?search=acme&page=9          → q=acme page=5 (params: clamped it; the address bar keeps 9 on a first load)
click Next                         → page=6; the answer carries a query effect and the runtime calls pushState
click <a href="?tab=books">        → a patch: no page fetch; the runtime sends $params with the URL's values
click Clear                        → q and page back to their state: values; both parameters leave the URL
click Wide columns, open a new tab → columns=title,price,stock again (remember:, same session)
click Load rows                    → rows=3 on this render; rows=[] in the saved instance (temporary:)
```

A same-path link whose changed parameters all belong to components on the page is a **patch**: `pushState`, and only the owning components re-render through the `$params` built-in. A link that also changes a parameter nobody owns, or the path, is a normal navigation.

## Step 4 — Decide who may write each field

Every `state:` field is client-writable by default, because that is what `$set` and `brick:model` are for. `locked:` lists the fields only the server writes. Here `editing_id` is the row the server opened, so a `$set` of it is refused while `note` still rides along as a deferred set. `components/RowEditor.html`:

<!-- snippet: examples/interactivity-url-state/theme/components/RowEditor.html -->
```html
---
state:
  editing_id: 0
  note: ""
brick:
  locked: [editing_id]
---
<brick:script>
def open(state, props, id):
    return dict(state, editing_id = id)
</brick:script>
<div class="editor">
  <button id="edit7" brick:click="open(7)">Edit row 7</button>
  <input id="note" brick:model.defer="note" value="{{ .State.note }}">
  <p id="editing">editing={{ .State.editing_id }} note={{ .State.note }}</p>
</div>
```

`writable:` is the opposite default: only the listed fields (plus `query:` and upload fields) are client-writable. A `writable:` list alone closes one component under an open engine; `Options.LockState` (or `lockState: true` in `brick.yaml`) closes every component, and `brick: {lock_state: false}` reopens one. `components/Profile.html`:

<!-- snippet: examples/interactivity-url-state/theme/components/Profile.html -->
```html
---
state:
  title: ""
  role: viewer
brick:
  writable: [title]
---
<div class="profile">
  <input id="title" brick:model="title" value="{{ .State.title }}">
  <p id="who">title={{ .State.title }} role={{ .State.role }}</p>
</div>
```

The gate covers every client write: `$set`, `$toggle`, `$reset`, deferred `sets` and `brick:model`. Actions, `init:`, `updated:`, `params:` and remembered values are server writes and are never refused.

## Try it

```sh
cd examples/interactivity-url-state && go run .
```

Open http://localhost:8091/?search=Acme&page=9. The test drives each step over the real action endpoint: `cd examples && go test ./interactivity-url-state/` prints `ok  github.com/ubgo/brick/examples/interactivity-url-state`. What it sends and gets back:

```text
GET /?search=acme&page=9
← <p id="where">q=acme tab=all page=5 columns=title,price rows=0</p>

POST {"action":"$set","args":["page",2]}
← effects: [{"kind":"query","data":{"set":{"page":"2"}}}]   (no "replace": page is history: push)

POST {"action":"$params","sets":[["tab","books"],["page","2"],["q","acme"]]}
← html: …q=acme tab=books page=2…

POST RowEditor {"action":"$set","args":["editing_id",9]}
← 422 brick: field value refused: locked field: state.editing_id is locked (brick: {locked:}); only the server writes it

POST Profile {"action":"$set","args":["role","admin"]}
← 422 brick: field value refused: locked field: state.role is closed to the client (Options.LockState or brick: {lock_state:}; list it in brick: {writable:} to allow it); only the server writes it
```

## Pitfalls

- **A bad URL value is ignored, never an error.** `?page=abc` goes through the same gate as `$set`, so it is dropped with a Dev warning and the field keeps its declared value. Validate in `params:` if a value must be in range, as `on_params` does.
- **`$params` sets are keyed by field, not by parameter.** The runtime sends `["q","acme"]` for `?search=acme`; a host or test that posts `$params` by hand uses the field names.
- **A correction on the first load does not reach the address bar.** `params:` clamps `page=9` to 5 in the component, but the URL keeps `9` until the next change; a correction during `$params` replaces the history entry, so Back never lands on the uncorrected address.
- **`remember:` without a Rememberer is a compile error**, and with no session nothing is loaded or saved. The test shows a second session gets the declared `columns`.
- **A locked field cannot be in `query:` or `uploads:`**, because the URL and an upload are client writes; listing a field in both `locked:` and `writable:` is a compile error. `brick check` reports a `brick:model` bound to a locked field, since the server would refuse it on every keystroke.
- **Locking stops a client choosing another row, not a stale one.** Keep scoping by owner in the action (`where id = ? and owner = ?`); `errors.Is(err, brick.ErrLockedField)` is also an `ErrBadValue`, so existing 422 handling applies.
- **`temporary:` fields must be top-level `state:` fields**, and the next action starts from the declared value, so reload them in the action that needs them.

## Related

- [Stateful components and actions](stateful-components.md), [events and modifiers](events-and-modifiers.md), [navigation](navigation.md) (a patch link is a navigation the component answers).
- The guide's [server state section](../../guide.md#server-state--replies-first-values-the-url-and-memory).
- [Attribute reference](../../reference/attributes.md), [frontmatter reference](../../reference/frontmatter.md), [feature index](../../reference/features.md#stateful-components-and-actions).
- Example: [`examples/interactivity-url-state`](../../../examples/interactivity-url-state/).
