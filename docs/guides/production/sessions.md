# Sessions and sign-out

## What it is and when you want it

A stateful component's handle is in the page, so anyone who copies it could replay it. Session binding ties every instance to the session that rendered it: brick asks your app "who is this request?", keeps only the SHA-256 of the answer with the instance, and an action from any other session finds the instance gone (`410`). Sign-out then ends a session's state at once, before it expires. You want both as soon as a component holds anything a user would not want another user to see or change.

## Before you start

- Your app already has sessions (a cookie, a token). brick never makes or reads one itself; it only asks you for the id.
- A state store: sealed ([Sealed state](sealed-state.md)) or a server store ([State stores](state-stores.md)). Sign-out works differently for each, as Step 3 shows.
- Examples: [examples/production-sealed-state](../../../examples/production-sealed-state/) (sealed) and [examples/production-state-stores](../../../examples/production-state-stores/) (server stores).

## Step 1 — Tell brick who the request is

`Options.Sessions` is a `brick.SessionBinder`; `brick.SessionFunc` adapts a plain function. Return `""` for a signed-out visitor: that binds nothing, and the state is public.

<!-- snippet: examples/production-sealed-state/main.go lines=83-91 -->
```go
// sessionOf answers "who is this request?" from the cookie; no cookie is
// no session, which binds nothing.
func sessionOf(r *http.Request) (string, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", nil
	}
	return c.Value, nil
}
```

The action handler calls it on every action. A page render has no `*http.Request` to ask, so an engine-only host names the session itself with `brick.WithSession(id)` (the site layer does this for you from the same `Options.Sessions`):

<!-- snippet: examples/production-sealed-state/main.go lines=124-140 -->
```go
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := sessionOf(r)
		if id == "" {
			id = rand.Text()
			http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		}
		// An engine-only host names the render's session itself; the
		// action handler asks Options.Sessions on every action.
		html, err := a.eng.Render(r.Context(), page, nil, brick.WithSession(id))
		if err != nil {
			log.Printf("render %s: %v", page, err)
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
```

## Step 2 — Another session cannot replay the handle

With both in place, an action carrying another session's cookie answers exactly like a lost blob, so a prober learns nothing about whether the handle was real:

<!-- snippet: examples/production-sealed-state/main_test.go lines=103-112 -->
```go
// The blob is bound to the session that rendered it: another visitor
// replaying it gets 410, the same answer as a lost blob.
func TestBoundToSession(t *testing.T) {
	h := start(t, newKey(t))
	handle, token := openPage(t, h, "alice")
	rec := click(h, "mallory", handle, token)
	if rec.Code != http.StatusGone {
		t.Fatalf("other session: want 410, got %d %s", rec.Code, rec.Body)
	}
}
```

## Step 3 — End a session's state on sign-out

**Server stores.** `Engine.RevokeSession(ctx, id)` takes your raw session id and ends every instance bound to it in every store that implements the optional `brick.Revoker` upgrade (`MemoryStates`, `brick.Bytes` over a store that implements it, `contrib/states-sql`, `contrib/states-redis`), and closes that session's server-push connections. A store that cannot revoke returns `errors.ErrUnsupported` and is skipped.

<!-- snippet: examples/production-state-stores/main.go lines=152-158 -->
```go
	mux.HandleFunc("POST /signout", func(w http.ResponseWriter, r *http.Request) {
		u, _ := userOf(r)
		if err := eng.RevokeSession(r.Context(), u); err != nil {
			log.Printf("sign-out: %v", err)
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
```

A `BytesStore` you write gets revocation by adding one method; `Save` is handed the hashed session for exactly this:

<!-- snippet: examples/production-state-stores/main.go lines=86-98 -->
```go
// RevokeSession is the optional Revoker upgrade: Engine.RevokeSession
// (sign-out, "log out other devices") ends every value of one session.
// This is why Save is given the hashed session id.
func (m *mapStore) RevokeSession(_ context.Context, session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.rows {
		if r.session == session {
			delete(m.rows, k)
		}
	}
	return nil
}
```

**Sealed state.** A sealed store keeps nothing on the server, so there is nothing to delete; `RevokeSession` skips it. Instead, `SealedOptions.Revoked` is asked on every open whether a blob was issued before its session signed out. The app keeps the sign-out times, keyed by the hashed id the callback receives:

<!-- snippet: examples/production-sealed-state/main.go lines=93-98 -->
```go
// hashSession is how brick names a session to a store and to
// SealedOptions.Revoked: the hex SHA-256 of the app's id.
func hashSession(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}
```

<!-- snippet: examples/production-sealed-state/main.go lines=100-108 -->
```go
// isRevoked is SealedOptions.Revoked: a blob issued before its session
// signed out no longer opens. A sealed store keeps nothing on the server,
// so this list is the only way to end a session's state early.
func (a *app) isRevoked(_ context.Context, session string, issued time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	at, ok := a.revoked[session]
	return ok && !issued.After(at)
}
```

<!-- snippet: examples/production-sealed-state/main.go lines=115-123 -->
```go
	mux.HandleFunc("POST /signout", func(w http.ResponseWriter, r *http.Request) {
		if id, _ := sessionOf(r); id != "" {
			a.mu.Lock()
			a.revoked[hashSession(id)] = time.Now()
			a.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, MaxAge: -1, Path: "/"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
```

If your app rotates the session id on sign-out (it should), the old blobs already fail the binding check and `Revoked` is a second layer; it matters for apps that cannot rotate ids.

## Try it

From `examples/`:

```console
$ go test -v -run 'BoundToSession|SignOut' ./production-sealed-state/
=== RUN   TestBoundToSession
--- PASS: TestBoundToSession (0.00s)
=== RUN   TestSignOut
--- PASS: TestSignOut (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/production-sealed-state	0.025s
$ go test -v -run NamedStore ./production-state-stores/
=== RUN   TestNamedStore
--- PASS: TestNamedStore (0.01s)
PASS
ok  	github.com/ubgo/brick/examples/production-state-stores	0.021s
```

[Sealed state → Try it](sealed-state.md#try-it) shows the same sign-out with `curl` against the running server: the click before `/signout` answers `200`, the one after answers `410` with `brick: this component's state was not found; the page recovers it`.

## Pitfalls

- **`Revoked` receives the hashed id, not yours.** brick passes `hex(SHA-256(id))`, the same value stores see in `Instance.Session`. Hash your id the same way when you record a sign-out, as `hashSession` above does; brick does not export a helper for it.
- **A render without `WithSession` binds nothing.** If the page render forgets the option while `Options.Sessions` answers an id on actions, the binding check fails and every first click answers `410`. In an engine-only host, pass `brick.WithSession` on every render of a page with stateful components.
- **`RevokeSession` on sealed state alone does nothing.** It returns `nil` because no store implements `Revoker`; nothing tells you the blobs are still valid. Pair sealed state with `SealedOptions.Revoked`.
- **Signed out is a session too.** A guest's state is bound to the empty session, so a guest can click a guest page. To refuse actions from visitors who are not signed in, add `actionmw.RequireSession()` ([Action middleware](action-middleware.md)).
- **A `410` is not an error page.** The runtime treats it as "recover": it reloads the component with fresh state. That is what a user sees after sign-out, and after a key leaves the ring.

## Related

- [Sealed state and key rings](sealed-state.md) · [State stores](state-stores.md) · [Action middleware](action-middleware.md)
- [Feature index: State storage and security](../../reference/features.md#state-storage-and-security)
- Examples: [examples/production-sealed-state](../../../examples/production-sealed-state/), [examples/production-state-stores](../../../examples/production-state-stores/)
