# forms-signup

A signup form whose fields are `brick:model.defer`: typing sends nothing, and *Save* sends one action that carries every held value as `sets`, including a list (the topic checkboxes) and a dotted path (`address.city`). It also shows `brick:dirty`, `brick:loading.delay`, `brick:confirm` and `brick:disable-with`, and the compile-time checks that catch a misspelt field or modifier. A second component, `Cart`, binds list rows by index (`items.0.qty`) and replaces the confirm and prompt dialogs with `brick.confirmWith` and `brick.promptWith`. The engine is mounted on a plain `net/http` mux.

## Run it

```sh
cd examples
go run ./forms-signup -addr :8084      # then open http://localhost:8084/
```

## Test it

```sh
cd examples
go test ./forms-signup/ -v
```

```text
--- PASS: TestSignupDeferredSets (0.00s)
--- PASS: TestCartNestedPaths (0.00s)
--- PASS: TestCartAsksServed (0.00s)
--- PASS: TestSignupLinksRuntime (0.00s)
--- PASS: TestFormTyposFailAtCompile (0.00s)
--- PASS: TestMisspeltActionFailsAtRunTime (0.00s)
ok  	github.com/ubgo/brick/examples/forms-signup	0.038s
```

Guide: [Deferred fields, lists and nested paths](../../docs/guides/forms/deferred-fields.md).
