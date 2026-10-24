package brick

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// StateKeySize is the size of a state key: AES-256.
const StateKeySize = 32

// KeyIDSize is the size of a key id: the first bytes of SHA-256(key).
const KeyIDSize = 4

// Key is one sealing key. ID picks the key when a blob
// is opened without trying each one, and reveals nothing usable about it.
type Key struct {
	ID    [KeyIDSize]byte
	Bytes []byte
}

// KeyRing is where sealing keys come from: a static list, the environment,
// a KMS, Vault, the host's own table. Rotation is the order of the ring:
// Current seals, Lookup opens any key still in it.
type KeyRing interface {
	// Current is the key new blobs are sealed with.
	Current(ctx context.Context) (Key, error)
	// Lookup finds a key by id to open a blob; ok=false for a retired key.
	Lookup(ctx context.Context, id [KeyIDSize]byte) (Key, bool, error)
}

// NewKey wraps 32 key bytes with their id.
func NewKey(b []byte) (Key, error) {
	if len(b) != StateKeySize {
		return Key{}, fmt.Errorf("brick: a state key is %d bytes, got %d (brick keygen makes one)", StateKeySize, len(b))
	}
	sum := sha256.Sum256(b)
	var k Key
	copy(k.ID[:], sum[:KeyIDSize])
	k.Bytes = bytes.Clone(b)
	return k, nil
}

// staticRing is an immutable KeyRing, safe for concurrent use.
type staticRing struct{ keys []Key }

// StaticKeys is a KeyRing of fixed keys, newest first: the first seals,
// every one opens. Rotate by adding the new key second, deploying,
// promoting it to first in a later deploy, and removing the old one a TTL
// after that.
func StaticKeys(keys ...[]byte) (KeyRing, error) {
	if len(keys) == 0 {
		return nil, errors.New("brick: StaticKeys needs at least one key; generate one with brick.NewStateKey (brick keygen)")
	}
	r := &staticRing{}
	seen := map[[KeyIDSize]byte]bool{}
	for i, b := range keys {
		k, err := NewKey(b)
		if err != nil {
			return nil, fmt.Errorf("brick: key %d: %w", i+1, err)
		}
		if seen[k.ID] {
			return nil, fmt.Errorf("brick: key %d repeats an earlier key", i+1)
		}
		seen[k.ID] = true
		r.keys = append(r.keys, k)
	}
	return r, nil
}

func (r *staticRing) Current(context.Context) (Key, error) { return r.keys[0], nil }

func (r *staticRing) Lookup(_ context.Context, id [KeyIDSize]byte) (Key, bool, error) {
	for _, k := range r.keys {
		if k.ID == id {
			return k, true, nil
		}
	}
	return Key{}, false, nil
}

// KeysFromEnv reads a StaticKeys ring from one variable through lookup
// (os.LookupEnv, or a test's map): comma-separated base64 keys, newest
// first — the BRICK_SECRET format. lookup is a parameter, never os.Getenv
// directly, so the caller decides where keys come from.
func KeysFromEnv(lookup func(string) (string, bool), name string) (KeyRing, error) {
	v, ok := lookup(name)
	if !ok || strings.TrimSpace(v) == "" {
		return nil, fmt.Errorf("brick: %s is not set; generate a key with brick keygen", name)
	}
	keys, err := ParseStateKeys(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return StaticKeys(keys...)
}

// NewStateKey returns a fresh random 32-byte state key (brick keygen).
func NewStateKey() ([]byte, error) {
	k := make([]byte, StateKeySize)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	return k, nil
}

// EncodeStateKey writes a key the way ParseStateKeys reads it: standard
// base64.
func EncodeStateKey(k []byte) string { return base64.StdEncoding.EncodeToString(k) }

// ParseStateKeys reads comma-separated base64 keys, newest first. Each
// must decode to 32 bytes.
func ParseStateKeys(s string) ([][]byte, error) {
	var out [][]byte
	for i, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, err := base64.StdEncoding.DecodeString(part)
		if err != nil {
			k, err = base64.RawURLEncoding.DecodeString(part)
		}
		if err != nil {
			return nil, fmt.Errorf("brick: state key %d is not base64", i+1)
		}
		if len(k) != StateKeySize {
			return nil, fmt.Errorf("brick: state key %d is %d bytes; a state key is %d (brick keygen makes one)", i+1, len(k), StateKeySize)
		}
		out = append(out, k)
	}
	if len(out) == 0 {
		return nil, errors.New("brick: no state keys given")
	}
	return out, nil
}
