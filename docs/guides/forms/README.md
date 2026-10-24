# Forms, validation and uploads

Guides for the forms a stateful component draws: binding fields to state, submitting without JavaScript, declaring checks once, taking files, and links that send a verb other than GET. Each page builds one runnable program under [examples/](../../../examples/) whose test runs the real handler and checks the output the page shows.

| Guide | What it covers | Example |
|---|---|---|
| [Deferred fields, lists and nested paths](deferred-fields.md) | `brick:model.defer` and the `sets` wire field, list checkboxes, dotted paths, `brick:submit`, `brick:dirty`, `brick:loading`, `brick:confirm`, `brick:disable-with`, compile-time typo checks, list rows by index (`items.0.qty`), `brick.confirmWith` / `brick.promptWith` | [forms-signup](../../../examples/forms-signup/) |
| [Forms without JavaScript, in your own router](without-javascript.md) | the `method="post"` rewrite, `Engine.IsFormPost` / `FormPost`, `SetFormResume` / `FormResumeOption` / `WithFormResume`, the cross-site check, browser-only calls (`$dispatch`) left inert | [forms-nojs](../../../examples/forms-nojs/) |
| [Validation: rules declared once](validation.md) | `rules:` and `messages:`, touched fields, `brick.validate` / `brick.untouch`, wildcard rules on list items, `.Form` objects, a custom `Options.Validator`, `brick.errors(el)` in client code | [forms-validation-lists](../../../examples/forms-validation-lists/) |
| [Uploads: files into state, bytes into your storage](uploads.md) | `uploads:`, `brick:upload`, `brick:upload-progress`, `$removeUpload`, the `upload/disk` sink, `Options.UploadCheck`, uploads without JavaScript, presigned uploads, `brick.upload(el, field, files)` and the upload events | [forms-uploads](../../../examples/forms-uploads/) |
| [Method links](method-links.md) | `brick:method` on `<a href>`, `brick.MethodOverride`, `brick.methodFormWith` for a CSRF token | [methodlinks](../../../examples/methodlinks/), [forms-method-csrf](../../../examples/forms-method-csrf/) |

Run every example's test from the `examples` module:

```sh
cd examples
go test ./forms-signup/ ./forms-nojs/ ./forms-validation-lists/ ./forms-uploads/ ./forms-method-csrf/ ./methodlinks/
```

The one-page reference for the same features is the [guide](../../guide.md#forms--deferred-fields-arrays-modifiers-loading-and-confirm), and every feature is listed in the [feature index](../../reference/features.md#forms-validation-and-uploads).
