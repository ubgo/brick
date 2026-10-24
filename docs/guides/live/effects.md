# Effects — an action that asks the browser to do something

## What it is and when you want it

An action normally answers with one thing: its component's new HTML. Effects are the other things an action can ask the browser to do once that HTML is in: show a message, change the page title, go to another page, download a file, change the address bar, or tell other components that something happened. You want them whenever "re-render this component" is not the whole answer, for example "Deleted. Go back to the list." Effects are data, never code: the server names a kind and its fields, and the browser runtime decides how to carry it out.

## Before you start

- You have a stateful component with an action (`state:` in its frontmatter and a `<brick:script>` block). If not, start with [Actions — a button that changes server state](../../guide.md#actions--a-button-that-changes-server-state).
- The engine has `Options.Scripting` (here Starlark, `contrib/script-starlark`) and `Options.States`.
- The page has `{{ assets }}`, so the browser runtime is linked.
- The example for this page is [examples/live-effects-events](../../../examples/live-effects-events/). Run it from the `examples` folder.

## Step 1 — Give the engine scripting and a state store

Actions run in a script and keep their component's state between requests, so the engine needs both; the middleware lines are used in Steps 3 and 4, `RedirectHosts` and `MaxEffectBytes` in Step 5, and `Runtime` in [Retries and runtime settings](browser-runtime.md).

<!-- ds:block id=newapp-hh3jnj8r title="examples/live-effects-events/main.go" -->
**examples/live-effects-events/main.go** · [`examples/live-effects-events/main.go:68-90`](../../../examples/live-effects-events/main.go#L68-L90)

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
		FS:               root.FS(),
		Scripting:        sc,
		States:           brick.NewMemoryStates(0, 0),
		ActionMiddleware: []brick.ActionMiddleware{audit, celebrate},
		RedirectHosts:    []string{payHost},
		MaxEffectBytes:   maxEffectBytes,
		Runtime:          runtimeConfig,
	})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng}, nil
}
```
<!-- /ds:block hash=764fde -->

## Step 2 — Record effects in the action

Each `brick.<effect>(…)` call in an action adds one effect to the answer; the action still returns its new state as usual.

`examples/live-effects-events/theme/components/Inbox.html`

<!-- snippet: examples/live-effects-events/theme/components/Inbox.html -->
```html
---
state:
  messages:
    - {id: 1, subject: Welcome}
    - {id: 2, subject: Invoice}
    - {id: 3, subject: "Lunch?"}
---
<brick:script>
def archive(state, props, id):
    left = [m for m in state["messages"] if m["id"] != id]
    if len(left) == len(state["messages"]):
        brick.flash("Message %d was already archived." % id, "warning")
        return state
    brick.dispatch("message-archived", {"id": id})
    brick.flash("Archived message %d." % id, "success")
    brick.title("Inbox (%d)" % len(left))
    return dict(state, messages = left)

def dismiss(state, props):
    brick.clear_flash()
    return state

def read(state, props, id):
    brick.navigate("/messages/%d" % id)
    return state

def unread_only(state, props):
    brick.url("/?filter=unread", replace = True)
    return state

def export(state, props):
    brick.download("/export.csv", "inbox.csv")
    return state

def sign_out(state, props):
    brick.redirect("/login")
    return state

def pay(state, props):
    brick.redirect("https://pay.example/checkout")
    return state

def leave(state, props):
    brick.redirect("https://elsewhere.example/")
    return state

def bulk(state, props):
    brick.dispatch("big", {"blob": "x" * 5000})
    return state
</brick:script>
<brick:client>
// The browser side of the host's own effect kind: the server sends data,
// never code, and this handler decides what x-confetti looks like.
brick.effect("x-confetti", (effect) => {
  document.documentElement.dataset.confetti = String(effect.data.pieces);
});

// The second custom kind, recorded from Go by the audit middleware.
brick.effect("x-audit", (effect) => {
  document.documentElement.dataset.audited = effect.data.action;
});

// Tell the host which build of the page sent each action. A hook may add
// headers; changing Content-Type, Accept or a Brick-* header throws.
brick.hook("request", (r) => {
  r.headers["X-Page-Build"] = "inbox-1";
});

// A 422 is a script failure the component already shows; a 500 or more
// comes from the host, so start that component over from a fresh render.
brick.on("action-error", (d) => {
  if (d.root && d.status >= 500) brick.reloadComponent(d.root);
});

// The runtime retried an action after a 503 or 429.
brick.on("action-retry", (d) => {
  console.info("retrying", d.action, "in", d.wait, "ms");
});
</brick:client>
<section class="inbox">
  <ul>{{ range .State.messages }}
    <li>{{ .subject }}
      <button brick:click="read({{ .id }})">Read</button>
      <button brick:click="archive({{ .id }})">Archive</button></li>{{ end }}
  </ul>
  <button brick:click="unread_only">Unread only</button>
  <button brick:click="export">Export</button>
  <button brick:click="dismiss">Dismiss messages</button>
  <button brick:click="sign_out">Sign out</button>
  <button brick:click="pay">Pay</button>
  <button brick:click="leave">Leave</button>
  <button brick:click="bulk">Bulk</button>
</section>
```

The page puts the Inbox (and the Badge from [Events](events.md)) on one page:

`examples/live-effects-events/theme/content/index.html`

<!-- snippet: examples/live-effects-events/theme/content/index.html -->
```html
<!doctype html>
<html>
<head><title>Inbox (3)</title></head>
<body>
<Badge />
<Inbox />
{{ assets }}
</body>
</html>
```

Every effect you can record from Starlark:

| Call | What the browser does |
|---|---|
| `brick.flash(message, level = "info")` | Shows a one-shot message. `level` is `neutral`, `info`, `success`, `warning` or `danger`. A kit `<Toaster>` shows it as a toast; without one the runtime shows a small polite message. A flash survives a following `navigate`, `redirect` or `submit`. |
| `brick.clear_flash(level = None)` | Dismisses the flashes on screen: all of them, or one level. A flash recorded later in the same action still shows. |
| `brick.title(text)` | Sets `document.title`. The last call wins. |
| `brick.navigate(url)` | Goes to `url` without a full reload when navigation is on (`Options.Navigation`), otherwise a full load. |
| `brick.redirect(url)` | A full page load of `url`. |
| `brick.download(url, filename = None)` | Downloads the file your app serves at `url`. An action never streams the file itself. |
| `brick.url(url, replace = False)` | Changes the address bar (a new history entry, or the current one replaced) without a request. Same origin only. |
| `brick.dispatch(name, data = None, to = None)` | Sends an event to other components on the page. See [Events](events.md). |
| `brick.rows(…)`, `brick.region(…)`, `brick.submit(form)` | Edit a list by rows, apply a region of the render, or hand a form to the browser's own submit. See [Rows, regions, streams and native submit](rows-and-regions.md). |
| `brick.transition(types = "")` | Runs this answer's morph in a view transition with those types, or skips it with `"none"`. |
| `brick.publish(topic)`, `brick.start_job(…)`, `brick.cancel_job(id)` | Nothing in this browser: they run on the server once the action succeeded. See [Push and presence](push-and-presence.md) and [Background jobs](background-jobs.md). |

In Go the same effects are methods of `*brick.Effects`, which `brick.EffectsFrom(ctx)` returns inside an action: `Flash`, `ClearFlash`, `Title`, `Navigate`, `Redirect`, `Download`, `URL`, `Dispatch`, `Rows`, `Region`, `Submit`, `Transition`, and `Add` for any `brick.Effect`.

The browser runs the effects in the order they were recorded, after the morph. `redirect`, `navigate` and `submit` leave the page, so they always run last, and when an action records more than one of them only the last one runs.

## Step 3 — Add your own effect kind from Go

A host can add effects of its own, for things only the app knows about. A custom kind starts with `x-` (`brick.CustomEffectPrefix`), so no future engine kind can collide with it. This middleware adds `x-confetti` to every action that archived a message:

<!-- ds:block id=celebrate-3xgk747s title="examples/live-effects-events/main.go" -->
**examples/live-effects-events/main.go** · [`examples/live-effects-events/main.go:117-131`](../../../examples/live-effects-events/main.go#L117-L131)

```go
func celebrate(next brick.ActionFunc) brick.ActionFunc {
	return func(ctx context.Context, call *brick.ActionCall) (*brick.ActionResult, error) {
		res, err := next(ctx, call)
		if err != nil {
			return nil, err
		}
		archived := slices.ContainsFunc(res.Effects, func(ef brick.Effect) bool {
			return ef.Kind == brick.EffectDispatch && ef.Name == eventArchived
		})
		if archived {
			res.Effects = append(res.Effects, brick.Effect{Kind: confettiKind, Data: map[string]any{"pieces": 40}})
		}
		return res, nil
	}
}
```
<!-- /ds:block hash=8c9f4f -->

The browser side is the `<brick:client>` block at the top of `Inbox.html` (Step 2): `brick.effect("x-confetti", fn)` registers the handler. `fn` receives the effect object as the server sent it (`effect.data.pieces` here). An effect kind with no handler is logged with `console.warn` and emitted on `brick.on("effect", …)`, so nothing fails silently. `brick.effect("flash", fn)` replaces how every flash is shown, the same way.

A middleware runs around every action (`Options.ActionMiddleware`, outermost first). After `next` it sees the effects in `res.Effects` and may add, remove or rewrite them; whatever it leaves there is what the browser receives.

## Step 4 — Record an effect from Go with `EffectsFrom`

A script is not the only place that records effects. Inside any action, Go code finds the action's collector with `brick.EffectsFrom(ctx)` and calls the same methods the script calls, or `Add` for a kind of your own. This middleware records an `x-audit` effect before the action runs, whenever the action is `export`:

<!-- ds:block id=audit-fv9qfz4v title="examples/live-effects-events/main.go" -->
**examples/live-effects-events/main.go** · [`examples/live-effects-events/main.go:100-110`](../../../examples/live-effects-events/main.go#L100-L110)

```go
func audit(next brick.ActionFunc) brick.ActionFunc {
	return func(ctx context.Context, call *brick.ActionCall) (*brick.ActionResult, error) {
		if call.Action == "export" {
			err := brick.EffectsFrom(ctx).Add(brick.Effect{Kind: auditKind, Data: map[string]any{"action": call.Action, "component": call.Component}})
			if err != nil {
				return nil, err
			}
		}
		return next(ctx, call)
	}
}
```
<!-- /ds:block hash=7f277a -->

`Add` checks the effect when it is called, so a bad kind fails this action with an error instead of sending the browser an answer it cannot read. Effects apply in the order they were recorded: the audit effect arrives ahead of the download the script records. The test runs the action and reads the effects off the result:

<!-- ds:block id=testauditeffect-rgpqq5vp title="examples/live-effects-events/main_test.go" -->
**examples/live-effects-events/main_test.go** · [`examples/live-effects-events/main_test.go:168-178`](../../../examples/live-effects-events/main_test.go#L168-L178)

```go
func TestAuditEffect(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, inboxPage)
	res := live.Component("Inbox").Call("export")
	bricktest.AssertEffects(t, res, auditKind, brick.EffectDownload)
	bricktest.AssertEffect(t, res, brick.Effect{Kind: auditKind, Data: map[string]any{"action": "export", "component": "Inbox"}})
	bricktest.AssertNoEffect(t, live.Component("Inbox").Call("dismiss"), auditKind)
}
```
<!-- /ds:block hash=8ccc92 -->

`EffectsFrom` returns nil outside an action, including while a component renders, so only call it from middleware or a helper an action calls. The browser side is a second `brick.effect("x-audit", fn)` in the Inbox's `<brick:client>` block (Step 2). Only a browser runs that handler; the Go test checks that the effect is in the answer and `TestClientModuleServed` checks that the handler is served.

## Step 5 — Limit where redirects may go and how big effects may be

Two options keep effects small and on your site. `Options.RedirectHosts` lists the outside hosts a `redirect`, `navigate`, `download` or `url` effect may name besides the request's own host, such as a payment provider. `Options.MaxEffectBytes` caps the encoded effects and reply of one action; the default is 32 KiB (`brick.DefaultMaxEffectBytes`) and the example lowers it to 4 KiB so the limit is easy to hit.

<!-- snippet: examples/live-effects-events/main.go lines=26-43 -->
```go
const (
	// inboxPage is the one page; it holds a Badge and an Inbox.
	inboxPage = "content/index.html"
	// eventArchived is the event Inbox.archive dispatches and Badge
	// listens to. The two files share only this name.
	eventArchived = "message-archived"
	// confettiKind is this app's own effect kind. Custom kinds start with
	// brick.CustomEffectPrefix ("x-") so no engine kind can collide.
	confettiKind = brick.CustomEffectPrefix + "confetti"
	// auditKind is the second custom kind, recorded from Go before the
	// action runs (see audit).
	auditKind = brick.CustomEffectPrefix + "audit"
	// payHost is the one outside host a redirect may name: the payment
	// provider. Every other absolute URL is refused (RedirectHosts).
	payHost = "pay.example"
	// maxEffectBytes is far below the 32 KiB default so the size limit is
	// easy to reach in a test; a real app keeps the default unless its
	// effects are legitimately larger.
```

`newApp` in Step 1 passes both to `brick.New`. The Inbox has three more actions to try them:

<!-- snippet: examples/live-effects-events/theme/components/Inbox.html lines=39-49 -->
```python
def pay(state, props):
    brick.redirect("https://pay.example/checkout")
    return state

def leave(state, props):
    brick.redirect("https://elsewhere.example/")
    return state

def bulk(state, props):
    brick.dispatch("big", {"blob": "x" * 5000})
    return state
```

`pay` redirects to the allowed host and `leave` to one that is not; `bulk` records an event with 5 000 bytes of data. The tests show what the user of the app sees:

<!-- ds:block id=testredirecthosts-bgf65v6x title="examples/live-effects-events/main_test.go" -->
**examples/live-effects-events/main_test.go** · [`examples/live-effects-events/main_test.go:183-194`](../../../examples/live-effects-events/main_test.go#L183-L194)

```go
func TestRedirectHosts(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, inboxPage)
	bricktest.AssertRedirect(t, live.Component("Inbox").Call("pay"), "https://pay.example/checkout")
	_, err = live.Component("Inbox").TryCall("leave")
	if err == nil || !strings.Contains(err.Error(), `names host "elsewhere.example", which is neither this site nor in Options.RedirectHosts`) {
		t.Fatalf("err = %v", err)
	}
}
```
<!-- /ds:block hash=816e4e -->

<!-- ds:block id=testmaxeffectbytes-t8k2m4qz title="examples/live-effects-events/main_test.go" -->
**examples/live-effects-events/main_test.go** · [`examples/live-effects-events/main_test.go:198-209`](../../../examples/live-effects-events/main_test.go#L198-L209)

```go
func TestMaxEffectBytes(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, inboxPage)
	_, err = live.Component("Inbox").TryCall("bulk")
	if err == nil || !strings.Contains(err.Error(), "effects over 4096 bytes (Options.MaxEffectBytes)") {
		t.Fatalf("err = %v", err)
	}
	t.Log(err)
}
```
<!-- /ds:block hash=b33c88 -->

The over-limit error says `effects over 4096 bytes (Options.MaxEffectBytes): events carry ids, not records`. The fix is never a bigger limit: send ids and let the receiver read the record.

## Step 6 — Serve the routes

The action route takes the POST the runtime sends; the other routes are the pages and the file the effects point at.

<!-- ds:block id=app-handler-nr4g93km title="examples/live-effects-events/main.go" -->
**examples/live-effects-events/main.go** · [`examples/live-effects-events/main.go:136-152`](../../../examples/live-effects-events/main.go#L136-L152)

```go
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.Handle("POST "+brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("GET /messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "message %s", r.PathValue("id"))
	})
	mux.HandleFunc("GET /export.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, "id,subject\n1,Welcome\n2,Invoice\n3,Lunch?\n")
	})
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "signed out")
	})
	return mux
}
```
<!-- /ds:block hash=d31fec -->

## Try it

From the `examples` folder:

```sh
go run ./live-effects-events -demo
```

The demo serves the app on a local test server and clicks each button over real HTTP, the way the runtime does (a JSON POST to `/_brick/action` with `Accept: application/vnd.brick.action+json`). Each `effect` line is one entry of the answer's `effects` array, exactly as it arrived:

<!-- snippet: examples/live-effects-events/testdata/transcript.txt -->
```text
GET /
  <p class="badge">0 archived</p>, 3 messages
POST Inbox.archive(2)
  effect {"kind":"dispatch","name":"message-archived","data":{"id":2}}
  effect {"kind":"flash","message":"Archived message 2.","level":"success"}
  effect {"kind":"title","text":"Inbox (2)"}
  effect {"kind":"x-confetti","data":{"pieces":40}}
  html: 2 messages
the runtime delivers message-archived to Badge:
POST Badge.count({"id":2})
  html: <p class="badge">1 archived, last #2</p>
POST Inbox.archive(2)
  effect {"kind":"flash","message":"Message 2 was already archived.","level":"warning"}
POST Inbox.read(3)
  effect {"kind":"navigate","url":"/messages/3"}
POST Inbox.unread_only()
  effect {"kind":"url","url":"/?filter=unread","replace":true}
POST Inbox.export()
  effect {"kind":"x-audit","data":{"action":"export","component":"Inbox"}}
  effect {"kind":"download","url":"/export.csv","filename":"inbox.csv"}
POST Inbox.dismiss()
  effect {"kind":"flash-clear"}
POST Inbox.sign_out()
  effect {"kind":"redirect","url":"/login"}
```

`go test ./live-effects-events/` checks this transcript byte for byte. To click it yourself, run `go run ./live-effects-events` and open http://localhost:8084/.

## Pitfalls

- **Effects only exist inside an action.** Calling `brick.flash` from a loader, a template function or the re-render after an action fails with `brick.flash can only be called inside an action, never while a page renders`. Drawing a page can never fire an effect, so rendering twice never shows a message twice.
- **A failed action sends no effects.** When the action (or a middleware) returns an error, everything it recorded is discarded, including the effects recorded before the failure.
- **Flash levels are a closed list.** `brick.flash("hi", "error")` fails with `level "error" is not one of neutral, info, success, warning, danger`.
- **URLs stay on your site.** `redirect`, `navigate`, `download` and `url` take a path or your own host. `brick.redirect("https://elsewhere.example/")` fails with `URL "https://elsewhere.example/" names host "elsewhere.example", which is neither this site nor in Options.RedirectHosts`. Add trusted hosts (a payment provider) to `Options.RedirectHosts` ([Step 5](#step-5--limit-where-redirects-may-go-and-how-big-effects-may-be)); the browser checks the URL again before it follows it.
- **Effects are small.** One action's effects (and its `brick.reply`) are capped at 32 KiB (`Options.MaxEffectBytes`). Past it the action fails with `effects over 32768 bytes (Options.MaxEffectBytes): events carry ids, not records`. Send ids and let the receiver read the record.
- **A client that does not ask gets HTML only.** The envelope with `effects` is sent only to a request whose `Accept` lists `application/vnd.brick.action+json` (the runtime always does). A plain `curl` gets the component's HTML, and in Dev mode the engine warns `<Inbox>.archive recorded 4 effects the client did not accept (send Accept: application/vnd.brick.action+json)`, counting the ones middleware added.
- **A custom kind must start with `x-`.** In the browser, `brick.effect("confetti", fn)` throws `brick.effect: custom kinds start with "x-"`. In Go, `Effects.Add` refuses one with `effect kind "confetti" is not an engine kind; a host's own kind starts with "x-"`. (The example's middleware appends to `res.Effects` after `next`, which is not re-checked, so keep the prefix there yourself.)

## Related

- [Events — one component tells the others](events.md), the `dispatch` effect in depth, using the same example.
- [Rows, regions, streams and native submit](rows-and-regions.md), the effects that edit part of a component.
- [The browser runtime: events, effects and retries](browser-runtime.md) and the [runtime API reference](../../reference/runtime-api.md) for `brick.effect`, `brick.on("flash")` and `brick.on("effect")`.
- Feature index: [Effects and events](../../reference/features.md#effects-and-events).
- The one-page guide's section: [Events and effects](../../guide.md#events-and-effects--components-talking-to-each-other).
- Example: [examples/live-effects-events](../../../examples/live-effects-events/).
