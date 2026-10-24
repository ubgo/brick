// Command tooling-statetest is a custom state store (a whole StateStore,
// not a BytesStore) plus the tests that prove it with statetest, the
// conformance suite every brick store passes. The program wires the store
// into an engine and prints one round trip; the value is in main_test.go.
//
//	go run ./tooling-statetest
//	go test ./tooling-statetest
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sync"

	"github.com/ubgo/brick"
)

// handleBytes is the random part of a handle: 16 bytes is 128 bits, enough
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

// main stores an instance, reads it back by handle and prints it, the round trip
// statetest's checks assert on.
func main() {
	ctx := context.Background()
	s := newMemStore()
	h, err := s.Put(ctx, &brick.Instance{Name: "Cart", State: map[string]any{"items": int64(2)}})
	if err != nil {
		log.Fatal(err)
	}
	inst, ok, err := s.Get(ctx, h)
	if err != nil || !ok {
		log.Fatalf("get: %v %v", ok, err)
	}
	fmt.Printf("handle %s…: %s has %v items\n", h[:8], inst.Name, inst.State["items"])
}
