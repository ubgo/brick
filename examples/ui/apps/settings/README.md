# Settings

Profile, account, notifications, appearance and billing — five stateful forms built only from kit components.

Open it at http://localhost:8002/apps/settings/ (`task ui:serve`).

## Files

| File | What it holds |
|---|---|
| `SettingsAccount.html` | The Account section — e-mail, language, time zone and birthday, plus a danger zone whose ConfirmDialog runs the deleteAccount action. |
| `SettingsAppearance.html` | The Appearance section. |
| `SettingsBilling.html` | The Billing section — the current plan with its usage, a plan change validated against that usage (Hobby holds one project, and the demo account has seven), the payment method with an inline edit form, and the invoice table. |
| `SettingsNotifications.html` | The Notifications section — e-mail and push switches grouped in Fieldsets and a digest frequency. |
| `SettingsProfile.html` | The Profile section of the Settings app — a stateful form whose save action validates every field and returns [state, errors], so the errors render inline and the typed values survive. |
| `../../content/apps/settings.html` | The page: `layout: app`, `AppShell` with the shared `AppNav`, this app's component, and the View source tabs. It stays in `content/` because brick derives URLs from there. |
| `../../apps_settings_test.go` | The app's tests: they load the page and post to `/_brick/action` the way the runtime does. |

## Test

```sh
task ui:test:app APP=settings   # only this app's tests
task ui:test                # every showcase test
```
