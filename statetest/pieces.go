package statetest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"

	"github.com/ubgo/brick"
)

// Codec runs the codec suite: Sample round-trips unchanged, and garbage is
// an error, never a half-filled instance.
func Codec(t *testing.T, c brick.Codec) {
	t.Helper()
	in := Sample("abc")
	b, err := c.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := c.Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("round trip changed the instance:\n got %#v\nwant %#v", out, in)
	}
	again, err := c.Encode(out)
	if err != nil {
		t.Fatalf("Encode of the decoded instance: %v", err)
	}
	if !bytes.Equal(b, again) {
		t.Error("encoding is not stable: equal instances gave different bytes")
	}
	if inst, err := c.Decode([]byte("\x00not an instance")); err == nil {
		t.Errorf("garbage decoded to %#v", inst)
	}
}

// Sealer runs the sealer suite: a round trip, every changed byte and every
// other additional data refused with brick.ErrSealOpen, and two seals of
// the same input differing (a fresh salt or nonce each time).
func Sealer(t *testing.T, s brick.Sealer) {
	t.Helper()
	ctx := context.Background()
	plain, ad := []byte("the state"), []byte("ad-1")
	sealed, err := s.Seal(ctx, plain, ad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(sealed, plain) {
		t.Error("sealed bytes contain the plaintext")
	}
	got, err := s.Open(ctx, sealed, ad)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("Open: %q %v", got, err)
	}
	if _, err := s.Open(ctx, sealed, []byte("ad-2")); !errors.Is(err, brick.ErrSealOpen) {
		t.Errorf("other additional data: %v, want ErrSealOpen", err)
	}
	for i := range sealed {
		bad := bytes.Clone(sealed)
		bad[i] ^= 1
		if _, err := s.Open(ctx, bad, ad); !errors.Is(err, brick.ErrSealOpen) {
			t.Fatalf("byte %d changed: %v, want ErrSealOpen", i, err)
		}
	}
	if _, err := s.Open(ctx, nil, ad); !errors.Is(err, brick.ErrSealOpen) {
		t.Errorf("empty input: %v", err)
	}
	// A failed second Seal returns nothing, which never equals sealed, so
	// the nonce check below would pass without the error check.
	again, err := s.Seal(ctx, plain, ad)
	if err != nil {
		t.Fatalf("second Seal: %v", err)
	}
	if bytes.Equal(again, sealed) {
		t.Error("two seals of the same input are identical: the sealer reuses its nonce")
	}
}

// KeyRing runs the key ring suite: Current is a valid key that Lookup
// finds by its id, and an unknown id is not found.
func KeyRing(t *testing.T, r brick.KeyRing) {
	t.Helper()
	ctx := context.Background()
	k, err := r.Current(ctx)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(k.Bytes) != brick.StateKeySize {
		t.Fatalf("Current key is %d bytes, want %d", len(k.Bytes), brick.StateKeySize)
	}
	sum := sha256.Sum256(k.Bytes)
	if !bytes.Equal(k.ID[:], sum[:brick.KeyIDSize]) {
		t.Error("a key's ID is the first bytes of SHA-256 of the key")
	}
	got, ok, err := r.Lookup(ctx, k.ID)
	if err != nil || !ok || !bytes.Equal(got.Bytes, k.Bytes) {
		t.Fatalf("Lookup(Current.ID): %v %v", ok, err)
	}
	if _, ok, err := r.Lookup(ctx, [brick.KeyIDSize]byte{0xde, 0xad, 0xbe, 0xef}); ok || err != nil {
		t.Errorf("unknown id: %v %v", ok, err)
	}
}
