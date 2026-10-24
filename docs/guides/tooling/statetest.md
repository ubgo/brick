# statetest: prove a custom state store

## What it is and when you want it

`statetest` is the conformance suite for brick's state pieces: a store, a codec, a sealer and a key ring. brick's own pieces and the Redis and SQL modules run it, so every piece agrees on what a round trip, a missing handle, a delete, a revocation and a tampered blob mean. If you write your own store, run the same suite against it; it takes one test function and catches the mistakes that only show up in production, such as keeping the caller's map or reporting a lost connection as "not found".

## Before you start

- A custom `brick.StateStore` (or `Codec`, `Sealer`, `KeyRing`). If all you have is a place that keeps bytes under a key, you do not need to write a `StateStore`; wrap it with `brick.Bytes` ([State stores](../production/state-stores.md)).
- `import "github.com/ubgo/brick/statetest"`. Part of the root module.
- The example this page builds is [examples/tooling-statetest](../../../examples/tooling-statetest/).

## Step 1 — Write the store

A `StateStore` has three methods: `Put` returns the handle the page carries, `Get` returns a copy or `ok=false`, `Delete` forgets. The optional `Revoker` (end every instance of a session) and `Lister` (walk live instances) upgrades are found by type assertion, so a store that lacks them is never forced to lie about it. This store keeps each instance as codec bytes, which is also what makes it copy on the way in and out.

<!-- snippet: examples/tooling-statetest/main.go lines=22-84 -->
```go
// that a handle cannot be guessed (the action token is a second check).
const handleBytes = 16

// memStore keeps each instance as codec bytes under a random handle. Storing
// bytes, not the *brick.Instance, is what makes it copy on Put and on Get:
// the suite's CopiesNotShares case fails a store that keeps the caller's map.
type memStore struct {
	mu    sync.Mutex
	codec brick.Codec
	rows  map[string][]byte
}

// newMemStore returns an empty store that keeps state as JSON bytes, the same
// shape a real database or Redis store persists.
func newMemStore() *memStore {
	return &memStore{codec: brick.JSONCodec{}, rows: map[string][]byte{}}
}

// Put keeps the handle when the instance already has one (a server store
// keeps its handle on a later Put) and mints one otherwise.
func (m *memStore) Put(_ context.Context, inst *brick.Instance) (string, error) {
	b, err := m.codec.Encode(inst)
	if err != nil {
		return "", err
	}
	h := inst.Handle
	if h == "" {
		raw := make([]byte, handleBytes)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		h = hex.EncodeToString(raw)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[h] = b
	return h, nil
}

// Get reports a missing handle as ok=false with no error: an error means the
// store itself failed, and the action would answer 503 instead of 410.
func (m *memStore) Get(_ context.Context, handle string) (*brick.Instance, bool, error) {
	m.mu.Lock()
	b, ok := m.rows[handle]
	m.mu.Unlock()
	if !ok {
		return nil, false, nil
	}
	inst, err := m.codec.Decode(b)
	if err != nil {
		return nil, false, err
	}
	inst.Handle = handle
	return inst, true, nil
}

// Delete of a missing handle is not an error.
func (m *memStore) Delete(_ context.Context, handle string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, handle)
	return nil
}
```

<!-- snippet: examples/tooling-statetest/main.go lines=86-122 -->
```go
// RevokeSession is the optional Revoker upgrade: it ends every instance of
// one (hashed) session, so sign-out forgets that user's components only.
func (m *memStore) RevokeSession(_ context.Context, session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, b := range m.rows {
		inst, err := m.codec.Decode(b)
		if err != nil {
			return err
		}
		if inst.Session == session {
			delete(m.rows, h)
		}
	}
	return nil
}

// Each is the optional Lister upgrade, for an inspector: fn returning false
// stops the walk. The lock is not held while fn runs.
func (m *memStore) Each(_ context.Context, fn func(*brick.Instance) bool) error {
	m.mu.Lock()
	snap := make([][]byte, 0, len(m.rows))
	for _, b := range m.rows {
		snap = append(snap, b)
	}
	m.mu.Unlock()
	for _, b := range snap {
		inst, err := m.codec.Decode(b)
		if err != nil {
			return err
		}
		if !fn(inst) {
			return nil
		}
	}
	return nil
}
```

## Step 2 — Run the suite

`statetest.Store(t, newStore, opts)` runs the cases as subtests; `newStore` returns a fresh, empty store for each. Set `StoreOptions{Stateless: true}` only for a store that keeps nothing server-side (a sealing store), whose handle changes on every `Put` and whose `Delete` forgets nothing. The same package has `Codec(t, c)`, `Sealer(t, s)` and `KeyRing(t, r)` for the other pieces.

<!-- snippet: examples/tooling-statetest/main_test.go -->
```go
package main

import (
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
	"github.com/ubgo/brick/statetest"
)

// The whole conformance suite against the custom store. Because memStore
// also implements Revoker and Lister, the suite's RevokeSession and Each
// cases run instead of skipping.
func TestMemStoreConforms(t *testing.T) {
	statetest.Store(t, func(*testing.T) brick.StateStore { return newMemStore() }, statetest.StoreOptions{})
}

// A custom codec is proved the same way; this one is brick's own, shown so
// the call shape is on record.
func TestCodecConforms(t *testing.T) {
	statetest.Codec(t, brick.JSONCodec{})
}

// The store also works wired into an engine under a name.
func TestWiresIntoEngine(t *testing.T) {
	_, err := brick.New(brick.Options{
		FS:          bricktest.Theme("content/index.html", "<h1>Hi</h1>"),
		StateStores: map[string]brick.StateStore{"mem": newMemStore()},
	})
	if err != nil {
		t.Fatal(err)
	}
}
```

## Try it

From `examples/`:

```console
$ go test -v ./tooling-statetest/
=== RUN   TestMemStoreConforms
=== RUN   TestMemStoreConforms/RoundTrip
=== RUN   TestMemStoreConforms/Missing
=== RUN   TestMemStoreConforms/CopiesNotShares
=== RUN   TestMemStoreConforms/PutAgain
=== RUN   TestMemStoreConforms/Delete
=== RUN   TestMemStoreConforms/RevokeSession
=== RUN   TestMemStoreConforms/Each
=== RUN   TestMemStoreConforms/Concurrent
--- PASS: TestMemStoreConforms (0.00s)
=== RUN   TestCodecConforms
--- PASS: TestCodecConforms (0.00s)
=== RUN   TestWiresIntoEngine
--- PASS: TestWiresIntoEngine (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/tooling-statetest	0.020s
```

What each store case demands:

| Case | A store passes only if |
|---|---|
| `RoundTrip` | `Get` returns an instance equal to what was put, with `Handle` set to the handle asked for |
| `Missing` | an empty, unknown or garbled handle is `ok=false` with no error |
| `CopiesNotShares` | changing the caller's map after `Put`, or the returned map after `Get`, never changes what is stored |
| `PutAgain` | a server store keeps the handle on a later `Put` and shows the new state |
| `Delete` | the instance is gone; deleting a missing handle is not an error |
| `RevokeSession` | one session's instances go, another's stay (skipped if the store is not a `Revoker`) |
| `Each` | all instances are seen once, and `fn` returning false stops the walk (skipped if not a `Lister`) |
| `Concurrent` | 64 parallel `Put`/`Get` pairs each read back their own state (run with `-race`) |

## Pitfalls

- **Skipped is not passed.** `RevokeSession` and `Each` skip when the store lacks the upgrade, and also when an adapter reports `errors.ErrUnsupported`. A store you meant to support revocation with that shows `SKIP` has not been tested for it.
- **A store that shares the caller's data fails loudly.** A store that keeps the `*Instance` it was given and returns it from `Get` (and forgets to set the handle) fails like this:

  ```console
  --- FAIL: TestLeaky (0.00s)
      --- FAIL: TestLeaky/RoundTrip (0.00s)
          store.go:55: Get sets Handle to the handle asked for: "", want "h"
      --- FAIL: TestLeaky/CopiesNotShares (0.00s)
          store.go:80: a store must keep a copy: changing the caller's map changed the stored state
      --- FAIL: TestLeaky/Concurrent (0.00s)
          store.go:195: goroutine 49: true <nil> &{ 4a7d00000000000000000000000000ff default ...
  ```

- **A lost connection is an error, not "not found".** `Missing` pins that an unknown handle is `ok=false`; the other half (a backend outage is an error, so the action answers `503` and the runtime retries instead of `410`) is not in the suite, because it needs a way to break your backend. Test it yourself, as the Redis module's `ExampleNew_outage` does.
- **Run it with `-race`.** The `Concurrent` case only proves thread safety when the race detector is on.
- **It checks the contract, not your backend.** TTL expiry, size bounds and pruning are specific to a store; test those beside the suite.

## Related

- [State stores](../production/state-stores.md) · [Sealed state and key rings](../production/sealed-state.md) · [Sessions and sign-out](../production/sessions.md)
- [bricktest](bricktest.md) for testing the components that use the store
- [Feature index: State storage and security](../../reference/features.md#state-storage-and-security)
- Example: [examples/tooling-statetest](../../../examples/tooling-statetest/)
