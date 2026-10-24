# forms-validation-lists

Two forms whose checks are declared once under `brick: {rules:}`. The signup form uses `required`, `email`, `min`, `same`, a custom message, `brick.validate()` and `brick.untouch()`, with errors shown only for touched fields. The todo list uses wildcard rules on list items (`todos.*.title`, `todos.*.tags.*`), `.Form` objects for each row, a `slug` rule the host adds through its own `Options.Validator`, and a `<brick:client>` block that reads the render's errors with `brick.errors(el)`.

## Run it

```sh
cd examples
go run ./forms-validation-lists -addr :8086      # then open http://localhost:8086/
```

## Test it

```sh
cd examples
go test ./forms-validation-lists/ -v
```

```text
--- PASS: TestSignupRules (0.01s)
--- PASS: TestTodoListRules (0.00s)
--- PASS: TestRuleTyposFailAtCompile (0.00s)
--- PASS: TestErrorsReadableByTheBrowser (0.00s)
ok  	github.com/ubgo/brick/examples/forms-validation-lists	0.038s
```

Guide: [Validation: rules declared once](../../docs/guides/forms/validation.md).
