# State stores: memory, named, Redis, SQL and your own

## What it is and when you want it

A state store keeps stateful components' instances between actions. brick ships three kinds and lets you add your own: `NewMemoryStates` keeps them in this process (fast, lost on restart, one server only), `NewSealedStates` puts them in the page ([Sealed state](sealed-state.md)), and `brick.Bytes` turns any place that keeps bytes under a key (a SQL table, Redis, your own map) into a store. One engine can hold several, by name, so a search box can stay in memory while a cart lives in a database for a day.

## Before you start

- A stateful component ([Stateful components](../interactivity/stateful-components.md)).
- For a server store shared by several processes, a database or Redis your app already connects to. brick never opens a connection itself: you pass the client.
- The example this page builds is [examples/production-state-stores](../../../examples/production-state-stores/).

## Step 1 — Pick a store per component

`Options.States` is the store named `default`. `Options.StateStores` adds more, and a component picks one in its frontmatter with `brick: {store: name}`; a component that names none uses `default`.

<!-- snippet: examples/production-state-stores/theme/components/Cart.html -->
```html
---
state:
  items: 0
brick:
  store: cart
---
<brick:script>
def put(state, props):
    return dict(state, items = state["items"] + 1)
</brick:script>
<div class="cart"><button brick:click="put">Add to cart</button> {{ .State.items }} items</div>
```

<!-- snippet: examples/production-state-stores/theme/components/Search.html -->
```html
---
state:
  q: ""
---
<div class="search"><input brick:model="q" value="{{ .State.q }}"> {{ with .State.q }}Results for {{ . }}{{ end }}</div>
```

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

`NewMemoryStates(max, ttl)` bounds the number of instances and how long an unused one lives (zero means 10,000 and 30 minutes). Every handle starts with its store's name (`default.…`, `cart.…`), so an action always goes back to the store that holds it.

## Step 2 — Write a `BytesStore`

A `BytesStore` has three methods: `Load`, `Save` and `Remove`. `brick.Bytes` wraps it and owns everything else: random 128-bit handles, the TTL, the codec, and optional encryption at rest (`BytesOptions.Sealer`). The example's store is a map, so it runs anywhere:

<!-- snippet: examples/production-state-stores/main.go lines=37-44 -->
```go
// mapStore is a BytesStore: bytes under a key, with a TTL and the hashed
// session each value belongs to. brick.Bytes adds the handles, the codec
// and the checks; the store only keeps bytes.
type mapStore struct {
	mu   sync.Mutex
	rows map[string]row
	now  func() time.Time
}
```

<!-- snippet: examples/production-state-stores/main.go lines=58-67 -->
```go
// Load returns a live value; an expired one is missing, never an error.
func (m *mapStore) Load(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[key]
	if !ok || !m.now().Before(r.expires) {
		return nil, false, nil
	}
	return r.data, true, nil
}
```

<!-- snippet: examples/production-state-stores/main.go lines=69-75 -->
```go
// Save stores a copy: the caller may reuse its slice.
func (m *mapStore) Save(_ context.Context, key string, data []byte, ttl time.Duration, session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[key] = row{data: append([]byte(nil), data...), expires: m.now().Add(ttl), session: session}
	return nil
}
```

<!-- snippet: examples/production-state-stores/main.go lines=77-84 -->
```go
// Remove deletes key; removing a missing key is not an error, so a retried
// cleanup stays idempotent.
func (m *mapStore) Remove(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, key)
	return nil
}
```

Optional upgrades are separate interfaces, so a store that lacks one never has to pretend: `brick.Revoker` (sign-out, see [Sessions](sessions.md#step-3--end-a-sessions-state-on-sign-out)), `brick.BytesLister` (walk every live value), and `brick.BytesEndWatcher` (report expiries to end hooks). `BytesOptions.Codec` (or `Options.Codec` for every store) replaces the default JSON encoding when your state holds values JSON cannot carry.

## Step 3 — Check it with the conformance suite

`statetest.Store` is the suite brick's own stores pass: a round trip, a missing handle, copies not shared, a second put, delete, revocation, listing and concurrent use. Run it on yours:

<!-- snippet: examples/production-state-stores/main_test.go lines=77-82 -->
```go
// The host's store passes the same conformance suite as brick's own.
func TestMapStoreConforms(t *testing.T) {
	statetest.Store(t, func(*testing.T) brick.StateStore {
		return brick.Bytes(newMapStore(), brick.BytesOptions{TTL: cartTTL})
	}, statetest.StoreOptions{})
}
```

[statetest](../tooling/statetest.md) covers the suite and its siblings for sealers, codecs and key rings.

## Redis and SQL

Two contrib modules are ready-made `BytesStore`s; each lives in its own Go module, so its driver never enters yours.

| Module | Backed by | Configure |
|---|---|---|
| [contrib/states-sql](../../../contrib/states-sql/) | any `*sql.DB` (Postgres, MySQL, SQLite) | `statessql.New(statessql.Options{DB: db, Dialect: statessql.Postgres()})`, then `st.Migrate(ctx)` once at start |
| [contrib/states-redis](../../../contrib/states-redis/) | a go-redis client | `statesredis.New(rdb, statesredis.Options{Prefix: "shop:"})` |

Both implement `Revoker`. Wiring Redis as the default store and a second prefix as the `cart` store, from the module's `Example`:

<!-- snippet: contrib/states-redis/example_test.go lines=37-45 -->
```go
	states := brick.Bytes(statesredis.New(rdb, statesredis.Options{Prefix: "shop:"}), brick.BytesOptions{TTL: stateTTL})
	carts := brick.Bytes(statesredis.New(rdb, statesredis.Options{Prefix: "shop:cart:"}), brick.BytesOptions{TTL: 24 * time.Hour})

	_, err := brick.New(brick.Options{
		FS:          cartTheme,
		States:      states,                                     // the store named "default"
		StateStores: map[string]brick.StateStore{"cart": carts}, // brick: {store: cart}
	})
	fmt.Println("engine built:", err == nil)
```

The SQL store takes a `*sql.DB` you opened with your own driver. `Migrate` creates the table and is safe to run on every start; the example uses SQLite in memory where production would use Postgres:

<!-- snippet: contrib/states-sql/example_test.go lines=43-56 -->
```go
	st, err := statessql.New(statessql.Options{DB: db, Dialect: statessql.SQLite()})
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := st.Migrate(ctx); err != nil { // idempotent
		fmt.Println(err)
		return
	}
	eng, err := brick.New(brick.Options{FS: counterTheme, States: brick.Bytes(st, brick.BytesOptions{TTL: stateTTL})})
	if err != nil {
		fmt.Println(err)
		return
	}
```

The modules' `Example` functions run without a server (SQL on in-memory SQLite, Redis as configuration only); `task test:sql` and `task test:redis` run both stores against real servers.

## Try it

From `examples/`:

```console
$ go test -v ./production-state-stores/
=== RUN   TestNamedStore
--- PASS: TestNamedStore (0.01s)
=== RUN   TestMapStoreConforms
=== RUN   TestMapStoreConforms/RoundTrip
=== RUN   TestMapStoreConforms/Missing
=== RUN   TestMapStoreConforms/CopiesNotShares
=== RUN   TestMapStoreConforms/PutAgain
=== RUN   TestMapStoreConforms/Delete
=== RUN   TestMapStoreConforms/RevokeSession
=== RUN   TestMapStoreConforms/Each
    store.go:157: Lister that reports ErrUnsupported (an adapter over a store without it)
=== RUN   TestMapStoreConforms/Concurrent
--- PASS: TestMapStoreConforms (0.00s)
    --- PASS: TestMapStoreConforms/RoundTrip (0.00s)
    --- PASS: TestMapStoreConforms/Missing (0.00s)
    --- PASS: TestMapStoreConforms/CopiesNotShares (0.00s)
    --- PASS: TestMapStoreConforms/PutAgain (0.00s)
    --- PASS: TestMapStoreConforms/Delete (0.00s)
    --- PASS: TestMapStoreConforms/RevokeSession (0.00s)
    --- SKIP: TestMapStoreConforms/Each (0.00s)
    --- PASS: TestMapStoreConforms/Concurrent (0.00s)
=== RUN   TestUnknownStore
    main_test.go:102: unknown store: components/Wish.html:1: brick: store: "carts" is not a state store of this engine (have: cart, default)
--- PASS: TestUnknownStore (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/production-state-stores	0.031s
```

`Each` is skipped because the map store does not implement `BytesLister`; add `EachBytes` and it runs.

## Pitfalls

- **A misspelled store name fails to compile.** `brick: {store: carts}` with only `cart` registered gives `brick: store: "carts" is not a state store of this engine (have: cart, default)`. `default` is reserved for `Options.States`: putting it in `StateStores` fails `New` with `brick: Options.StateStores: "default" is Options.States' name; pick another`.
- **Memory state does not survive a restart or a second server.** `NewMemoryStates` is per process. Behind a load balancer, use sealed state or a shared store, or every second click lands on a server that has never seen the instance and answers `410`.
- **An outage is not "not found".** A store must return an error when it cannot reach its backend, never `ok=false`: the engine then answers `503` with `Retry-After` and the runtime retries, instead of `410`, which would make every open tab reset its components at once. The Redis module's `ExampleNew_outage` pins this.
- **Expired memory instances stay until something prunes them.** `MemoryStates.Prune` removes expired instances and fires their end hooks; the site layer calls it on a timer, an engine-only host should too, or ends fire only when the size bound pushes instances out.
- **`Save` must copy.** The slice passed to `Save` may be reused by the caller; `statetest`'s `CopiesNotShares` case fails a store that keeps it.

## Related

- [Sealed state and key rings](sealed-state.md) · [Sessions and sign-out](sessions.md) · [statetest](../tooling/statetest.md)
- [Feature index: State storage and security](../../reference/features.md#state-storage-and-security)
- Example: [examples/production-state-stores](../../../examples/production-state-stores/) · contrib: [states-sql](../../../contrib/states-sql/), [states-redis](../../../contrib/states-redis/)
