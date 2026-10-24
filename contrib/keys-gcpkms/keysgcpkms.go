// Package keysgcpkms is a brick.KeyRing whose state keys are protected by
// Google Cloud KMS, by envelope encryption:
// each 32-byte data key is stored encrypted under a Cloud KMS key (in
// config, an environment variable, a secret store; the ciphertext is safe
// to keep anywhere), and New asks Cloud KMS to decrypt them once at start.
// Sealing and opening then run locally on brick's AES-GCM sealer, so no
// request waits on Cloud KMS or pays for a call, and an outage after start
// changes nothing.
//
// The plaintext keys exist only in this process's memory, the same as a
// BRICK_SECRET ring; what Cloud KMS adds is that the stored form is useless
// without IAM permission to decrypt, every decrypt is in Cloud Audit Logs,
// and revoking that permission stops new servers from starting.
//
// Tested against an in-process fake of the API only: Google ships no KMS
// emulator, and this module has not been run against real Cloud KMS.
package keysgcpkms

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"

	"cloud.google.com/go/kms/apiv1/kmspb"
	"github.com/googleapis/gax-go/v2"
	"github.com/ubgo/brick"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// DecryptAPI is the one Cloud KMS call New makes.
// *kms.KeyManagementClient satisfies it; a test or a wrapper can stand in.
type DecryptAPI interface {
	Decrypt(ctx context.Context, req *kmspb.DecryptRequest, opts ...gax.CallOption) (*kmspb.DecryptResponse, error)
}

// EncryptAPI is the Cloud KMS call NewWrappedKey makes.
// *kms.KeyManagementClient satisfies it.
type EncryptAPI interface {
	Encrypt(ctx context.Context, req *kmspb.EncryptRequest, opts ...gax.CallOption) (*kmspb.EncryptResponse, error)
}

// Options configures New.
type Options struct {
	// KeyName is the CryptoKey's resource name,
	// projects/P/locations/L/keyRings/R/cryptoKeys/K. Required. Cloud KMS
	// picks the version from the ciphertext, so rotating the CryptoKey
	// needs no change here while older versions stay enabled.
	KeyName string
	// Wrapped holds the encrypted data keys, newest first: the first seals
	// and every one opens, exactly like brick.StaticKeys. Rotate by adding
	// a new wrapped key second, deploying, then promoting it to first.
	Wrapped [][]byte
	// AAD must match the additional authenticated data the keys were
	// encrypted with (none by default), so a value like "app=shop" keeps
	// one app's keys from opening in another.
	AAD []byte
}

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

func crc(b []byte) *wrapperspb.Int64Value {
	return wrapperspb.Int64(int64(crc32.Checksum(b, castagnoli)))
}

// New decrypts every wrapped data key with Cloud KMS and returns a ring of
// the plaintexts. Requests and responses carry CRC32C checksums, Google's
// recommended end-to-end integrity check, so a corrupted plaintext is a
// refusal, not a wrong key. Any failure is an error: a server must never
// start without its key or with a weaker one. Errors name the key's
// position, never its bytes.
func New(ctx context.Context, client DecryptAPI, opts Options) (brick.KeyRing, error) {
	if client == nil {
		return nil, errors.New("keys-gcpkms: no Cloud KMS client")
	}
	if opts.KeyName == "" {
		return nil, errors.New("keys-gcpkms: Options.KeyName (the CryptoKey resource name) is required")
	}
	if len(opts.Wrapped) == 0 {
		return nil, errors.New("keys-gcpkms: no wrapped data keys; make one with keysgcpkms.NewWrappedKey")
	}
	plain := make([][]byte, 0, len(opts.Wrapped))
	// The ring keeps its own copies (brick.NewKey clones), so the Cloud
	// KMS plaintexts are zeroed on every path out.
	defer func() {
		for _, p := range plain {
			clear(p)
		}
	}()
	for i, w := range opts.Wrapped {
		if len(w) == 0 {
			return nil, fmt.Errorf("keys-gcpkms: data key %d is empty", i+1)
		}
		req := &kmspb.DecryptRequest{Name: opts.KeyName, Ciphertext: w, CiphertextCrc32C: crc(w)}
		if opts.AAD != nil {
			req.AdditionalAuthenticatedData = opts.AAD
			req.AdditionalAuthenticatedDataCrc32C = crc(opts.AAD)
		}
		resp, err := client.Decrypt(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("keys-gcpkms: decrypt data key %d: %w", i+1, err)
		}
		plain = append(plain, resp.GetPlaintext())
		if resp.GetPlaintextCrc32C() == nil || resp.GetPlaintextCrc32C().GetValue() != crc(resp.GetPlaintext()).GetValue() {
			return nil, fmt.Errorf("keys-gcpkms: data key %d: the plaintext's checksum does not match (corrupted in transit)", i+1)
		}
		if len(resp.GetPlaintext()) != brick.StateKeySize {
			return nil, fmt.Errorf("keys-gcpkms: data key %d decrypts to %d bytes; a state key is %d (make one with keysgcpkms.NewWrappedKey)", i+1, len(resp.GetPlaintext()), brick.StateKeySize)
		}
	}
	return brick.StaticKeys(plain...)
}

// NewWrappedKey makes a fresh 32-byte data key locally, encrypts it with
// Cloud KMS (which has no data-key endpoint) and returns only the
// ciphertext, for Options.Wrapped; the local plaintext is zeroed before
// returning. The integrity checks Cloud KMS reports are verified.
func NewWrappedKey(ctx context.Context, client EncryptAPI, keyName string, aad []byte) ([]byte, error) {
	if client == nil {
		return nil, errors.New("keys-gcpkms: no Cloud KMS client")
	}
	plain := make([]byte, brick.StateKeySize)
	defer clear(plain)
	if _, err := rand.Read(plain); err != nil {
		return nil, err
	}
	req := &kmspb.EncryptRequest{Name: keyName, Plaintext: plain, PlaintextCrc32C: crc(plain)}
	if aad != nil {
		req.AdditionalAuthenticatedData = aad
		req.AdditionalAuthenticatedDataCrc32C = crc(aad)
	}
	resp, err := client.Encrypt(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("keys-gcpkms: encrypt data key: %w", err)
	}
	if !resp.GetVerifiedPlaintextCrc32C() || (aad != nil && !resp.GetVerifiedAdditionalAuthenticatedDataCrc32C()) {
		return nil, errors.New("keys-gcpkms: Cloud KMS did not verify the request checksum (corrupted in transit)")
	}
	if resp.GetCiphertextCrc32C() == nil || resp.GetCiphertextCrc32C().GetValue() != crc(resp.GetCiphertext()).GetValue() {
		return nil, errors.New("keys-gcpkms: the ciphertext's checksum does not match (corrupted in transit)")
	}
	return resp.GetCiphertext(), nil
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
			return nil, fmt.Errorf("keys-gcpkms: wrapped key %d is not base64", i+1)
		}
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil, errors.New("keys-gcpkms: no wrapped keys given")
	}
	return out, nil
}
