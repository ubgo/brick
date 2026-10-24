# Events — one component tells the others

## What it is and when you want it

An action re-renders only its own component. When an inbox archives a message, a counter elsewhere on the page still shows the old number. An event fixes that without the two components knowing each other: the action dispatches a named event, and every other component that lists that name under `brick: {on:}` runs one of its own actions. You want events whenever one action changes something a second component shows: a count in the header, a "last saved" note, a list beside a form.

## Before you start

- Read [Effects](effects.md) first: `brick.dispatch` is one of the effects an action records, and it uses the same example.
- Both components are stateful (`state:` in the frontmatter). A listener is an action, and only a stateful component has actions.
- The example is [examples/live-effects-events](../../../examples/live-effects-events/).

## Step 1 — Dispatch the event from the action

`brick.dispatch(name, data)` in the Inbox's `archive` action records the event with the id of the archived message. The event carries an id, not the record.

`examples/live-effects-events/theme/components/Inbox.html` (the `archive` action)

<!-- snippet: examples/live-effects-events/theme/components/Inbox.html lines=9-17 -->
```python
def archive(state, props, id):
    left = [m for m in state["messages"] if m["id"] != id]
    if len(left) == len(state["messages"]):
        brick.flash("Message %d was already archived." % id, "warning")
        return state
    brick.dispatch("message-archived", {"id": id})
    brick.flash("Archived message %d." % id, "success")
    brick.title("Inbox (%d)" % len(left))
    return dict(state, messages = left)
```

## Step 2 — Listen in the other component

The Badge lists the event under `brick: {on:}` and names the action to run. The listener gets the event's data as its last argument.

`examples/live-effects-events/theme/components/Badge.html`

<!-- snippet: examples/live-effects-events/theme/components/Badge.html -->
```html
---
state: {archived: 0, last: 0}
brick:
  on:
    message-archived: count
---
<brick:script>
def count(state, props, event):
    return dict(state, archived = state["archived"] + 1, last = event["id"])
</brick:script>
<brick:client>
// Run once for every Badge on the page, now or inserted later. brick.watch
// calls the function whenever the "archived" signal of this root changes,
// whether a click, an optimistic write or a morph changed it, and returns
// a function that stops watching; returning it as the cleanup runs it
// when the Badge leaves the page.
brick.mount("Badge", (badge) =>
  brick.watch(badge, "archived", (count, was) => {
    badge.dataset.delta = String(count - was);
  }),
);

// brick.find(type) is the first stateful root of that type (or null) and
// brick.all() every stateful root on the page, in document order.
brick.on("message-archived", () => {
  const inbox = brick.find("Inbox");
  if (inbox) inbox.dataset.touched = "yes";
  document.documentElement.dataset.statefulRoots = String(brick.all().length);
});
</brick:client>
<p class="badge">{{ .State.archived }} archived{{ if .State.last }}, last #{{ .State.last }}{{ end }}</p>
```

Instead of an action of its own, a listener may be `$refresh`, which re-renders the component with no change: the usual choice when the component reads its numbers from your data layer rather than from its state (`contact-deleted: $refresh`).

## Step 3 — Let a child tell its parent, from an attribute

Two components that are not siblings should not need to know each other, but a child placed by a parent already has one fixed neighbour. The parent writes `brick:on:<event>` on the tag that places the child and names one of its own actions; only that child's events run it. The child dispatches with `$dispatch(name, data)` straight from an attribute, which sends no request, or runs an action of the parent with `$parent.<name>`.

`examples/live-effects-events/theme/components/Thread.html`

<!-- snippet: examples/live-effects-events/theme/components/Thread.html -->
```html
---
state: {liked: 0, pinned: false}
---
<brick:script>
def liked(state, props, event):
    return dict(state, liked = state["liked"] + 1)

def pin(state, props):
    return dict(state, pinned = True)
</brick:script>
<article class="thread">
  <p id="summary">{{ .State.liked }} liked{{ if .State.pinned }}, pinned{{ end }}</p>
  <Reply brick:on:liked="liked" />
</article>
```

`examples/live-effects-events/theme/components/Reply.html`

<!-- snippet: examples/live-effects-events/theme/components/Reply.html -->
```html
---
state: {}
---
<p class="reply">
  <button id="like" brick:click='$dispatch("liked", {"id": 7})'>Like</button>
  <button id="pin" brick:click="$parent.pin">Pin the thread</button>
</p>
```

The page that places the thread:

<!-- snippet: examples/live-effects-events/theme/content/thread.html -->
```html
<!doctype html>
<html>
<head><title>Thread</title></head>
<body>
<Thread />
{{ assets }}
</body>
</html>
```

`brick:on:liked="liked"` is on the tag in `Thread.html`, which is therefore stateful (it has `state:`); the Reply is stateful too, because only a stateful component has `brick:click`. The test below clicks both buttons. `Deliver` runs what the runtime would do with the dispatched event; `$parent.pin` is a normal action call that `Click` runs on the parent.

<!-- ds:block id=testthreadchildevents-m3v9c8xd title="examples/live-effects-events/main_test.go" -->
**examples/live-effects-events/main_test.go** · [`examples/live-effects-events/main_test.go:217-236`](../../../examples/live-effects-events/main_test.go#L217-L236)

```go
func TestThreadChildEvents(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, threadPage)
	thread := live.Component("Thread")
	thread.AssertSet(t, "liked", 0)

	// $dispatch("liked", {"id": 7}) runs no request; Deliver does what the
	// runtime does with the event.
	live.Deliver(live.Component("Reply").Click("#like"))
	thread.AssertSet(t, "liked", 1)

	live.Component("Reply").Click("#pin") // $parent.pin
	thread.AssertSet(t, "pinned", true)
	if !strings.Contains(thread.HTML(), `1 liked, pinned`) {
		t.Fatalf("thread:\n%s", thread.HTML())
	}
}
```
<!-- /ds:block hash=9a4a38 -->

What only a browser does: reading the `brick:click` attribute and calling the runtime's `brick.dispatch`. The test replays that step by hand, so it shows the result (the parent's state and markup) but not the click handler itself.

## Step 4 — Test it without a browser

`bricktest.Live` fills the page and holds its stateful components. `Call` runs an action, `AssertDispatched` checks the event it recorded, and `Deliver` runs every listener the dispatch wakes, as the runtime would.

<!-- ds:block id=testbadgehearsarchive-y3vufghu title="examples/live-effects-events/main_test.go" -->
**examples/live-effects-events/main_test.go** · [`examples/live-effects-events/main_test.go:101-114`](../../../examples/live-effects-events/main_test.go#L101-L114)

```go
func TestBadgeHearsArchive(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, inboxPage)
	res := live.Component("Inbox").Call("archive", 2)
	bricktest.AssertDispatched(t, res, eventArchived, map[string]any{"id": 2})
	bricktest.AssertFlash(t, res, brick.FlashSuccess, "Archived message 2.")
	live.Deliver(res)
	badge := live.Component("Badge")
	badge.AssertSet(t, "archived", 1)
	badge.AssertSet(t, "last", 2)
}
```
<!-- /ds:block hash=925e01 -->

## Try it

From the `examples` folder:

```sh
go run ./live-effects-events -demo
```

The demo clicks Archive over real HTTP. The answer carries the `dispatch` effect; the demo then does what the runtime does with it and posts the Badge's `count` action with the event's data:

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

## More ways to send and hear events

- **To one component.** `brick.dispatch("saved", {"id": 7}, to = "Badge")` reaches only components whose file stem is `Badge`; `to = "self"` reaches only the sender's own listeners.
- **Per record.** A listener key may be a template, `"contact-updated.{{ .Props.id }}": reload`, so each row hears only its own record's events.
- **From one child only.** On the tag that places a child, `<ContactRow brick:on:deleted="row_deleted" />` (Step 3 shows a complete one) runs `row_deleted` on the component that wrote the tag, only when that child dispatches `deleted`. The file with the tag must be stateful.
- **From the browser, with no request.** `brick:click='$dispatch("contact-selected", {"id": 42})'` dispatches from an attribute; client code calls `brick.dispatch(el, "contact-selected", {id: 42})`. The listeners then run as usual.
- **In client code.** `brick.on("message-archived", (detail) => …)` in a `<brick:client>` block hears every event, with `detail.$source` set to the sending component's `data-brick` id. This is how a server makes browser code respond: it dispatches an event, it never sends JavaScript.
- **From a child to its parent's action.** `brick:click="$parent.save"` runs `save` on the stateful component around this one (Step 3 shows it). Prefer an event: it keeps the child reusable.

## Pitfalls

- **Event names are lowercase.** Browsers lowercase attribute names, so `brick.dispatch("Message-Archived")` fails with `event "Message-Archived": event names are lowercase (browsers lowercase attribute names); write "message-archived"`. A name is lowercase letters, digits and `. _ : -`, at most 64 characters.
- **Some names belong to the runtime.** `navigate`, `flash`, `action-error` and the other [runtime events](../../reference/runtime-api.md#runtime-events), and anything starting with `brick:`, fail with `event "navigate" is reserved for the runtime`, so an action can never fake a navigation or a recovery.
- **The sender does not hear its own event**, unless the dispatch says `to = "self"`.
- **Chains stop.** An event that triggers a listener that dispatches again is followed at most 4 hops deep, never through a name already in the chain, and to at most 50 listeners; past that the runtime logs a warning and emits `event-loop` with `{name, chain}`.
- **Same page only.** An event never reaches another tab, another user, or a component that was not on the page when it was sent. For other pages and other people, use [push](push-and-presence.md).
- **A listener is an ordinary action.** It runs with its own component's handle and token and through the same middleware, so an event never lets a component do anything a click on it could not.
- **Listener names are checked when the file is read.** `brick: {on: {message-archived: cuont}}` with no `cuont` action fails when the file is read with `on: message-archived: "cuont" is not a function of this component (have: count, $refresh)`, the same as a misspelled `brick:click`.

## Related

- [Effects](effects.md), every other effect an action can record.
- [Push and presence](push-and-presence.md), for updates that must reach other pages and other people.
- [Runtime API: `brick.on`, `brick.dispatch`, `brick.emit`](../../reference/runtime-api.md).
- Feature index: [Effects and events](../../reference/features.md#effects-and-events).
- The one-page guide's section: [Events and effects](../../guide.md#events-and-effects--components-talking-to-each-other).
- Example: [examples/live-effects-events](../../../examples/live-effects-events/).
