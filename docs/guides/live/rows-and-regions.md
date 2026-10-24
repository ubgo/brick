# Rows, regions, streams and native submit

## What it is and when you want it

A component's state should stay small: it is stored between requests and sent back with every action. Four effects change part of a component without its state holding all of it. `brick.rows` adds, updates and removes rows of a list one at a time (a chat log, an activity feed). `brick.region` keeps what earlier answers put in an element and adds the next page to it (a "load more" list). `brick.stream` sends text into an element while the action is still running (a slow reply, word by word). `brick.submit` hands a form to the browser's own submit (a sign-in that must set a cookie through a classic POST).

## Before you start

- Read [Effects](effects.md): these four are effects, recorded inside an action.
- Each one names its target with `brick:ref="name"` on an element inside the component. Rows and regions also need `brick:ignore` on that element, so an ordinary re-render leaves its content alone.
- The example is [examples/live-rows-regions](../../../examples/live-rows-regions/). Run it from the `examples` folder.

## Step 1 — A list fed row by row

The chat log is an empty `<ul brick:ref="log" brick:ignore>`. The `send` action never stores the messages: `brick.rows` renders the `Msg` component once per item, with the item as its props, and the browser inserts each row into the list. Rows are keyed by the item's `id`.

`examples/live-rows-regions/theme/components/Chat.html`

<!-- snippet: examples/live-rows-regions/theme/components/Chat.html -->
```html
---
state: {next: 1, answer: ""}
---
<brick:script>
def send(state, props, text):
    msg = {"id": state["next"], "text": text}
    brick.rows("log", component = "Msg", items = [msg], at = "top", limit = 3)
    return dict(state, next = state["next"] + 1)

def remove(state, props, id):
    brick.rows("log", op = "delete", keys = [str(id)])
    return state

def clear(state, props):
    brick.rows("log", op = "reset")
    return state

def think(state, props):
    words = ["Rows", " stay", " out", " of", " state."]
    for w in words:
        brick.stream("answer", w)
    return dict(state, answer = "".join(words))
</brick:script>
<section class="chat">
  <ul brick:ref="log" brick:ignore></ul>
  <p brick:ref="answer">{{ .State.answer }}</p>
  <button brick:click='send("Hello")'>Send</button>
  <button brick:click="clear">Clear</button>
  <button brick:click="think">Think</button>
</section>
```

`examples/live-rows-regions/theme/components/Msg.html`

<!-- snippet: examples/live-rows-regions/theme/components/Msg.html -->
```html
---
props:
  id: {type: int}
  text: {type: string}
---
<li class="msg">{{ .Props.text }}</li>
```

The arguments of `brick.rows(ref, op = "insert", component = None, items = [], key = "id", at = "bottom", limit = 0, keys = [])`:

| Argument | Meaning |
|---|---|
| `ref` | The `brick:ref` of the list in this component. |
| `op` | `insert` (the default) adds rows, and a key already in the list is replaced where it stands; `delete` removes the rows named by `keys`; `reset` empties the list, then inserts `items`. |
| `component`, `items` | Each item is rendered as `component` with the item as its props. Needed for `insert`. |
| `key` | The item field that keys its row, `id` by default. |
| `at`, `limit` | `top` or `bottom` (the default) is where new rows go; a `limit` above zero trims the list from the other end. |

In Go: `brick.EffectsFrom(ctx).Rows(ctx, brick.RowsSpec{Ref: "log", Component: "Msg", Items: items, At: brick.RowsTop, Limit: 3})`.

## Step 2 — Stream text while the action runs

`think` in the same file calls `brick.stream("answer", text)` for each word. Each call is sent to the browser at once and appended to the `brick:ref="answer"` element; the action's final render then lands as usual. `brick.stream(ref, text, replace = True)` replaces the element's text instead of appending. A host's Go action uses `brick.StreamFrom(ctx).Write(ref, text, replace)`.

## Step 3 — A "load more" list that keeps its earlier pages

The feed is written inline in the template. Its state holds only the current page; `brick.region("feed", "append")` tells the browser to add the feed's children from this render after the ones it already shows. `prepend` adds them before, and `replace` (the default) swaps them.

`examples/live-rows-regions/theme/components/Feed.html`

<!-- snippet: examples/live-rows-regions/theme/components/Feed.html -->
```html
---
state: {page: 1, items: ["Item 1", "Item 2"]}
---
<brick:script>
PER_PAGE = 2

def more(state, props):
    page = state["page"] + 1
    first = (page - 1) * PER_PAGE + 1
    brick.region("feed", "append")
    return dict(state, page = page, items = ["Item %d" % n for n in range(first, first + PER_PAGE)])
</brick:script>
<section class="feed">
  <ul brick:ref="feed" brick:ignore>{{ range .State.items }}<li>{{ . }}</li>{{ end }}</ul>
  <button brick:click="more">Load more</button>
</section>
```

Unlike `brick.rows`, a region needs no row component and no keys. In Go: `brick.EffectsFrom(ctx).Region("feed", brick.RegionAppend)`.

## Step 4 — Hand a form to the browser's own submit

Some steps must be a real form post: a sign-in that sets a cookie, or a payment provider's endpoint. The action can still run first (validate, show a flash); `brick.submit("login")` then submits the `brick:ref="login"` form natively once the morph is in, bypassing brick's own submit handling.

`examples/live-rows-regions/theme/components/Login.html`

<!-- snippet: examples/live-rows-regions/theme/components/Login.html -->
```html
---
state: {email: ""}
---
<brick:script>
def sign_in(state, props):
    brick.flash("Signing you in…")
    brick.submit("login")
    return state
</brick:script>
<form brick:ref="login" method="post" action="/session" brick:submit="sign_in">
  <input name="email" type="email" brick:model="email">
  <button>Sign in</button>
</form>
```

The page and the host:

`examples/live-rows-regions/theme/content/index.html`

<!-- snippet: examples/live-rows-regions/theme/content/index.html -->
```html
<!doctype html>
<html>
<head><title>Rows and regions</title></head>
<body>
<Chat />
<Feed />
<Login />
{{ assets }}
</body>
</html>
```

<!-- ds:block id=app-handler-t9c9w5t8 title="examples/live-rows-regions/main.go" -->
**examples/live-rows-regions/main.go** · [`examples/live-rows-regions/main.go:53-70`](../../../examples/live-rows-regions/main.go#L53-L70)

```go
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.Handle("POST "+brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		html, err := a.eng.Render(r.Context(), page, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "signed in as %s", r.FormValue("email"))
	})
	return mux
}
```
<!-- /ds:block hash=56f786 -->

## Try it

From the `examples` folder:

```sh
go run ./live-rows-regions -demo
```

Each effect line is what the answer carried. The row HTML is JSON, so `<` appears as `<`. The `think` action is posted with the streaming `Accept` the runtime sends, so its chunks arrive as separate NDJSON lines before the final answer:

<!-- snippet: examples/live-rows-regions/testdata/transcript.txt -->
```text
GET /
  feed: <ul brick:ref="feed" brick:ignore><li>Item 1</li><li>Item 2</li></ul>
POST Chat.send("Hello")
  effect {"kind":"rows","ref":"log","op":"insert","at":"top","limit":3,"rows":[{"key":"1","html":"\u003cli class=\"msg\"\u003eHello\u003c/li\u003e"}]}
POST Chat.send("How are you?")
  effect {"kind":"rows","ref":"log","op":"insert","at":"top","limit":3,"rows":[{"key":"2","html":"\u003cli class=\"msg\"\u003eHow are you?\u003c/li\u003e"}]}
POST Chat.send("Still there?")
  effect {"kind":"rows","ref":"log","op":"insert","at":"top","limit":3,"rows":[{"key":"3","html":"\u003cli class=\"msg\"\u003eStill there?\u003c/li\u003e"}]}
POST Chat.send("Bye")
  effect {"kind":"rows","ref":"log","op":"insert","at":"top","limit":3,"rows":[{"key":"4","html":"\u003cli class=\"msg\"\u003eBye\u003c/li\u003e"}]}
POST Chat.remove(2)
  effect {"kind":"rows","ref":"log","op":"delete","keys":["2"]}
POST Chat.clear()
  effect {"kind":"rows","ref":"log","op":"reset","at":"bottom"}
POST Chat.think()  (Accept: application/vnd.brick.stream+ndjson)
  chunk {"stream":{"ref":"answer","text":"Rows"}}
  chunk {"stream":{"ref":"answer","text":" stay"}}
  chunk {"stream":{"ref":"answer","text":" out"}}
  chunk {"stream":{"ref":"answer","text":" of"}}
  chunk {"stream":{"ref":"answer","text":" state."}}
  html: <p brick:ref="answer">Rows stay out of state.</p>
POST Feed.more()
  effect {"kind":"region","ref":"feed","op":"append"}
  html: <ul brick:ref="feed" brick:ignore><li>Item 3</li><li>Item 4</li></ul>
POST Login.sign_in()
  effect {"kind":"flash","message":"Signing you in…","level":"info"}
  effect {"kind":"submit","ref":"login"}
```

Four sends with `limit = 3` leave three rows in the browser (`Bye`, `Still there?`, `How are you?`), then `remove(2)` deletes one and `clear` empties the list. `go test ./live-rows-regions/` checks the transcript. Run `go run ./live-rows-regions` and open http://localhost:8085/ to watch it in a browser.

## Pitfalls

- **Forget `brick:ignore` and the next render wipes the rows.** The list's content lives only in the browser; without `brick:ignore` the morph after any action puts the template's (empty) list back. The same holds for a region.
- **Every row needs its key.** `brick.rows("log", component = "Msg", items = [{"text": "x"}])` fails with `item 0 has no "id" field to key its row by`. Name another field with `key =`.
- **An insert needs its component.** Without `component =` it fails with `an insert needs the component that renders each item`. There is no `html =` argument: a row is always a component's render, so a user's text arrives escaped (`<b>hi</b>` becomes `&lt;b&gt;hi&lt;/b&gt;`, which `TestRowTextIsEscaped` checks).
- **A region is a piece of the render.** `brick.region` after `brick.skip_render()` fails, and so does `brick.skip_render()` after `brick.region`, with `the action skipped its render (brick.skip_render), and a region is a piece of that render` in either order.
- **Streamed text is text.** The runtime sets each chunk with `textContent`, so markup in a chunk shows as characters. Put markup in the final render.
- **A stream is sent only to a client that asks.** Without the stream type in `Accept` (the runtime always sends it), the chunks are dropped and only the final answer arrives, so an action behaves the same in a test or a plain client. A streamed action that has started is never retried after a 503 or 429, because its text may already be on the page.
- **`submit` leaves the page.** It runs after every other effect, a later `redirect` or `navigate` in the same action replaces it, and flashes are carried to the next page. The target must be a `<form>` with that `brick:ref` in the same component; otherwise the browser logs `brick: submit: no brick:ref="login" form in this component` and stays.

## Related

- [Effects](effects.md) and [Events](events.md).
- [The browser runtime](browser-runtime.md) for `brick.ref(el, name)`, which finds a `brick:ref` element from client code.
- Feature index: [Effects and events](../../reference/features.md#effects-and-events) (rows, regions, submit) and the streamed action output row.
- The one-page guide: [Events and effects](../../guide.md#events-and-effects--components-talking-to-each-other) and [Lazy islands and streamed replies](../../guide.md#lazy-islands-and-streamed-replies).
- Example: [examples/live-rows-regions](../../../examples/live-rows-regions/); the kit showcase has [RuntimeRows.html](../../../examples/ui/demos/RuntimeRows.html), [RegionFeed.html](../../../examples/ui/demos/RegionFeed.html), [RuntimeStream.html](../../../examples/ui/demos/RuntimeStream.html) and [RuntimeSubmit.html](../../../examples/ui/demos/RuntimeSubmit.html).
