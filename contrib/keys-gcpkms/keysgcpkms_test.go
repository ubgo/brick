package keysgcpkms

import (
	"bytes"

	kms "cloud.google.com/go/kms/apiv1"
	"context"
	"errors"
	"strings"
	"testing"

	"cloud.google.com/go/kms/apiv1/kmspb"
	"github.com/googleapis/gax-go/v2"
	"github.com/ubgo/brick"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const testKey = "projects/p/locations/global/keyRings/r/cryptoKeys/brick"

// fakeKMS "encrypts" as keyName|aad|plaintext XOR a mask, checks the
// request checksums and returns response checksums the way Cloud KMS does,
// and refuses another key or AAD. corrupt flips the plaintext checksum, to
// stand for a response damaged in transit. It keeps every plaintext it
// hands out so a test can see they were zeroed.
type fakeKMS struct {
	err      error
	corrupt  bool
	size     int
	returned [][]byte
}

func (f *fakeKMS) wrap(name string, aad, plain []byte) []byte {
	out := []byte(name + "|" + string(aad) + "|")
	for _, b := range plain {
		out = append(out, b^0x5a)
	}
	return out
}

func (f *fakeKMS) Decrypt(_ context.Context, req *kmspb.DecryptRequest, _ ...gax.CallOption) (*kmspb.DecryptResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	if req.GetCiphertextCrc32C().GetValue() != crc(req.GetCiphertext()).GetValue() {
		return nil, errors.New("rpc error: code = InvalidArgument desc = ciphertext checksum mismatch")
	}
	if req.GetAdditionalAuthenticatedData() != nil && req.GetAdditionalAuthenticatedDataCrc32C().GetValue() != crc(req.GetAdditionalAuthenticatedData()).GetValue() {
		return nil, errors.New("rpc error: code = InvalidArgument desc = aad checksum mismatch")
	}
	prefix := []byte(req.GetName() + "|" + string(req.GetAdditionalAuthenticatedData()) + "|")
	if !bytes.HasPrefix(req.GetCiphertext(), prefix) {
		return nil, errors.New("rpc error: code = InvalidArgument desc = Decryption failed")
	}
	var plain []byte
	for _, b := range req.GetCiphertext()[len(prefix):] {
		plain = append(plain, b^0x5a)
	}
	if f.size > 0 {
		plain = plain[:f.size]
	}
	f.returned = append(f.returned, plain)
	sum := crc(plain)
	if f.corrupt {
		sum = wrapperspb.Int64(sum.GetValue() + 1)
	}
	return &kmspb.DecryptResponse{Plaintext: plain, PlaintextCrc32C: sum}, nil
}

func (f *fakeKMS) Encrypt(_ context.Context, req *kmspb.EncryptRequest, _ ...gax.CallOption) (*kmspb.EncryptResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.returned = append(f.returned, req.GetPlaintext())
	ct := f.wrap(req.GetName(), req.GetAdditionalAuthenticatedData(), req.GetPlaintext())
	ctSum := crc(ct)
	if f.corrupt {
		ctSum = wrapperspb.Int64(ctSum.GetValue() + 1)
	}
	return &kmspb.EncryptResponse{
		Name:                    req.GetName() + "/cryptoKeyVersions/1",
		Ciphertext:              ct,
		CiphertextCrc32C:        ctSum,
		VerifiedPlaintextCrc32C: req.GetPlaintextCrc32C().GetValue() == crc(req.GetPlaintext()).GetValue(),
		VerifiedAdditionalAuthenticatedDataCrc32C: req.GetAdditionalAuthenticatedData() != nil &&
			req.GetAdditionalAuthenticatedDataCrc32C().GetValue() == crc(req.GetAdditionalAuthenticatedData()).GetValue(),
	}, nil
}

func mustKey(t *testing.T) []byte {
	t.Helper()
	k, err := brick.NewStateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestRingSealsWithFirstOpensWithAll(t *testing.T) {
	ctx := context.Background()
	f := &fakeKMS{}
	aad := []byte("app=shop")
	oldW, err := NewWrappedKey(ctx, f, testKey, aad)
	if err != nil {
		t.Fatal(err)
	}
	newW, err := NewWrappedKey(ctx, f, testKey, aad)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range f.returned {
		if !bytes.Equal(p, make([]byte, len(p))) {
			t.Fatalf("NewWrappedKey left plaintext %d in memory", i+1)
		}
	}
	oldRing, err := New(ctx, f, Options{KeyName: testKey, AAD: aad, Wrapped: [][]byte{oldW}})
	if err != nil {
		t.Fatal(err)
	}
	sealedOld, err := brick.NewAESGCMSealer(oldRing, nil).Seal(ctx, []byte("old state"), nil)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := ParseWrapped(EncodeWrapped(newW, oldW))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := New(ctx, f, Options{KeyName: testKey, AAD: aad, Wrapped: wrapped})
	if err != nil {
		t.Fatal(err)
	}
	s := brick.NewAESGCMSealer(ring, nil)
	got, err := s.Open(ctx, sealedOld, nil)
	if err != nil || string(got) != "old state" {
		t.Fatalf("a blob sealed under the older key does not open: %q %v", got, err)
	}
	sealed, err := s.Seal(ctx, []byte("new"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := brick.NewAESGCMSealer(oldRing, nil).Open(ctx, sealed, nil); err == nil {
		t.Fatal("the ring is not sealing with the first key")
	}
}

// A server must never start without its key: every failure on the way to
// a ring is an error, and the error never carries key material.
func TestRefusesToStartWithoutAKey(t *testing.T) {
	ctx := context.Background()
	k := mustKey(t)
	good := (&fakeKMS{}).wrap(testKey, nil, k)
	cases := map[string]struct {
		client DecryptAPI
		opts   Options
	}{
		"no client":         {nil, Options{KeyName: testKey, Wrapped: [][]byte{good}}},
		"no key name":       {&fakeKMS{}, Options{Wrapped: [][]byte{good}}},
		"no wrapped keys":   {&fakeKMS{}, Options{KeyName: testKey}},
		"empty wrapped key": {&fakeKMS{}, Options{KeyName: testKey, Wrapped: [][]byte{{}}}},
		"kms down":          {&fakeKMS{err: errors.New("rpc error: code = Unavailable")}, Options{KeyName: testKey, Wrapped: [][]byte{good}}},
		"other crypto key":  {&fakeKMS{}, Options{KeyName: testKey + "2", Wrapped: [][]byte{good}}},
		"wrong aad":         {&fakeKMS{}, Options{KeyName: testKey, AAD: []byte("app=blog"), Wrapped: [][]byte{good}}},
		"corrupt response":  {&fakeKMS{corrupt: true}, Options{KeyName: testKey, Wrapped: [][]byte{good}}},
		"short data key":    {&fakeKMS{size: 16}, Options{KeyName: testKey, Wrapped: [][]byte{good}}},
		"second failing":    {&fakeKMS{}, Options{KeyName: testKey, Wrapped: [][]byte{good, []byte("garbage")}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ring, err := New(ctx, tc.client, tc.opts)
			if err == nil || ring != nil {
				t.Fatalf("New started without a usable key: ring=%v err=%v", ring, err)
			}
			if msg := err.Error(); strings.Contains(msg, string(k)) || strings.Contains(msg, EncodeWrapped(k)) {
				t.Fatalf("error leaks the key: %s", msg)
			}
		})
	}
}

func TestPlaintextsAreZeroed(t *testing.T) {
	ctx := context.Background()
	for _, fail := range []bool{false, true} {
		f := &fakeKMS{}
		wrapped := [][]byte{f.wrap(testKey, nil, mustKey(t))}
		if fail {
			wrapped = append(wrapped, f.wrap(testKey, nil, []byte("short")))
		}
		ring, err := New(ctx, f, Options{KeyName: testKey, Wrapped: wrapped})
		if (err != nil) != fail {
			t.Fatalf("fail=%v: err=%v", fail, err)
		}
		for i, p := range f.returned {
			if !bytes.Equal(p, make([]byte, len(p))) {
				t.Fatalf("fail=%v: plaintext %d was not zeroed", fail, i+1)
			}
		}
		if ring != nil {
			cur, _ := ring.Current(ctx)
			if bytes.Equal(cur.Bytes, make([]byte, len(cur.Bytes))) {
				t.Fatal("zeroing reached the ring's own copy")
			}
		}
	}
}

func TestNewWrappedKeyRefusesUnverified(t *testing.T) {
	ctx := context.Background()
	if _, err := NewWrappedKey(ctx, &fakeKMS{corrupt: true}, testKey, nil); err == nil {
		t.Fatal("a ciphertext with a bad checksum was accepted")
	}
	if _, err := NewWrappedKey(ctx, &fakeKMS{err: errors.New("PermissionDenied")}, testKey, nil); err == nil {
		t.Fatal("a Cloud KMS error made a key")
	}
	if _, err := NewWrappedKey(ctx, nil, testKey, nil); err == nil {
		t.Fatal("no client made a key")
	}
	for _, s := range []string{"", " , ", "not base64!"} {
		if _, err := ParseWrapped(s); err == nil {
			t.Errorf("ParseWrapped(%q) accepted", s)
		}
	}
}

// *kms.KeyManagementClient must satisfy both interfaces, or the README's
// one-line wiring would not compile.
var _ interface {
	DecryptAPI
	EncryptAPI
} = (*kms.KeyManagementClient)(nil)
