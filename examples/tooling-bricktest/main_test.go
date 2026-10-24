package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// live renders the page with an in-memory upload sink and returns the
// page the tests drive.
func live(t *testing.T) (*bricktest.LivePage, *bricktest.MemoryUploads) {
	t.Helper()
	sink := bricktest.NewMemoryUploads()
	eng, err := newEngine("theme", sink)
	if err != nil {
		t.Fatal(err)
	}
	return bricktest.Live(t, eng, page), sink
}

// Submitting the form runs the action the way the runtime does: the
// deferred fields ride along, the rules run, and the flash is an effect.
func TestSignup(t *testing.T) {
	page, _ := live(t)
	form := page.Component("Signup")

	// Bad input: the action returns the rule errors and the page shows them.
	form.Submit("#signup", map[string]any{"name": "A", "email": "nope"})
	form.AssertErrors(t, "name", "email")
	if form.View().Errors["email"] == "" {
		t.Fatalf("no email error: %+v", form.View().Errors)
	}

	// Good input: errors clear, the count goes up, a flash is recorded.
	res := form.Submit("#signup", map[string]any{"name": "Ada", "email": "ada@example.com"})
	form.AssertNoErrors(t)
	form.AssertSet(t, "accounts", 1)
	form.AssertSet(t, "name", "")
	bricktest.AssertFlash(t, res, "success", "Welcome, Ada!")
	bricktest.AssertRendered(t, res)
	if got := form.Text(); !strings.Contains(got, "1 accounts") {
		t.Fatalf("text: %q", got)
	}
}

// Upload sends a file through the whole pipeline into MemoryUploads;
// TryUpload returns a refusal instead of failing the test.
func TestAvatarUpload(t *testing.T) {
	page, sink := live(t)
	avatar := page.Component("Avatar")
	png := []byte("\x89PNG\r\n\x1a\n0000")
	avatar.Upload("avatar", "me.png", "image/png", png)
	if html := avatar.HTML(); !strings.Contains(html, `alt="me.png"`) {
		t.Fatalf("html: %s", html)
	}
	if b, ref, ok := sink.File("u1"); !ok || string(b) != string(png) || ref.Type != "image/png" {
		t.Fatalf("stored: %q %+v %v", b, ref, ok)
	}
	_, err := avatar.TryUpload("avatar", "notes.txt", "text/plain", []byte("hello"))
	if !errors.Is(err, brick.ErrUploadType) || sink.Len() != 1 {
		t.Fatalf("text file: %v, files %d", err, sink.Len())
	}
	t.Logf("refused: %v", err)
}

// The view log shows what a template saw, including a loader's values it
// never prints.
func TestPriceLoader(t *testing.T) {
	page, _ := live(t)
	page.AssertViewHas(t, "Price", "sku", "mug")
	page.AssertViewHas(t, "Price", "currency", "EUR")
	if v := page.Views("Price"); len(v) != 1 || v[0].Loaded["amount"] != "12.00" {
		t.Fatalf("views: %+v", v)
	}
}

// One component alone, compared with a golden file (go test -update
// rewrites it).
func TestPriceGolden(t *testing.T) {
	eng, err := newEngine("theme", bricktest.NewMemoryUploads())
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.RenderComponent(context.Background(), "Price", map[string]any{"sku": "mug"}, "")
	if err != nil {
		t.Fatal(err)
	}
	bricktest.Golden(t, "price", out)
}
