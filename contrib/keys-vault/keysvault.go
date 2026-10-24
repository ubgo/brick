// Package keysvault is a brick.KeyRing whose state keys are protected by
// Vault's transit engine, by envelope
// encryption: each 32-byte data key is stored wrapped by a transit key
// ("vault:v1:…", safe to keep in config or an environment variable), and
// New asks Vault to unwrap them once at start. Sealing and opening then run
// locally on brick's AES-GCM sealer, so no request waits on Vault, and a
// Vault outage or token expiry after start changes nothing.
//
// The plaintext keys exist only in this process's memory, the same as a
// BRICK_SECRET ring; what Vault adds is that the stored form is useless
// without a token allowed to decrypt with that transit key, every decrypt
// is in Vault's audit log, and revoking the policy stops new servers from
// starting.
package keysvault

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/vault/api"
	"github.com/ubgo/brick"
)

// DefaultMount is where Vault mounts the transit engine unless told
// otherwise (`vault secrets enable transit`).
const DefaultMount = "transit"

// Writer is the one Vault call this package makes: a write to a transit
// path. client.Logical() of an *api.Client satisfies it, so the host
// builds and authenticates the client (token, AppRole, Kubernetes, …) its
// own way, and a test can stand in.
type Writer interface {
	WriteWithContext(ctx context.Context, path string, data map[string]any) (*api.Secret, error)
}

// Options configures New.
type Options struct {
	// Mount is the transit engine's mount path; DefaultMount when empty.
	Mount string
	// KeyName is the transit key the data keys are wrapped with. Required.
	KeyName string
	// Wrapped holds the wrapped data keys ("vault:v1:…"), newest first:
	// the first seals and every one opens, exactly like brick.StaticKeys.
	// Rotate by adding a new wrapped key second, deploying, then promoting
	// it to first. Rotating the transit key itself needs no change here:
	// Vault still unwraps older versions until min_decryption_version
	// moves past them.
	Wrapped []string
	// Context is the derivation context for a transit key created with
	// derived=true; nil otherwise. It must match the one used to wrap.
	Context []byte
}

func (o Options) path(op string) string {
	mount := strings.Trim(o.Mount, "/")
	if mount == "" {
		mount = DefaultMount
	}
	return mount + "/" + op + "/" + o.KeyName
}

// New unwraps every data key with Vault and returns a ring of the
// plaintexts. Any failure (no key, a Vault error, a key of the wrong size)
// is an error: a server must never start without its key or with a weaker
// one. Errors name the key's position, never its bytes or its ciphertext.
func New(ctx context.Context, vault Writer, opts Options) (brick.KeyRing, error) {
	if vault == nil {
		return nil, errors.New("keys-vault: no Vault client")
	}
	if opts.KeyName == "" {
		return nil, errors.New("keys-vault: Options.KeyName (the transit key) is required")
	}
	if len(opts.Wrapped) == 0 {
		return nil, errors.New("keys-vault: no wrapped data keys; make one with keysvault.NewWrappedKey")
	}
	plain := make([][]byte, 0, len(opts.Wrapped))
	// The ring keeps its own copies (brick.NewKey clones), so the decoded
	// plaintexts are zeroed on every path out. The base64 string Vault's
	// JSON response carried is immutable and is left to the collector.
	defer func() {
		for _, p := range plain {
			clear(p)
		}
	}()
	for i, w := range opts.Wrapped {
		w = strings.TrimSpace(w)
		if !strings.HasPrefix(w, "vault:v") {
			return nil, fmt.Errorf("keys-vault: data key %d is not a transit ciphertext (vault:v…)", i+1)
		}
		data := map[string]any{"ciphertext": w}
		if opts.Context != nil {
			data["context"] = base64.StdEncoding.EncodeToString(opts.Context)
		}
		sec, err := vault.WriteWithContext(ctx, opts.path("decrypt"), data)
		if err != nil {
			return nil, fmt.Errorf("keys-vault: decrypt data key %d: %w", i+1, err)
		}
		var b64 string
		if sec != nil {
			b64, _ = sec.Data["plaintext"].(string)
		}
		if b64 == "" {
			return nil, fmt.Errorf("keys-vault: decrypt data key %d: Vault returned no plaintext", i+1)
		}
		p, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("keys-vault: decrypt data key %d: Vault's plaintext is not base64", i+1)
		}
		plain = append(plain, p)
		if len(p) != brick.StateKeySize {
			return nil, fmt.Errorf("keys-vault: data key %d unwraps to %d bytes; a state key is %d (make one with keysvault.NewWrappedKey)", i+1, len(p), brick.StateKeySize)
		}
	}
	return brick.StaticKeys(plain...)
}

// NewWrappedKey asks Vault for a fresh 256-bit data key in wrapped form
// only (transit's datakey/wrapped endpoint), for Options.Wrapped: the
// plaintext never leaves Vault. Only Mount, KeyName and Context of opts
// are used.
func NewWrappedKey(ctx context.Context, vault Writer, opts Options) (string, error) {
	if vault == nil {
		return "", errors.New("keys-vault: no Vault client")
	}
	if opts.KeyName == "" {
		return "", errors.New("keys-vault: Options.KeyName (the transit key) is required")
	}
	data := map[string]any{"bits": brick.StateKeySize * 8}
	if opts.Context != nil {
		data["context"] = base64.StdEncoding.EncodeToString(opts.Context)
	}
	sec, err := vault.WriteWithContext(ctx, opts.path("datakey/wrapped"), data)
	if err != nil {
		return "", fmt.Errorf("keys-vault: generate data key: %w", err)
	}
	var ct string
	if sec != nil {
		ct, _ = sec.Data["ciphertext"].(string)
	}
	if ct == "" {
		return "", errors.New("keys-vault: Vault returned no ciphertext")
	}
	return ct, nil
}

// ParseWrapped reads comma-separated wrapped keys, newest first, from one
// environment variable or config value (the BRICK_SECRET shape, but
// ciphertext, so it is not itself a secret). Transit ciphertexts never
// contain a comma.
func ParseWrapped(s string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("keys-vault: no wrapped keys given")
	}
	return out, nil
}
