package keysvault

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/vault/api"
	"github.com/ubgo/brick"
)

// fakeTransit wraps as "vault:v1:<keyname>:<base64 plaintext>" and refuses
// a decrypt under another key name, the check real transit makes that New
// relies on. It records every path it was asked for.
type fakeTransit struct {
	err   error
	size  int
	paths []string
}

func wrapFor(name string, plain []byte) string {
	return "vault:v1:" + name + ":" + base64.StdEncoding.EncodeToString(plain)
}

func (f *fakeTransit) WriteWithContext(_ context.Context, path string, data map[string]any) (*api.Secret, error) {
	f.paths = append(f.paths, path)
	if f.err != nil {
		return nil, f.err
	}
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	if strings.Contains(path, "/datakey/wrapped/") {
		k, err := brick.NewStateKey()
		if err != nil {
			return nil, err
		}
		return &api.Secret{Data: map[string]any{"ciphertext": wrapFor(name, k)}}, nil
	}
	ct, _ := data["ciphertext"].(string)
	prefix := "vault:v1:" + name + ":"
	if !strings.HasPrefix(ct, prefix) {
		return nil, errors.New("Error making API request. Code: 400. Errors: * cipher: message authentication failed")
	}
	plain := strings.TrimPrefix(ct, prefix)
	if f.size > 0 {
		b, _ := base64.StdEncoding.DecodeString(plain)
		plain = base64.StdEncoding.EncodeToString(b[:f.size])
	}
	return &api.Secret{Data: map[string]any{"plaintext": plain}}, nil
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
	f := &fakeTransit{}
	newK, oldK := mustKey(t), mustKey(t)
	oldRing, err := New(ctx, f, Options{KeyName: "brick", Wrapped: []string{wrapFor("brick", oldK)}})
	if err != nil {
		t.Fatal(err)
	}
	sealedOld, err := brick.NewAESGCMSealer(oldRing, nil).Seal(ctx, []byte("old state"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := New(ctx, f, Options{Mount: "/kv-transit/", KeyName: "brick", Wrapped: []string{wrapFor("brick", newK), wrapFor("brick", oldK)}})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.paths[len(f.paths)-1]; got != "kv-transit/decrypt/brick" {
		t.Fatalf("decrypt path = %q", got)
	}
	cur, _ := ring.Current(ctx)
	if !bytes.Equal(cur.Bytes, newK) {
		t.Fatal("Current is not the first (newest) data key")
	}
	got, err := brick.NewAESGCMSealer(ring, nil).Open(ctx, sealedOld, nil)
	if err != nil || string(got) != "old state" {
		t.Fatalf("a blob sealed under the older key does not open: %q %v", got, err)
	}
}

// A server must never start without its key: every failure on the way to
// a ring is an error, and the error never carries key material.
func TestRefusesToStartWithoutAKey(t *testing.T) {
	ctx := context.Background()
	k := mustKey(t)
	good := wrapFor("brick", k)
	cases := map[string]struct {
		vault Writer
		opts  Options
	}{
		"no client":        {nil, Options{KeyName: "brick", Wrapped: []string{good}}},
		"no key name":      {&fakeTransit{}, Options{Wrapped: []string{good}}},
		"no wrapped keys":  {&fakeTransit{}, Options{KeyName: "brick"}},
		"not a ciphertext": {&fakeTransit{}, Options{KeyName: "brick", Wrapped: []string{base64.StdEncoding.EncodeToString(k)}}},
		"vault down":       {&fakeTransit{err: errors.New("dial tcp: connection refused")}, Options{KeyName: "brick", Wrapped: []string{good}}},
		"other key name":   {&fakeTransit{}, Options{KeyName: "other", Wrapped: []string{good}}},
		"short data key":   {&fakeTransit{size: 16}, Options{KeyName: "brick", Wrapped: []string{good}}},
		"second failing":   {&fakeTransit{}, Options{KeyName: "brick", Wrapped: []string{good, wrapFor("other", k)}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ring, err := New(ctx, tc.vault, tc.opts)
			if err == nil || ring != nil {
				t.Fatalf("New started without a usable key: ring=%v err=%v", ring, err)
			}
			msg := err.Error()
			if strings.Contains(msg, base64.StdEncoding.EncodeToString(k)) || strings.Contains(msg, string(k)) {
				t.Fatalf("error leaks the key: %s", msg)
			}
		})
	}
}

func TestNewWrappedKey(t *testing.T) {
	ctx := context.Background()
	f := &fakeTransit{}
	w, err := NewWrappedKey(ctx, f, Options{KeyName: "brick"})
	if err != nil {
		t.Fatal(err)
	}
	if f.paths[0] != "transit/datakey/wrapped/brick" {
		t.Fatalf("path = %q", f.paths[0])
	}
	parsed, err := ParseWrapped(" " + w + " ,")
	if err != nil || len(parsed) != 1 || parsed[0] != w {
		t.Fatalf("ParseWrapped = %v, %v", parsed, err)
	}
	if _, err := New(ctx, f, Options{KeyName: "brick", Wrapped: parsed}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWrappedKey(ctx, &fakeTransit{err: errors.New("403 permission denied")}, Options{KeyName: "brick"}); err == nil {
		t.Fatal("a Vault error made a key")
	}
	if _, err := ParseWrapped(" , "); err == nil {
		t.Fatal("ParseWrapped accepted nothing")
	}
}
