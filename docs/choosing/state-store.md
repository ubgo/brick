# Choosing a state store

A stateful component keeps its data between clicks. That data has to live somewhere, and the place you pick decides what survives a restart, whether a second server can answer the next click, and how much state one component may hold. This page helps you pick; the [state stores](../guides/production/state-stores.md) and [sealed state](../guides/production/sealed-state.md) guides build each option step by step.

## The decision

| Option | Where the state lives | Survives a restart | Works on several servers | Size limit | Pick it when |
|---|---|---|---|---|---|
| Memory (`brick.NewMemoryStates(max, ttl)`) | this process | no | no | `max` instances (zero means 10,000), unused ones expire after `ttl` (zero means 30 minutes) | development, one small server, disposable state such as a search box |
| Sealed (`brick.NewSealedStates`) | inside the page, encrypted and authenticated with AES-256-GCM | yes | yes, any server holding the same key ring | 64 KiB per component by default (`SealedOptions.MaxBytes`) | the production default when state is small; the only choice for a static export |
| SQL (`contrib/states-sql`) | a table in Postgres, MySQL or SQLite | yes | yes | what one row holds | you already run a database and state is large or must be listed and revoked |
| Redis (`contrib/states-redis`) | Redis | yes | yes | what one Redis value holds | you already run Redis, and you want revocation and expiry without a table |
| Your own (`brick.Bytes` over a `BytesStore`) | wherever you keep bytes under a key | yours | yours | yours | a store brick does not ship; prove it with [statetest](../guides/tooling/statetest.md) |

You can mix them. `Options.States` is the store named `default`, `Options.StateStores` adds named ones, and a component picks one in its frontmatter with `brick: {store: cart}`. A search box can stay in memory while a cart lives in SQL. See [Step 1 of the state stores guide](../guides/production/state-stores.md#step-1--pick-a-store-per-component).

<!-- snippet: examples/production-state-stores/main.go lines=107-129 -->
```go
// newApp builds the engine with both stores and returns the cart's
// backing store so the host (and the test) can look inside it.
func newApp(dir string) (*brick.Engine, *mapStore, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, nil, err
	}
	carts := newMapStore()
	eng, err := brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(maxSearches, searchTTL),
		StateStores: map[string]brick.StateStore{
			"cart": brick.Bytes(carts, brick.BytesOptions{TTL: cartTTL}),
		},
		Sessions: brick.SessionFunc(userOf),
	})
	return eng, carts, err
}
```

## If you are here, then

- **You are building a site and running `brick serve` or `brick build`:** set `BRICK_SECRET` (`brick keygen` prints one). The CLI seals state with it. Without it, state is sealed with a key made for the process, so a restart invalidates open pages (they recover in place), and `brick build` refuses a page with a stateful component.
- **You use the `site` package from Go:** set `site.Options.StateKeys` for sealed state. With neither `StateKeys` nor `Engine.States`, state stays in this process and the first stateful response logs a warning saying what that costs.
- **You use the engine alone, in your own `net/http` app:** you must set `Options.States`. Left nil, a page with a stateful component is an error. For one process, `brick.NewMemoryStates(0, 0)` is the shortest line; for production, sealed or a shared store.
- **You run more than one process behind a load balancer:** sealed state, SQL or Redis. Memory state answers the second server with `410` (the instance is unknown to it) and the runtime resets the component.
- **State holds a large list or a file's metadata that grows:** a server store (SQL or Redis), selected for that one component with `brick: {store: name}`. A sealed blob over 64 KiB fails the render and names the component.
- **You need to end a user's state on sign-out:** SQL and Redis implement `Revoker`; sealed state takes a `Revoked` callback. Either way, bind state to the visitor first with `Options.Sessions` ([Sessions and sign-out](../guides/production/sessions.md)).
- **You export a static site:** only sealed state works, because the exported HTML carries sealed handles that the action server must be able to open. `Export` returns `ErrExportNeedsKeys` without keys.

## Where the key comes from

Sealed state needs a key ring: the first key seals, every key opens, so rotation is putting the new key first. The ring itself is one interface, `brick.KeyRing`, so the source is your choice.

| Source | Use it when | Notes |
|---|---|---|
| `brick.KeysFromEnv(os.LookupEnv, "BRICK_SECRET")` | the secret already reaches the process as an environment variable | comma-separated base64 keys, newest first; fails at start when unset, on purpose |
| `brick.StaticKeys(keys...)` | a secret manager hands you the bytes | you feed it; nothing reads the environment behind your back |
| [`contrib/keys-awskms`](../../contrib/keys-awskms/) | the key must never sit in plain text in deploy config, and you are on AWS | a wrapped data key is unwrapped once at start; sealing stays local |
| [`contrib/keys-gcpkms`](../../contrib/keys-gcpkms/) | the same, on Google Cloud | fake-tested only, not against a real project |
| [`contrib/keys-vault`](../../contrib/keys-vault/) | the same, with HashiCorp Vault transit | unwrapped once at start |
| your own `KeyRing` | a secrets service or table | implement `Current` and `Lookup`, then pass `statetest.KeyRing` |

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

The same two calls take a KMS ring; the [sealed state guide](../guides/production/sealed-state.md#keys-from-a-kms) shows the AWS version running against an offline stand-in, and rotation without logging anyone out.

## Pitfalls

- **Memory state is per process.** Behind a load balancer, every second click can land on a server that never saw the instance. Expired memory instances also stay until something calls `MemoryStates.Prune`; the site layer does it on a timer, an engine-only host should too.
- **Do not generate a random key per process to "fix" the missing-secret error.** Every restart would reset every visitor's state and two servers could not open each other's blobs.
- **Removing the old key too early resets components.** Keep `new,old` for at least one TTL (the sealed default is 12 hours).
- **A store outage is not "not found".** A `BytesStore` must return an error when it cannot reach its backend. The engine then answers `503` with `Retry-After` and the runtime retries; answering "missing" would make it `410` and reset every open tab at once.
- **Encrypted is not read-only.** Sealing stops the browser reading or changing the blob, but a field that a binding writes is still writable through `$set`. Turn on `Options.LockState` and list `writable:` fields ([URL and server state](../guides/interactivity/url-and-server-state.md)).

## Guides and examples

| Option | Guide | Example |
|---|---|---|
| Memory and named stores | [State stores](../guides/production/state-stores.md) | [production-state-stores](../../examples/production-state-stores/) |
| Sealed state and key rings | [Sealed state](../guides/production/sealed-state.md) | [production-sealed-state](../../examples/production-sealed-state/) |
| SQL | [State stores: Redis and SQL](../guides/production/state-stores.md#redis-and-sql) | [contrib/states-sql](../../contrib/states-sql/) |
| Redis | [State stores: Redis and SQL](../guides/production/state-stores.md#redis-and-sql) | [contrib/states-redis](../../contrib/states-redis/) |
| Your own store | [State stores, step 2](../guides/production/state-stores.md#step-2--write-a-bytesstore) | [production-state-stores](../../examples/production-state-stores/), [statetest](../guides/tooling/statetest.md) |
| Sessions and sign-out | [Sessions](../guides/production/sessions.md) | [production-sealed-state](../../examples/production-sealed-state/) |

Back to [choosing guides](README.md).
