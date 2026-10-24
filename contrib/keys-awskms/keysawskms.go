// Package keysawskms is a brick.KeyRing whose state keys live in AWS KMS
// , by envelope encryption: each 32-byte data
// key is stored encrypted under a KMS key (in config, an environment
// variable, a secret store; the ciphertext is safe to keep anywhere), and
// New asks KMS to decrypt them once at start. Sealing and opening then run
// locally on brick's AES-GCM sealer, so no request ever waits on KMS or
// pays for a KMS call, and a KMS outage after start changes nothing.
//
// The plaintext keys exist only in this process's memory, the same as a
// BRICK_SECRET ring; what KMS adds is that the stored form is useless
// without IAM permission to decrypt, every decrypt is in CloudTrail, and
// revoking that permission stops new servers from starting.
package keysawskms

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/ubgo/brick"
)

// DecryptAPI is the one KMS call New makes. *kms.Client satisfies it; a
// test or a wrapper (retries, metrics) can stand in.
type DecryptAPI interface {
	Decrypt(ctx context.Context, in *kms.DecryptInput, optFns ...func(*kms.Options)) (*kms.DecryptOutput, error)
}

// GenerateDataKeyAPI is the KMS call NewWrappedKey makes. *kms.Client
// satisfies it.
type GenerateDataKeyAPI interface {
	GenerateDataKey(ctx context.Context, in *kms.GenerateDataKeyInput, optFns ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error)
}

// Options configures New.
type Options struct {
	// KeyID is the KMS key the data keys were encrypted under (id, ARN or
	// alias). Required: KMS can infer it for a symmetric key, but pinning
	// it means a ciphertext made under any other key is refused rather
	// than silently accepted.
	KeyID string
	// Wrapped holds the encrypted data keys, newest first: the first seals
	// and every one opens, exactly like brick.StaticKeys. Rotate by adding
	// a new wrapped key second, deploying, then promoting it to first.
	Wrapped [][]byte
	// EncryptionContext must match the context the keys were encrypted
	// with (none by default). KMS binds it as authenticated data and logs
	// it in CloudTrail, so a value like {"app": "shop"} keeps one app's
	// keys from opening in another.
	EncryptionContext map[string]string
}

// New decrypts every wrapped data key with KMS and returns a ring of the
// plaintexts. Any failure (no key, a KMS error, a key of the wrong size) is
// an error: a server must never start without its key or with a weaker one.
// Errors name the key's position, never its bytes.
func New(ctx context.Context, client DecryptAPI, opts Options) (brick.KeyRing, error) {
	if client == nil {
		return nil, errors.New("keys-awskms: no KMS client")
	}
	if opts.KeyID == "" {
		return nil, errors.New("keys-awskms: Options.KeyID is required, so a data key encrypted under another KMS key is refused")
	}
	if len(opts.Wrapped) == 0 {
		return nil, errors.New("keys-awskms: no wrapped data keys; make one with keysawskms.NewWrappedKey")
	}
	plain := make([][]byte, 0, len(opts.Wrapped))
	// The ring keeps its own copies (brick.NewKey clones), so the KMS
	// plaintexts are zeroed on every path out.
	defer func() {
		for _, p := range plain {
			clear(p)
		}
	}()
	for i, w := range opts.Wrapped {
		if len(w) == 0 {
			return nil, fmt.Errorf("keys-awskms: data key %d is empty", i+1)
		}
		out, err := client.Decrypt(ctx, &kms.DecryptInput{
			CiphertextBlob:      w,
			KeyId:               aws.String(opts.KeyID),
			EncryptionContext:   opts.EncryptionContext,
			EncryptionAlgorithm: types.EncryptionAlgorithmSpecSymmetricDefault,
		})
		if err != nil {
			return nil, fmt.Errorf("keys-awskms: decrypt data key %d: %w", i+1, err)
		}
		plain = append(plain, out.Plaintext)
		if len(out.Plaintext) != brick.StateKeySize {
			return nil, fmt.Errorf("keys-awskms: data key %d decrypts to %d bytes; a state key is %d (make one with keysawskms.NewWrappedKey)", i+1, len(out.Plaintext), brick.StateKeySize)
		}
	}
	return brick.StaticKeys(plain...)
}

// NewWrappedKey asks KMS for a fresh 256-bit data key and returns only its
// encrypted form, for Options.Wrapped; the plaintext KMS returns alongside
// is zeroed and never leaves this function. Store the result anywhere
// (base64 with EncodeWrapped); only a caller allowed to kms:Decrypt can use it.
func NewWrappedKey(ctx context.Context, client GenerateDataKeyAPI, keyID string, encryptionContext map[string]string) ([]byte, error) {
	if client == nil {
		return nil, errors.New("keys-awskms: no KMS client")
	}
	out, err := client.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
		KeyId:             aws.String(keyID),
		KeySpec:           types.DataKeySpecAes256,
		EncryptionContext: encryptionContext,
	})
	if err != nil {
		return nil, fmt.Errorf("keys-awskms: generate data key: %w", err)
	}
	clear(out.Plaintext)
	if len(out.CiphertextBlob) == 0 {
		return nil, errors.New("keys-awskms: KMS returned no ciphertext")
	}
	return out.CiphertextBlob, nil
}

// EncodeWrapped writes wrapped keys the way ParseWrapped reads them:
// standard base64, comma-separated, newest first.
func EncodeWrapped(wrapped ...[]byte) string {
	parts := make([]string, len(wrapped))
	for i, w := range wrapped {
		parts[i] = base64.StdEncoding.EncodeToString(w)
	}
	return strings.Join(parts, ",")
}

// ParseWrapped reads comma-separated base64 wrapped keys, newest first,
// from one environment variable or config value (the BRICK_SECRET shape,
// but ciphertext, so it is not itself a secret).
func ParseWrapped(s string) ([][]byte, error) {
	var out [][]byte
	for i, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(part)
		if err != nil {
			return nil, fmt.Errorf("keys-awskms: wrapped key %d is not base64", i+1)
		}
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil, errors.New("keys-awskms: no wrapped keys given")
	}
	return out, nil
}
