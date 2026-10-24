# Sealed state and key rings

## What it is and when you want it

A stateful component's state has to live somewhere between one click and the next. Sealed state puts the whole instance into the page itself, encrypted and authenticated with AES-256-GCM, so the server keeps nothing: any process that holds the same key ring opens it after a restart, a deploy, on a second server behind a load balancer, or from a static export. You want it when you run more than one process or restart often, and the state is small (the default cap is 64 KiB per component). Larger or shared state belongs in a server store ([State stores](state-stores.md)).

## Before you start

- A stateful component ([Stateful components](../interactivity/stateful-components.md)): `state:` in its frontmatter and an action.
- A key. `brick keygen` prints one: 32 random bytes as base64. Keep it in a secret manager, never in code or `brick.yaml` ([`brick keygen`](../../reference/cli.md#brick-keygen)).
- The example this page builds is [examples/production-sealed-state](../../../examples/production-sealed-state/).

## Step 1 — A stateful component

Nothing in the component says where its state is kept; the same file works with any store.

<!-- snippet: examples/production-sealed-state/theme/components/Counter.html -->
```html
---
state:
  count: 0
---
<brick:script>
def bump(state, props):
    return dict(state, count = state["count"] + 1)
</brick:script>
<div class="counter"><button brick:click="bump">Clicked {{ .State.count }} times</button></div>
```

The page shows it and links the runtime with `{{ assets }}`:

<!-- snippet: examples/production-sealed-state/theme/content/index.html -->
```html
<h1>Sealed counter</h1>
<Counter />
{{ assets }}
```

## Step 2 — Read the key ring and seal with it

`brick.KeysFromEnv` reads a comma-separated list of base64 keys, newest first, from one variable: the first key seals, every key opens. It takes the lookup function as a parameter (`os.LookupEnv` in `main`, a map in a test), so nothing reads the environment behind your back. `NewAESGCMSealer` turns the ring into a `Sealer`, and `NewSealedStates` makes it a state store. `TTL` bounds how old a blob may be; each action re-seals the instance, so a component in use keeps renewing it.

<!-- snippet: examples/production-sealed-state/main.go lines=46-81 -->
```go
// newApp builds the engine with sealed state. lookup is where the keys
// come from (os.LookupEnv in main, a map in tests), so the program never
// reads the environment behind its caller's back.
func newApp(dir string, lookup func(string) (string, bool)) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	keys, err := brick.KeysFromEnv(lookup, secretEnv)
	if err != nil {
		return nil, err
	}
	a := &app{revoked: map[string]time.Time{}}
	states, err := brick.NewSealedStates(brick.SealedOptions{
		Sealer:  brick.NewAESGCMSealer(keys, nil),
		TTL:     time.Hour,
		Revoked: a.isRevoked,
	})
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	a.eng, err = brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    states,
		Sessions:  brick.SessionFunc(sessionOf),
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}
```

`Sessions` and `Revoked` bind the state to the visitor and end it on sign-out; [Sessions and sign-out](sessions.md) explains both. The action endpoint and the runtime's assets are mounted beside the page:

<!-- snippet: examples/production-sealed-state/main.go lines=110-142 -->
```go
// handler serves the page, the actions and sign-out.
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("POST /signout", func(w http.ResponseWriter, r *http.Request) {
		if id, _ := sessionOf(r); id != "" {
			a.mu.Lock()
			a.revoked[hashSession(id)] = time.Now()
			a.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, MaxAge: -1, Path: "/"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
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
	return mux
}
```

## Step 3 — Rotate a key without logging anyone out

Rotation is the order of the ring. Deploy with the new key first and the old one second (`BRICK_SECRET=new,old`): new blobs are sealed with the new key, and pages still open in browsers keep working because the old key still opens them. After one TTL, every blob sealed with the old key has expired; deploy again with `BRICK_SECRET=new`. A blob whose key has left the ring answers `410 Gone`, and the runtime recovers by reloading the component with fresh state. The example's test runs exactly that sequence:

<!-- snippet: examples/production-sealed-state/main_test.go lines=87-101 -->
```go
// Rotation: a ring with the new key first still opens blobs sealed by the
// old one; once the old key is removed they are gone (410), and the
// runtime recovers by reloading the component.
func TestKeyRotation(t *testing.T) {
	oldKey, newKeyB := newKey(t), newKey(t)
	handle, token := openPage(t, start(t, oldKey), "alice")
	if rec := click(start(t, newKeyB+","+oldKey), "alice", handle, token); rec.Code != http.StatusOK {
		t.Fatalf("new,old ring: %d %s", rec.Code, rec.Body)
	}
	rec := click(start(t, newKeyB), "alice", handle, token)
	if rec.Code != http.StatusGone {
		t.Fatalf("old key removed: want 410, got %d %s", rec.Code, rec.Body)
	}
	t.Logf("old key removed: %d %s", rec.Code, strings.TrimSpace(rec.Body.String()))
}
```

## Keys from a KMS

A static key in an environment variable is the simplest ring. When the key must never sit in plain text in your deploy config, keep a *wrapped* data key instead (ciphertext, safe to store anywhere) and let a KMS unwrap it once at start; sealing then runs locally with no KMS call per request. Three contrib modules build such a ring over your own client, so they never open a connection or read credentials themselves:

| Module | Wraps the data key with | Options |
|---|---|---|
| [contrib/keys-awskms](../../../contrib/keys-awskms/) | AWS KMS `Decrypt` / `GenerateDataKey` | `keysawskms.Options{KeyID, Wrapped}` |
| [contrib/keys-gcpkms](../../../contrib/keys-gcpkms/) | Google Cloud KMS `Encrypt` / `Decrypt`, with CRC32C checks | `keysgcpkms.Options{KeyName, Wrapped, AAD}` |
| [contrib/keys-vault](../../../contrib/keys-vault/) | HashiCorp Vault transit | `keysvault.Options{KeyName, Wrapped}` |

Each returns a `brick.KeyRing`, which goes into the same two calls as Step 2. The AWS version, from the module's runnable `Example` (an offline stand-in plays the KMS):

<!-- snippet: contrib/keys-awskms/example_test.go lines=85-99 -->
```go
	keys, err := keysawskms.New(ctx, client, keysawskms.Options{KeyID: exampleKeyID, Wrapped: wrapped})
	if err != nil {
		fmt.Println(err) // never start without the key
		return
	}
	states, err := brick.NewSealedStates(brick.SealedOptions{Sealer: brick.NewAESGCMSealer(keys, nil)})
	if err != nil {
		fmt.Println(err)
		return
	}
	eng, err := brick.New(brick.Options{FS: counterTheme, States: states})
	if err != nil {
		fmt.Println(err)
		return
	}
```

The Vault transit version differs only in who wraps the key. A Vault `api.Client`'s `Logical()` is the writer; the example's offline stand-in plays transit:

<!-- snippet: contrib/keys-vault/example_test.go lines=71-80 -->
```go
	opts.Wrapped, err = keysvault.ParseWrapped(w) // e.g. os.Getenv("BRICK_WRAPPED_KEYS")
	if err != nil {
		fmt.Println(err)
		return
	}
	keys, err := keysvault.New(ctx, vault, opts)
	if err != nil {
		fmt.Println(err) // never start without the key
		return
	}
```

Google Cloud KMS takes the client and an optional additional authenticated data value (`AAD`), so a wrapped key made for one app is refused by another:

<!-- snippet: contrib/keys-gcpkms/example_test.go lines=91-103 -->
```go
	stored := keysgcpkms.EncodeWrapped(w) // e.g. os.Getenv("BRICK_WRAPPED_KEYS")

	// At start: decrypt the data keys once and build the ring.
	wrapped, err := keysgcpkms.ParseWrapped(stored)
	if err != nil {
		fmt.Println(err)
		return
	}
	keys, err := keysgcpkms.New(ctx, client, keysgcpkms.Options{KeyName: exampleKey, Wrapped: wrapped, AAD: aad})
	if err != nil {
		fmt.Println(err) // never start without the key
		return
	}
```

A data key wrapped under a different KMS key is refused when the ring is built, so a misconfigured deploy fails at start rather than at the first click: `keys-awskms: decrypt data key 1: IncorrectKeyException`. Any other ring (your own table, a secrets service) only has to implement `brick.KeyRing`'s two methods, `Current` and `Lookup`, and pass `statetest.KeyRing` ([statetest](../tooling/statetest.md)).

## Try it

From `examples/`:

```console
$ BRICK_SECRET=$(brick keygen) go run ./production-sealed-state
2026/10/14 10:04:59 serving on :8084
```

In another terminal, load the page, click once, sign out, and click again. The handle is a sealed blob a few hundred characters long; it is shortened to `<sealed blob>` in the output below.

```sh
curl -s -c jar http://localhost:8084/ > page.html
handle=$(grep -o 'data-brick-state="[^"]*"' page.html | cut -d'"' -f2)
token=$(grep -o 'data-brick-token="[^"]*"' page.html | cut -d'"' -f2)
click() { curl -s -w ' [%{http_code}]\n' -b jar -H 'Content-Type: application/json' -H 'Brick-Action: 1' -d "{\"handle\":\"$handle\",\"token\":\"$token\",\"action\":\"bump\"}" http://localhost:8084/_brick/action; }
click
curl -s -o /dev/null -b jar -X POST http://localhost:8084/signout
click
```

```console
<div data-brick="bf2734d64" data-brick-state="default.s1.<sealed blob>" data-brick-token="<token>" data-brick-name="Counter" class="counter"><button brick:click="bump">Clicked 1 times</button></div> [200]
brick: this component's state was not found; the page recovers it
 [410]
```

Stop the server, start it again with the same `BRICK_SECRET`, and the same action still answers `Clicked 1 times`: the state was in the page, not in the process. The tests:

```console
$ go test -v ./production-sealed-state/
=== RUN   TestSurvivesRestart
--- PASS: TestSurvivesRestart (0.00s)
=== RUN   TestKeyRotation
    main_test.go:100: old key removed: 410 brick: this component's state was not found; the page recovers it
--- PASS: TestKeyRotation (0.00s)
=== RUN   TestBoundToSession
--- PASS: TestBoundToSession (0.00s)
=== RUN   TestSignOut
--- PASS: TestSignOut (0.00s)
=== RUN   TestBadSecret
    main_test.go:134: BRICK_SECRET="": brick: BRICK_SECRET is not set; generate a key with brick keygen
    main_test.go:134: BRICK_SECRET="not-base64!": BRICK_SECRET: brick: state key 1 is not base64
    main_test.go:141: BRICK_SECRET unset: brick: BRICK_SECRET is not set; generate a key with brick keygen
--- PASS: TestBadSecret (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/production-sealed-state	0.030s
```

## Pitfalls

- **A missing secret fails at start, on purpose.** `KeysFromEnv` returns `brick: BRICK_SECRET is not set; generate a key with brick keygen` for an unset or empty variable, and `brick: state key 1 is not base64` for a malformed one. Do not fall back to a random key per process: every restart would then log every visitor out of their state, and two servers could not open each other's blobs.
- **Removing the old key too early resets components.** Keep `new,old` for at least one `TTL` (the default `DefaultSealedTTL` is 12 hours). Until then, a blob sealed with the old key answers 410 and the runtime reloads the component with fresh state, losing what the user had typed.
- **The handle names its store.** A sealed handle reads `default.s1.…`: the store name, then the format. Code that inspects handles must not assume the blob starts at the first byte.
- **Sealed state is in the page, so it is bounded.** A blob over `SealedOptions.MaxBytes` (default 64 KiB) fails the render, naming the component. Move large lists to a server store with `brick: {store: name}` ([State stores](state-stores.md)).
- **Encrypted is not hidden from the user's own actions.** Sealing stops the browser reading or changing state, but every field a binding writes is still writable through `$set`. Turn on `Options.LockState` and list `writable:` fields to close the rest ([URL and server state](../interactivity/url-and-server-state.md)).
- **`brick serve` reads the same variable.** The CLI seals with `BRICK_SECRET` too, so one secret works for `brick serve` in development and for your own binary in production ([CLI reference](../../reference/cli.md#brick-serve)).

## Related

- [Sessions and sign-out](sessions.md) · [State stores](state-stores.md) · [statetest](../tooling/statetest.md)
- [Feature index: State storage and security](../../reference/features.md#state-storage-and-security)
- Example: [examples/production-sealed-state](../../../examples/production-sealed-state/) · contrib: [keys-awskms](../../../contrib/keys-awskms/), [keys-gcpkms](../../../contrib/keys-gcpkms/), [keys-vault](../../../contrib/keys-vault/)
