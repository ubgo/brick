# Inbox

A three-pane mail client built only from kit components — folders, search, a reading pane and a compose dialog over SQLite, in one stateful component.

Open it at http://localhost:8002/apps/inbox/ (`task ui:serve`).

## Files

| File | What it holds |
|---|---|
| `InboxApp.html` | The Inbox app as one stateful component — folders with unread counts, the message list and the reading pane, over the messages table (apps/inbox/inbox.star). |
| `inbox.star` | The queries and actions, in Starlark (loaded with `use:` as `~/apps/inbox/inbox.star`). |
| `../../content/apps/inbox.html` | The page: `layout: app`, `AppShell` with the shared `AppNav`, this app's component, and the View source tabs. It stays in `content/` because brick derives URLs from there. |
| `../../demodb/db_inbox.go` | The app's tables and their deterministic seed. It is Go, so it lives in the importable `demodb` package; `seed()` in `demodb/db.go` runs it. |
| `../../apps_inbox_test.go` | The app's tests: they load the page and post to `/_brick/action` the way the runtime does. |

## Test

```sh
task ui:test:app APP=inbox   # only this app's tests
task ui:test                # every showcase test
```
