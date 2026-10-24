# Chat

A Slack-style chat built only from kit components — channels and direct messages with unread counts, a live conversation over SQLite, a composer with emoji and attachments, and a details drawer.

Open it at http://localhost:8002/apps/chat/ (`task ui:serve`).

## Files

| File | What it holds |
|---|---|
| `ChatApp.html` | The Chat demo as one stateful component — channels and direct messages with unread counts, the open conversation and the composer, over the chat tables (apps/chat/chat.star, demodb/db_chat.go). |
| `ChatFile.html` | A message's attachment chip, or nothing when name is "". |
| `ChatFiles.html` | The files shared in a channel, for ChatApp's details drawer, with an empty state. |
| `chat.star` | The queries and actions, in Starlark (loaded with `use:` as `~/apps/chat/chat.star`). |
| `../../content/apps/chat.html` | The page: `layout: app`, `AppShell` with the shared `AppNav`, this app's component, and the View source tabs. It stays in `content/` because brick derives URLs from there. |
| `../../demodb/db_chat.go` | The app's tables and their deterministic seed. It is Go, so it lives in the importable `demodb` package; `seed()` in `demodb/db.go` runs it. |
| `../../apps_chat_test.go` | The app's tests: they load the page and post to `/_brick/action` the way the runtime does. |

## Test

```sh
task ui:test:app APP=chat   # only this app's tests
task ui:test                # every showcase test
```
