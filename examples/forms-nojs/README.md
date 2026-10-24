# forms-nojs

A signup form that works with JavaScript switched off, answered by the host's own router instead of the site layer: `GET /signup` renders the page (resuming the instance a post left), and `POST /signup` hands the form to `Engine.FormPost`, answering `303` with the new state in a one-minute cookie, or `422` with the errors on the page. A cross-site post is refused before the action runs. A *Cancel* button with `$dispatch("signup-cancelled")` shows a call only a browser can make: it stays inert in the markup and adds no field to the post.

## Run it

```sh
cd examples
go run ./forms-nojs -addr :8085      # then open http://localhost:8085/signup with scripts off
```

## Test it

```sh
cd examples
go test ./forms-nojs/ -v
```

```text
--- PASS: TestSignupWithoutJavaScript (0.00s)
--- PASS: TestDispatchStaysInert (0.00s)
ok  	github.com/ubgo/brick/examples/forms-nojs	0.023s
```

Guide: [Forms without JavaScript, in your own router](../../docs/guides/forms/without-javascript.md).
