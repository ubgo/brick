# live-push-presence

Server push and presence over real event streams. An order list listens to the `orders` topic and refreshes on every open page when anyone ships an order, from an action (`brick.publish`) or from a webhook in Go (`Engine.Publish`). `PushOptions.AuthorizeTopic` keeps signed-out viewers off the stream, and a lobby lists who is on it (`listen: {presence:}`, `.Presence`, `PresenceOptions.Identify`). A viewer is named by a `user` cookie.

From the `examples` folder:

- `go run ./live-push-presence -demo` signs in two people and prints the requests, pushed events and refreshes (the same text as `testdata/transcript.txt`).
- `go run ./live-push-presence` serves it on http://localhost:8086/: open `/login?as=Ada` in one browser and `/login?as=Bob` in a private window, then `/orders/` and `/room/` in both.
- `go test ./live-push-presence/` checks the transcript and the error messages the guide quotes.

Behind several servers the in-memory broadcaster is replaced by `contrib/push-redis`; its wiring is shown in Step 5 of the guide, and the runnable copy is `Example` in [contrib/push-redis/example_test.go](../../contrib/push-redis/example_test.go), which needs a Redis. A passing run of the tests prints `ok  	github.com/ubgo/brick/examples/live-push-presence`.

Guide: [Push and presence](../../docs/guides/live/push-and-presence.md).
