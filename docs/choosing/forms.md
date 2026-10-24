# Choosing a form approach

A form in brick is a stateful component whose fields bind to its state and whose submit calls an action. The choices are how fields travel to the server, what happens when JavaScript is missing, where checks run, and where uploaded bytes go. This page shows which combination to pick.

## The decision

| Question | Options | Pick | Guide | Example |
|---|---|---|---|---|
| When do field values reach the server? | live `brick:model`, or `brick:model.defer` | `.defer` for any form that is filled in and then saved; live for a search box or filter that reacts while typing | [Deferred fields](../guides/forms/deferred-fields.md) | [forms-signup](../../examples/forms-signup/) |
| What if scripts are off or blocked? | rely on the runtime, or `brick:submit` as a plain post | `brick:submit`: the engine renders `method="post"` plus the instance fields, so the form works without JavaScript | [Forms without JavaScript](../guides/forms/without-javascript.md) | [forms-nojs](../../examples/forms-nojs/) |
| Who answers a plain post? | the site layer, or your router | the site layer (`brick serve`) does it for you; in your own router use `Engine.IsFormPost` and `Engine.FormPost` | [Forms without JavaScript](../guides/forms/without-javascript.md#step-2--answer-the-post-with-engineformpost) | [forms-nojs](../../examples/forms-nojs/) |
| Where are the rules written? | in the action, or `rules:` in frontmatter | `rules:` and `messages:` once, so `.Errors` is computed at every render for the fields the user has touched and the submit action checks everything with `brick.validate()` | [Validation](../guides/forms/validation.md) | [forms-validation-lists](../../examples/forms-validation-lists/) |
| A rule brick does not have? | a host rule | `Options.Validator`, named in `rules:` like a built-in | [Validation, step 3](../guides/forms/validation.md#step-3--add-your-own-rule-with-optionsvalidator) | [forms-validation-lists](../../examples/forms-validation-lists/) |
| Rows in a list? | repeat a rule per row | a wildcard path such as `todos.*.title` | [Validation, step 2](../guides/forms/validation.md#step-2--check-every-row-of-a-list-with--and-render-it-with-form) | [forms-validation-lists](../../examples/forms-validation-lists/) |
| A file field? | proxied through the engine, or presigned | proxied (`upload/disk` or any `UploadSink`) when the file must be checked; presigned when the file is large and the bytes can go straight to storage | [Uploads](../guides/forms/uploads.md) | [forms-uploads](../../examples/forms-uploads/) |
| A Delete or Log out link? | a form per link, or a method link | `brick:method` on the `<a href>` | [Method links](../guides/forms/method-links.md) | [methodlinks](../../examples/methodlinks/) |

## Uploads: proxied or presigned

| | Proxied (`UploadSink.Open`) | Presigned (`UploadPresigner`) |
|---|---|---|
| Where the bytes travel | browser, then your server, then the sink | browser straight to storage |
| Type check | the type is sniffed from the bytes, never taken from the browser, and `Options.UploadCheck` sees the first bytes | not sniffed: the stored size is confirmed, the type is the `Content-Type` the client signed for, and `UploadCheck` runs without the head bytes |
| Size limit | `max_size` enforced while the bytes stream | the engine accepts only an id it issued to that instance within the hour, and checks the limits again on what `Confirm` reads from storage |
| Works with JavaScript off | yes, a plain multipart post goes through `Open` | the sink still needs a working `Open` for this case |
| Ready-made | `upload/disk`; `contrib/upload-s3` as the sink | `contrib/upload-s3` offers presigned PUT URLs |
| Pick it when | files are small, or must be scanned or refused by content | files are large and your server should not carry them |

Both need `Options.Uploads`; without a sink a component that declares `uploads:` fails to compile, so files are never dropped silently.

## If X then Y

- **A sign-up or contact form:** deferred fields, `rules:`, and `brick:submit`. One request on submit, errors shown as each field is left, and it works with scripts off.
- **A search box or a filter bar:** live `brick:model` with `.debounce` or `.lazy`, or `query:` fields to keep the value in the URL ([State in the URL](../guides/interactivity/url-and-server-state.md)).
- **A form in a plain Go handler:** `Engine.IsFormPost(r)` then `Engine.FormPost(r)`. Check cross-site posts: the engine refuses a post from another origin.
- **A destructive action behind a confirm:** `brick:confirm` on the button; for a method link use plain `brick:confirm` (the `.prompt` form is refused there).
- **A list the user adds rows to:** list paths (`items.0.qty`), wildcard rules, and `.Form` objects to read the submitted values in templates.

## Pitfalls

- **A misspelled action name is not a compile error.** `brick:click="sav"` compiles and answers `400 brick: unknown action` at click time; modifier typos and bad JSON arguments are compile errors. `bricktest` can drive the form and catch it ([Testing with bricktest](../guides/tooling/bricktest.md)).
- **Without JavaScript a method link is an ordinary GET.** The route it points to must never change anything on GET.
- **brick has no CSRF token for plain forms.** The engine checks the post's origin; if your app has its own token, add it with `brick.methodFormWith` for method links and in your own handler for forms.
- **`uploads:` needs a sink**, and `$set` cannot write an upload field, so a client cannot point it at someone else's file.
- **A deferred field sends nothing on its own.** If an action never runs (no submit), nothing is saved; this is the point, but test the "leave the page" case.

Back to [choosing guides](README.md).
