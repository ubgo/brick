package main

import (
	"net/http"
	"strings"
	"testing"
)

// The sign-in screen: bad input returns inline errors, a wrong password a
// form-level alert, the demo account the success panel; register mode adds
// the name and terms fields and creates an account.
func TestAuthSignIn(t *testing.T) {
	_, h := showcase(t)
	code, body := get(t, h, "/apps/auth/")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	landCallsParse(t, body)
	mustContain(t, "auth", body, `brick:submit="sign_in"`, `Welcome back`, `demo@brick.dev`, `Forgot password?`, `or continue with email`, `type="password"`, `name="remember"`, `CTO, Northwind`)

	s := landOpen(t, h, "/apps/auth/")
	out := s.act("sign_in", map[string]string{"email": "nope", "password": "short"})
	mustContain(t, "bad input", out, "Enter an email address like you@company.com.", "Passwords are at least 8 characters.", `aria-invalid="true"`, `value="nope"`)
	if strings.Contains(out, "short") {
		t.Error("the password was echoed back into the page")
	}
	out = s.act("sign_in", map[string]string{"email": "demo@brick.dev", "password": "wrongpass"})
	mustContain(t, "wrong password", out, "Sign-in failed", "don&#39;t match")
	out = s.act("sign_in", map[string]string{"email": "Demo@Brick.dev ", "password": "brickyard", "remember": "on"})
	mustContain(t, "demo account", out, "Welcome back, Demo", "demo@brick.dev on this device for 30 days", `href="/apps/dashboard/"`, `brick:click="sign_out"`)
	if strings.Contains(out, "bk-field__error") {
		t.Errorf("success still shows errors:\n%s", out)
	}

	out = s.act("sign_out")
	mustContain(t, "sign out", out, `brick:submit="sign_in"`)
	out = s.act("show_register")
	landCallsParse(t, out)
	mustContain(t, "register mode", out, "Create your account", `name="name"`, `name="agree"`, `autocomplete="new-password"`)
	out = s.act("sign_in", map[string]string{"name": "A", "email": "ada@example.com", "password": "longenough"})
	mustContain(t, "register invalid", out, "Tell us your name.", "Please accept the terms to create an account.")
	out = s.act("sign_in", map[string]string{"name": "Ada Lovelace", "email": "ada@example.com", "password": "longenough", "agree": "on"})
	mustContain(t, "register valid", out, "Welcome aboard, Ada Lovelace", "ada@example.com")

	// ?mode=register opens the register form on a fresh visit, and the first
	// submit validates as a registration; after any action the query no
	// longer overrides the state.
	_, reg := get(t, h, "/apps/auth/?mode=register")
	mustContain(t, "?mode=register", reg, "Create your account", `name="agree"`, `<input type="hidden" name="mode" value="register">`)
	r := landOpen(t, h, "/apps/auth/?mode=register")
	out = r.act("sign_in", map[string]string{"mode": "register", "name": "", "email": "grace@example.com", "password": "longenough"})
	mustContain(t, "first submit registers", out, "Tell us your name.", "Create your account")
	out = r.act("show_sign_in")
	mustContain(t, "state wins after an action", out, "Welcome back", `name="remember"`)
	_, plain := get(t, h, "/apps/auth/?mode=bogus")
	mustContain(t, "unknown mode", plain, "Welcome back")
	out = s.act("show_sign_in")
	mustContain(t, "back to sign in", out, "Welcome back", `name="remember"`)
}
