# Sign in

A stateful sign-in and register form (`AuthSignInForm`) with `PasswordInput` and server-side validation. It has no script and no table: the form's actions are in the component.

Open it at http://localhost:8002/apps/auth/ (`task ui:serve`).

## Files

| File | What it holds |
|---|---|
| `AuthSignInForm.html` | The sign-in demo's form — sign in, or register, in one stateful component. |
| `../../content/apps/auth.html` | The page: `layout: app`, `AppShell` with the shared `AppNav`, this app's component, and the View source tabs. It stays in `content/` because brick derives URLs from there. |
| `../../apps_auth_test.go` | The app's tests: they load the page and post to `/_brick/action` the way the runtime does. |

## Test

```sh
task ui:test:app APP=auth   # only this app's tests
task ui:test                # every showcase test
```
