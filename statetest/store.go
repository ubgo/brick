// Package statetest is the conformance suite for brick's state pieces
// : Store, Sealer, Codec and KeyRing. The built-in
// pieces run it, and so should every contrib or host piece, so all of them
// agree on what a round trip, a missing handle, a delete, a revocation, a
// tampered blob and a retired key mean.
//
//	func TestMyStore(t *testing.T) {
//		statetest.Store(t, func(t *testing.T) brick.StateStore { return newMyStore(t) }, statetest.StoreOptions{})
//	}
package statetest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ubgo/brick"
)

// StoreOptions describe what the store under test keeps.
type StoreOptions struct {
	// Stateless marks a store that keeps nothing server-side (sealed): its
	// handle changes on every Put and Delete forgets nothing.
	Stateless bool
}

// concurrency is how many goroutines the race test runs.
const concurrency = 64

// Store runs the store suite. newStore returns a fresh, empty store for
// each subtest.
func Store(t *testing.T, newStore func(t *testing.T) brick.StateStore, opts StoreOptions) {
	t.Helper()
	ctx := context.Background()
	scoped := func(session string) context.Context {
		return brick.WithStateScope(ctx, brick.StateScope{Store: "default", Session: session})
	}

	t.Run("RoundTrip", func(t *testing.T) {
		s := newStore(t)
		in := Sample("")
		h, err := s.Put(scoped(""), in)
		if err != nil || h == "" {
			t.Fatalf("Put: %q %v", h, err)
		}
		out, ok, err := s.Get(scoped(""), h)
		if err != nil || !ok {
			t.Fatalf("Get: %v %v", ok, err)
		}
		if out.Handle != h {
			t.Errorf("Get sets Handle to the handle asked for: %q, want %q", out.Handle, h)
		}
		out.Handle = ""
		if !reflect.DeepEqual(out, in) {
			t.Errorf("round trip changed the instance:\n got %#v\nwant %#v", out, in)
		}
	})

	t.Run("Missing", func(t *testing.T) {
		s := newStore(t)
		for _, h := range []string{"", "nope", "00000000000000000000000000000000", "s1.AAAA"} {
			inst, ok, err := s.Get(scoped(""), h)
			if err != nil || ok || inst != nil {
				t.Errorf("Get(%q) = %v %v %v; want not found, no error", h, inst, ok, err)
			}
		}
	})

	t.Run("CopiesNotShares", func(t *testing.T) {
		s := newStore(t)
		in := Sample("")
		h := mustPut(t, s, scoped(""), in)
		in.State["email"] = "changed@example.com"
		out := mustGet(t, s, scoped(""), h)
		if out.State["email"] == "changed@example.com" {
			t.Fatal("a store must keep a copy: changing the caller's map changed the stored state")
		}
		out.State["email"] = "again"
		again := mustGet(t, s, scoped(""), h)
		if again.State["email"] == "again" {
			t.Fatal("Get must return a copy")
		}
	})

	t.Run("PutAgain", func(t *testing.T) {
		s := newStore(t)
		in := Sample("")
		h1 := mustPut(t, s, scoped(""), in)
		in.Handle = h1
		in.State["n"] = int64(2)
		in.Version++
		h2, err := s.Put(scoped(""), in)
		if err != nil {
			t.Fatal(err)
		}
		if !opts.Stateless && h2 != h1 {
			t.Errorf("a server store keeps the handle on a later Put: %q then %q", h1, h2)
		}
		out, ok := getNoErr(t, s, scoped(""), h2)
		if !ok || out.State["n"] != int64(2) || out.Version != 1 {
			t.Fatalf("second Put not seen: %v %#v", ok, out)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		s := newStore(t)
		h := mustPut(t, s, scoped(""), Sample(""))
		if err := s.Delete(scoped(""), h); err != nil {
			t.Fatal(err)
		}
		_, ok := getNoErr(t, s, scoped(""), h)
		if !opts.Stateless && ok {
			t.Fatal("Delete must forget the instance")
		}
		if err := s.Delete(scoped(""), "nope"); err != nil {
			t.Fatalf("deleting a missing handle is not an error: %v", err)
		}
	})

	t.Run("RevokeSession", func(t *testing.T) {
		s := newStore(t)
		r, ok := s.(brick.Revoker)
		if !ok {
			t.Skip("not a Revoker")
		}
		ha := mustPut(t, s, scoped("aaaa"), Sample("aaaa"))
		hb := mustPut(t, s, scoped("bbbb"), Sample("bbbb"))
		if err := r.RevokeSession(ctx, "aaaa"); errors.Is(err, errors.ErrUnsupported) {
			t.Skip("Revoker that reports ErrUnsupported (an adapter over a store without it)")
		} else if err != nil {
			t.Fatal(err)
		}
		if _, ok := getNoErr(t, s, scoped("aaaa"), ha); ok {
			t.Error("a revoked session's instance must be gone")
		}
		if _, ok := getNoErr(t, s, scoped("bbbb"), hb); !ok {
			t.Error("another session's instance must survive")
		}
	})

	t.Run("Each", func(t *testing.T) {
		s := newStore(t)
		l, ok := s.(brick.Lister)
		if !ok {
			t.Skip("not a Lister")
		}
		for range 3 {
			mustPut(t, s, scoped(""), Sample(""))
		}
		n := 0
		err := l.Each(ctx, func(*brick.Instance) bool { n++; return true })
		if errors.Is(err, errors.ErrUnsupported) {
			t.Skip("Lister that reports ErrUnsupported (an adapter over a store without it)")
		}
		if err != nil || n != 3 {
			t.Fatalf("Each saw %d (want 3): %v", n, err)
		}
		n = 0
		if err := l.Each(ctx, func(*brick.Instance) bool { n++; return false }); err != nil {
			t.Fatalf("Each stopped early: %v", err)
		}
		if n != 1 {
			t.Fatalf("Each must stop when fn returns false; saw %d", n)
		}
	})

	t.Run("Concurrent", func(t *testing.T) {
		s := newStore(t)
		var wg sync.WaitGroup
		errs := make(chan error, concurrency)
		for i := range concurrency {
			wg.Add(1)
			go func() {
				defer wg.Done()
				in := Sample("")
				in.State["n"] = int64(i)
				h, err := s.Put(scoped(""), in)
				if err != nil {
					errs <- err
					return
				}
				out, ok, err := s.Get(scoped(""), h)
				if err != nil || !ok || out.State["n"] != int64(i) {
					errs <- fmt.Errorf("goroutine %d: %v %v %v", i, ok, err, out)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
	})
}

// mustPut stores in and returns its handle, failing the test at once when
// Put fails. A suite step that ignored the error would carry on with an
// empty handle, and a later "must be gone" check would then pass on a store
// that never stored anything.
func mustPut(t *testing.T, s brick.StateStore, ctx context.Context, in *brick.Instance) string {
	t.Helper()
	h, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	return h
}

// getNoErr reads handle h, failing the test when Get errors; whether the
// instance is there is the caller's assertion, since a deleted or revoked
// handle must report not found without an error.
func getNoErr(t *testing.T, s brick.StateStore, ctx context.Context, h string) (*brick.Instance, bool) {
	t.Helper()
	out, ok, err := s.Get(ctx, h)
	if err != nil {
		t.Fatalf("Get(%q): %v", h, err)
	}
	return out, ok
}

// mustGet reads handle h and fails the test unless the instance is there.
func mustGet(t *testing.T, s brick.StateStore, ctx context.Context, h string) *brick.Instance {
	t.Helper()
	out, ok := getNoErr(t, s, ctx, h)
	if !ok || out == nil {
		t.Fatalf("Get(%q): not found right after Put", h)
	}
	return out
}

// Sample is an instance holding every JSON-compatible kind, in the shape a
// decoder returns (int64, float64, []any, map[string]any), so a round trip
// through any store compares equal with reflect.DeepEqual.
func Sample(session string) *brick.Instance {
	return &brick.Instance{
		Token: "4a7d00000000000000000000000000ff", Store: "default",
		Path: "~/components/SignUp.html", Name: "SignUp", ID: "b1",
		PagePath: "content/join.html", Locale: "en",
		Props: map[string]any{"plan": "pro"},
		Ctx:   map[string]any{"theme": "dark"},
		State: map[string]any{
			"email": "sara@example.com", "n": int64(3), "price": 3.0, "ok": true, "none": nil,
			"tags": []any{"a", int64(1)}, "nested": map[string]any{"x": 1.5},
		},
		Keys:    []string{"email", "n", "nested", "none", "ok", "price", "tags"},
		Session: session,
		Version: 0,
		Issued:  time.Date(2026, 10, 10, 9, 12, 44, 0, time.UTC),
	}
}
