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
