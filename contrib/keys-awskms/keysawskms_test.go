package keysawskms

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/ubgo/brick"
)

const testKeyID = "alias/brick-test"

// fakeKMS "encrypts" by prefixing the key id and XOR-ing with a mask, and
// refuses a decrypt under another key id or encryption context, the two
// checks real KMS makes that New relies on. It keeps every plaintext it
// hands out so a test can see they were zeroed.
type fakeKMS struct {
	ctx      map[string]string
	err      error
	size     int
	returned [][]byte
}

func (f *fakeKMS) wrap(keyID string, plain []byte) []byte {
	out := []byte(keyID + "|")
	for _, b := range plain {
		out = append(out, b^0x5a)
	}
	return out
}

func (f *fakeKMS) Decrypt(_ context.Context, in *kms.DecryptInput, _ ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	id := aws.ToString(in.KeyId)
	prefix := []byte(id + "|")
	if !bytes.HasPrefix(in.CiphertextBlob, prefix) {
		return nil, errors.New("IncorrectKeyException")
	}
	if !maps.Equal(in.EncryptionContext, f.ctx) {
		return nil, errors.New("InvalidCiphertextException")
	}
	var plain []byte
	for _, b := range in.CiphertextBlob[len(prefix):] {
		plain = append(plain, b^0x5a)
	}
	if f.size > 0 {
		plain = plain[:f.size]
	}
	f.returned = append(f.returned, plain)
	return &kms.DecryptOutput{Plaintext: plain}, nil
}

func (f *fakeKMS) GenerateDataKey(_ context.Context, in *kms.GenerateDataKeyInput, _ ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error) {
	plain, err := brick.NewStateKey()
	if err != nil {
		return nil, err
	}
	f.returned = append(f.returned, plain)
	return &kms.GenerateDataKeyOutput{Plaintext: plain, CiphertextBlob: f.wrap(aws.ToString(in.KeyId), plain)}, nil
}

func mustKey(t *testing.T) []byte {
	t.Helper()
	k, err := brick.NewStateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// The ring seals with the first key and opens with every one, exactly
// like StaticKeys over the plaintexts: rotation works through KMS.
func TestRingSealsWithFirstOpensWithAll(t *testing.T) {
	ctx := context.Background()
	f := &fakeKMS{}
	newK, oldK := mustKey(t), mustKey(t)
	oldRing, err := New(ctx, f, Options{KeyID: testKeyID, Wrapped: [][]byte{f.wrap(testKeyID, oldK)}})
	if err != nil {
		t.Fatal(err)
	}
	sealedOld, err := brick.NewAESGCMSealer(oldRing, nil).Seal(ctx, []byte("old state"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := New(ctx, f, Options{KeyID: testKeyID, Wrapped: [][]byte{f.wrap(testKeyID, newK), f.wrap(testKeyID, oldK)}})
	if err != nil {
		t.Fatal(err)
	}
	cur, err := ring.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := brick.NewKey(newK)
	if cur.ID != want.ID || !bytes.Equal(cur.Bytes, newK) {
		t.Fatal("Current is not the first (newest) data key")
	}
	s := brick.NewAESGCMSealer(ring, nil)
	got, err := s.Open(ctx, sealedOld, nil)
	if err != nil || string(got) != "old state" {
		t.Fatalf("a blob sealed under the older key does not open: %q %v", got, err)
	}
}

// A server must never start without its key: every failure on the way to
// a ring is an error, and the error never carries key material.
func TestRefusesToStartWithoutAKey(t *testing.T) {
	ctx := context.Background()
	k := mustKey(t)
	cases := map[string]struct {
		client DecryptAPI
		opts   Options
	}{
		"no client":          {nil, Options{KeyID: testKeyID, Wrapped: [][]byte{[]byte("x")}}},
		"no key id":          {&fakeKMS{}, Options{Wrapped: [][]byte{(&fakeKMS{}).wrap(testKeyID, k)}}},
		"no wrapped keys":    {&fakeKMS{}, Options{KeyID: testKeyID}},
		"empty wrapped key":  {&fakeKMS{}, Options{KeyID: testKeyID, Wrapped: [][]byte{{}}}},
		"kms down":           {&fakeKMS{err: errors.New("dial tcp: connection refused")}, Options{KeyID: testKeyID, Wrapped: [][]byte{(&fakeKMS{}).wrap(testKeyID, k)}}},
		"other kms key":      {&fakeKMS{}, Options{KeyID: testKeyID, Wrapped: [][]byte{(&fakeKMS{}).wrap("alias/other", k)}}},
		"wrong context":      {&fakeKMS{ctx: map[string]string{"app": "shop"}}, Options{KeyID: testKeyID, Wrapped: [][]byte{(&fakeKMS{}).wrap(testKeyID, k)}}},
		"short data key":     {&fakeKMS{size: 16}, Options{KeyID: testKeyID, Wrapped: [][]byte{(&fakeKMS{}).wrap(testKeyID, k)}}},
		"second key failing": {&fakeKMS{}, Options{KeyID: testKeyID, Wrapped: [][]byte{(&fakeKMS{}).wrap(testKeyID, k), []byte("garbage")}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ring, err := New(ctx, tc.client, tc.opts)
			if err == nil || ring != nil {
				t.Fatalf("New started without a usable key: ring=%v err=%v", ring, err)
			}
			msg := err.Error()
			if strings.Contains(msg, string(k)) || strings.Contains(msg, EncodeWrapped(k)) {
				t.Fatalf("error leaks the key: %s", msg)
			}
		})
	}
}

// The KMS plaintexts are zeroed once the ring has its own copies, on the
// success path and on a failure part way through.
func TestPlaintextsAreZeroed(t *testing.T) {
	ctx := context.Background()
	for _, fail := range []bool{false, true} {
		f := &fakeKMS{}
		wrapped := [][]byte{f.wrap(testKeyID, mustKey(t))}
		if fail {
			wrapped = append(wrapped, f.wrap(testKeyID, []byte("short")))
		}
		ring, err := New(ctx, f, Options{KeyID: testKeyID, Wrapped: wrapped})
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

func TestNewWrappedKeyRoundTrip(t *testing.T) {
	ctx := context.Background()
	f := &fakeKMS{}
	w, err := NewWrappedKey(ctx, f, testKeyID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.returned[0], make([]byte, brick.StateKeySize)) {
		t.Fatal("GenerateDataKey's plaintext was not zeroed")
	}
	parsed, err := ParseWrapped(" " + EncodeWrapped(w) + ", ")
	if err != nil || len(parsed) != 1 || !bytes.Equal(parsed[0], w) {
		t.Fatalf("ParseWrapped(EncodeWrapped) = %v, %v", parsed, err)
	}
	if _, err := New(ctx, f, Options{KeyID: testKeyID, Wrapped: parsed}); err != nil {
		t.Fatal(err)
	}
}

func TestParseWrappedRefusesBadInput(t *testing.T) {
	for _, s := range []string{"", " , ", "not base64!"} {
		if _, err := ParseWrapped(s); err == nil {
			t.Errorf("ParseWrapped(%q) accepted", s)
		}
	}
}
