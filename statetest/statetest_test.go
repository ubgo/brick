package statetest_test

import (
	"bytes"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/statetest"
)

func TestMemoryStates(t *testing.T) {
	t.Parallel()
	statetest.Store(t, func(*testing.T) brick.StateStore { return brick.NewMemoryStates(0, 0) }, statetest.StoreOptions{})
}

func TestSealedStates(t *testing.T) {
	t.Parallel()
	statetest.Store(t, func(t *testing.T) brick.StateStore {
		s, err := brick.NewSealedStates(brick.SealedOptions{Sealer: brick.NewAESGCMSealer(ring(t), nil)})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}, statetest.StoreOptions{Stateless: true})
}

func TestBytesStates(t *testing.T) {
	t.Parallel()
	statetest.Store(t, func(*testing.T) brick.StateStore { return brick.Bytes(newMapBytes(), brick.BytesOptions{}) }, statetest.StoreOptions{})
}

func TestBytesStatesSealedAtRest(t *testing.T) {
	t.Parallel()
	statetest.Store(t, func(t *testing.T) brick.StateStore {
		return brick.Bytes(newMapBytes(), brick.BytesOptions{Sealer: brick.NewAESGCMSealer(ring(t), nil)})
	}, statetest.StoreOptions{})
}

func TestJSONCodec(t *testing.T) {
	t.Parallel()
	statetest.Codec(t, brick.JSONCodec{})
}

func TestAESGCMSealer(t *testing.T) {
	t.Parallel()
	statetest.Sealer(t, brick.NewAESGCMSealer(ring(t), nil))
}

func TestStaticKeys(t *testing.T) {
	t.Parallel()
	statetest.KeyRing(t, ring(t))
	env := map[string]string{"BRICK_SECRET": brick.EncodeStateKey(bytes.Repeat([]byte{7}, brick.StateKeySize))}
	r, err := brick.KeysFromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok }, "BRICK_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	statetest.KeyRing(t, r)
}

func ring(t *testing.T) brick.KeyRing {
	t.Helper()
	r, err := brick.StaticKeys(bytes.Repeat([]byte{7}, brick.StateKeySize))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
