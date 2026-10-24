package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// settingsURL is the Settings demo app.
const settingsURL = "/apps/settings/"

// settingsInstance is one stateful section of the Settings page: the
// handle and token its root carries.
type settingsInstance struct{ handle, token string }

var settingsRootRe = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([^"]+)"`)

// settingsSection finds the stateful section whose markup contains marker
// (its submit button's text): each root's markup runs to the next root.
func settingsSection(t *testing.T, page, marker string) settingsInstance {
	t.Helper()
	locs := settingsRootRe.FindAllStringSubmatchIndex(page, -1)
	for i, l := range locs {
		end := len(page)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		if strings.Contains(page[l[0]:end], marker) {
			return settingsInstance{page[l[2]:l[3]], page[l[4]:l[5]]}
		}
	}
	t.Fatalf("no stateful section containing %q", marker)
	return settingsInstance{}
}

// settingsAct posts one action to the showcase and returns the re-rendered
// section.
func settingsAct(t *testing.T, h http.Handler, in settingsInstance, action string, args ...any) string {
	t.Helper()
	code, out := postAction(t, h, in.handle, in.token, action, args...)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d\n%s", action, code, out)
	}
	return out
}

// The Settings page renders the app shell and all five sections.
func TestSettingsPage(t *testing.T) {
	_, h := showcase(t)
	code, page := get(t, h, settingsURL)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	mustContain(t, settingsURL, page,
		`class="bk-appshell`, `aria-current="page"`, `<h1`, `>Settings</h1>`,
		`role="tablist"`, `>Profile<`, `>Account<`, `>Notifications<`, `>Appearance<`, `>Billing<`,
		`name="username" value="ada"`, `brick.dev/</span>`, `class="bk-textarea__count" aria-hidden="true">89 / 160</span>`,
		`role="combobox"`, `<option value="Europe/London" selected>`, `type="date"`, `role="alertdialog"`, `brick:click="deleteAccount"`,
		`role="switch"`, `checked name="digest" value="weekly"`, `settings-preview--dark`, `bk-accent-picker`, `bk-segmented`,
		`Visa ending in 4242`, `INV-1042`, `bk-progress`, `bk-toaster`)
	if n := len(settingsRootRe.FindAllString(page, -1)); n != 5 {
		t.Errorf("%d stateful sections, want 5", n)
	}
}

// Profile: invalid fields come back inline with the typed values kept; a
// valid save shows a success toast.
func TestSettingsProfileAction(t *testing.T) {
	_, h := showcase(t)
	_, page := get(t, h, settingsURL)
	in := settingsSection(t, page, "Save profile")
	out := settingsAct(t, h, in, "save", map[string]string{"name": "A", "username": "Admin", "bio": "hi", "website": "example.com"})
	mustContain(t, "invalid profile", out, "Enter your name (at least two letters).", "brick.dev/admin is taken.", "Start the address with https://.", `aria-invalid="true"`, `value="example.com"`)
	if strings.Contains(out, "Profile saved") {
		t.Error("an invalid profile was saved")
	}
	out = settingsAct(t, h, in, "save", map[string]string{"name": "Ada", "username": "ada_l", "bio": "", "website": ""})
	mustContain(t, "bad username", out, "Use only lowercase letters, digits and dashes.")
	out = settingsAct(t, h, in, "save", map[string]string{"name": "Ada King", "username": "ada-king", "bio": "Countess.", "website": "https://ada.dev"})
	mustContain(t, "valid profile", out, `Profile saved`, `id="settings-profile-1"`, `value="ada-king"`, "brick.dev/ada-king", `>AK<`)
	if strings.Contains(out, "bk-field__error") {
		t.Errorf("a valid profile still shows errors:\n%s", out)
	}
}

// Notifications: a digest with every e-mail switch off is refused; switches
// that are absent from the fields are off; a valid save shows a toast.
func TestSettingsNotificationsAction(t *testing.T) {
	_, h := showcase(t)
	_, page := get(t, h, settingsURL)
	in := settingsSection(t, page, "Save preferences")
	out := settingsAct(t, h, in, "save", map[string]string{"digest": "daily", "pushMentions": "on"})
	mustContain(t, "digest without e-mail", out, "Turn on at least one e-mail notification to receive a digest.")
	if strings.Contains(regexp.MustCompile(`<input[^>]*name="comments"[^>]*>`).FindString(out), "checked") {
		t.Error("an absent switch stayed on")
	}
	out = settingsAct(t, h, in, "save", map[string]string{"digest": "daily", "updates": "on"})
	mustContain(t, "valid notifications", out, `Notification preferences saved`, `checked name="digest" value="daily"`)
	if !strings.Contains(regexp.MustCompile(`<input[^>]*name="updates"[^>]*>`).FindString(out), "checked") {
		t.Error("a switch that was on was not saved")
	}
}

// Account's danger zone and Billing's plan rule: the confirm action
// schedules deletion (undoable), and Hobby is refused while seven projects
// exist.
func TestSettingsAccountAndBillingActions(t *testing.T) {
	_, h := showcase(t)
	_, page := get(t, h, settingsURL)
	acct := settingsSection(t, page, "Save account")
	out := settingsAct(t, h, acct, "deleteAccount")
	mustContain(t, "delete", out, "Account scheduled for deletion", `brick:click="undoDelete"`)
	out = settingsAct(t, h, acct, "undoDelete")
	mustContain(t, "undo", out, `brick:click="deleteAccount"`)
	out = settingsAct(t, h, acct, "save", map[string]string{"email": "ada@example", "language": "de", "timezone": "Mars/Base", "birthday": "3000-01-01"})
	mustContain(t, "invalid account", out, "Enter an e-mail address like ada@example.com.", "Choose a time zone from the list.", "Your birthday cannot be in the future.")
	bill := settingsSection(t, page, "Change plan")
	out = settingsAct(t, h, bill, "changePlan", map[string]string{"plan": "hobby"})
	mustContain(t, "downgrade", out, "Hobby holds 1 project and you have 7. Archive 6 first.", `checked name="plan" value="pro"`)
	out = settingsAct(t, h, bill, "changePlan", map[string]string{"plan": "team"})
	mustContain(t, "upgrade", out, `Plan changed`, "<b>$49</b>")
	out = settingsAct(t, h, bill, "editCard")
	mustContain(t, "edit card", out, `New card`, `brick:submit="updateCard"`)
	out = settingsAct(t, h, bill, "updateCard", map[string]string{"number": "4111", "expiry": "13/30"})
	mustContain(t, "bad card", out, "Enter the 15 or 16 digits on your card.", "Use MM/YY, like 08/29.", `brick:submit="updateCard"`)
	out = settingsAct(t, h, bill, "updateCard", map[string]string{"number": "5555 4444 3333 1111", "expiry": "09/29"})
	mustContain(t, "card", out, "Visa ending in 1111", `Card updated`)
}
