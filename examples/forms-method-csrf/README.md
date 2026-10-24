# forms-method-csrf

Two posts with `brick:method="delete"` links, for an app that guards its write routes with a CSRF token. The token reaches the page in a `<meta>` tag, the `PostRow` component's client code adds it to every method link's hidden form with `brick.methodFormWith`, and the `DELETE /posts/{id}` route refuses a request without it (`403`). The plain method-link program is [../methodlinks](../methodlinks/).

## Run it

```sh
cd examples
go run ./forms-method-csrf -addr :8088      # then open http://localhost:8088/
```

## Test it

```sh
cd examples
go test ./forms-method-csrf/ -v
```

```text
--- PASS: TestMethodFormCarriesCSRFToken (0.00s)
ok  	github.com/ubgo/brick/examples/forms-method-csrf	0.043s
```

Guide: [Method links: Delete, Publish and Log out without a form](../../docs/guides/forms/method-links.md).
