package brick

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Sealed state: the whole instance, encrypted and
// authenticated, travels in the page as its handle. The server keeps
// nothing, so any process holding the key ring opens any blob — a restart,
// a deploy, a second server, a static export.
const (
	// DefaultSealedTTL bounds how old a sealed blob may be.
	DefaultSealedTTL = 12 * time.Hour
	// DefaultSealedMaxBytes caps a blob; larger state belongs in a server
	// store.
	DefaultSealedMaxBytes = 64 << 10
	// sealedPrefix starts every blob handle; the digit is the store's
	// handle format, so a future one can be told apart before decoding.
	sealedPrefix = "s1."
	// sealedSkew is the clock allowance when checking expiry (gotcha 5).
	sealedSkew = 2 * time.Minute
	// expSize prefixes the plaintext with the expiry (unix seconds, 0 =
	// none), inside the encryption so it cannot be changed.
	expSize = 8
	// sealedAAD starts the additional data SealedStates binds.
	sealedAAD = "brick-state"
)

// AES-GCM sealer envelope: version | keyID | salt | ciphertext+tag.
const (
	gcmVersion    = 1
	gcmSaltSize   = 32
	gcmNonceSize  = 12
	gcmHeaderSize = 1 + KeyIDSize + gcmSaltSize
	// gcmInfo is the HKDF info: it separates these subkeys from any other
	// use of the same key.
	gcmInfo = "brick-state v1"
)

// ErrSealOpen is every Sealer.Open failure — wrong key, changed byte,
// other additional data — so a caller (and a prober) learns nothing about
// which.
var ErrSealOpen = errors.New("brick: sealed value does not open")

// Sealer turns bytes into sealed bytes and back (AEAD). It knows nothing
// about instances; SealedStates and Bytes use it.
type Sealer interface {
	// Seal encrypts and authenticates plaintext, binding ad without storing
	// it. The result carries what Open needs (version, key id, salt).
	Seal(ctx context.Context, plaintext, ad []byte) ([]byte, error)
	// Open reverses Seal. Any failure is ErrSealOpen, never a partial result.
	Open(ctx context.Context, sealed, ad []byte) ([]byte, error)
}

// aesGCMSealer is the default Sealer.
type aesGCMSealer struct {
	keys KeyRing
	rand io.Reader
}

// NewAESGCMSealer is the default Sealer: AES-256-GCM under a per-blob
// subkey, HKDF-SHA256(key, salt, "brick-state v1") giving the AES key and
// nonce from a random 32-byte salt per blob. A fresh salt per blob removes
// GCM's nonce-collision bound: no counter, no state, safe for concurrent
// use. rand nil means crypto/rand (any other reader is for tests only).
func NewAESGCMSealer(keys KeyRing, r io.Reader) Sealer {
	if r == nil {
		r = rand.Reader
	}
	return &aesGCMSealer{keys: keys, rand: r}
}

func (s *aesGCMSealer) Seal(ctx context.Context, plaintext, ad []byte) ([]byte, error) {
	k, err := s.keys.Current(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]byte, gcmHeaderSize, gcmHeaderSize+len(plaintext)+16)
	out[0] = gcmVersion
	copy(out[1:], k.ID[:])
	if _, err := io.ReadFull(s.rand, out[1+KeyIDSize:gcmHeaderSize]); err != nil {
		return nil, err
	}
	aead, nonce, err := gcmSubkey(k.Bytes, out[1+KeyIDSize:gcmHeaderSize])
	if err != nil {
		return nil, err
	}
	return aead.Seal(out, nonce, plaintext, gcmAD(out[:1+KeyIDSize], ad)), nil
}

func (s *aesGCMSealer) Open(ctx context.Context, sealed, ad []byte) ([]byte, error) {
	if len(sealed) < gcmHeaderSize || sealed[0] != gcmVersion {
		return nil, ErrSealOpen
	}
	var id [KeyIDSize]byte
	copy(id[:], sealed[1:1+KeyIDSize])
	k, ok, err := s.keys.Lookup(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSealOpen
	}
	aead, nonce, err := gcmSubkey(k.Bytes, sealed[1+KeyIDSize:gcmHeaderSize])
	if err != nil {
		return nil, ErrSealOpen
	}
	plain, err := aead.Open(nil, nonce, sealed[gcmHeaderSize:], gcmAD(sealed[:1+KeyIDSize], ad))
	if err != nil {
		return nil, ErrSealOpen
	}
	return plain, nil
}

// gcmSubkey derives the per-blob AES-256-GCM key and nonce.
func gcmSubkey(key, salt []byte) (cipher.AEAD, []byte, error) {
	out, err := hkdf.Key(sha256.New, key, salt, gcmInfo, StateKeySize+gcmNonceSize)
	if err != nil {
		return nil, nil, err
	}
	block, err := aes.NewCipher(out[:StateKeySize])
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	return aead, out[StateKeySize:], nil
}

// gcmAD binds the envelope header (version, key id) to the caller's ad, so
// neither can be swapped.
func gcmAD(head, ad []byte) []byte {
	return append(bytes.Clone(head), ad...)
}

// SealedOptions configures NewSealedStates.
type SealedOptions struct {
	// Sealer is the AEAD piece; required (NewAESGCMSealer over a KeyRing).
	Sealer Sealer
	// Codec encodes the plaintext; nil means Options.Codec, and failing
	// that JSONCodec.
	Codec Codec
	// TTL bounds a blob's age: each seal writes exp = now + TTL, so a
	// component in use keeps renewing it. Zero means DefaultSealedTTL.
	TTL time.Duration
	// MaxBytes refuses a larger blob at render, naming the component. Zero
	// means DefaultSealedMaxBytes.
	MaxBytes int
	// Revoked refuses a blob issued before the session was revoked, for
	// apps that cannot rotate session ids on sign-out. session is the
	// hashed id (Instance.Session). Nil means no check.
	Revoked func(ctx context.Context, session string, issued time.Time) bool
	// Clock replaces time.Now — for tests.
	Clock Clock
}

// SealedStates is a StateStore that keeps nothing: Put seals the instance
// into the handle (Codec → Sealer → base64url), Get opens it. It holds only
// immutable pieces, so it is safe for concurrent use without a lock.
type SealedStates struct {
	opts SealedOptions
}

// NewSealedStates checks the options and returns the store.
func NewSealedStates(opts SealedOptions) (*SealedStates, error) {
	if opts.Sealer == nil {
		return nil, errors.New("brick: SealedOptions.Sealer is required (brick.NewAESGCMSealer over brick.StaticKeys or brick.KeysFromEnv)")
	}
	if opts.TTL <= 0 {
		opts.TTL = DefaultSealedTTL
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultSealedMaxBytes
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &SealedStates{opts: opts}, nil
}

// withCodec returns a copy using c when none was set (Options.Codec).
func (s *SealedStates) withCodec(c Codec) *SealedStates {
	if s.opts.Codec != nil || c == nil {
		return s
	}
	cp := *s
	cp.opts.Codec = c
	return &cp
}

func (s *SealedStates) codec() Codec {
	if s.opts.Codec != nil {
		return s.opts.Codec
	}
	return JSONCodec{}
}

// Put seals inst and returns the blob as the handle.
func (s *SealedStates) Put(ctx context.Context, inst *Instance) (string, error) {
	plain, err := s.codec().Encode(inst)
	if err != nil {
		return "", err
	}
	var exp int64
	scope := StateScopeFrom(ctx)
	if !scope.NoExpiry {
		exp = s.opts.Clock().Add(s.opts.TTL).Unix()
	}
	msg := make([]byte, expSize+len(plain))
	binary.BigEndian.PutUint64(msg, uint64(exp))
	copy(msg[expSize:], plain)
	blob, err := s.opts.Sealer.Seal(ctx, msg, sealedStateAD(scope.Store, inst.Session))
	if err != nil {
		return "", err
	}
	handle := sealedPrefix + base64.RawURLEncoding.EncodeToString(blob)
	if len(handle) > s.opts.MaxBytes {
		return "", fmt.Errorf("%w: <%s> holds %d bytes of sealed state (including stored children), over SealedOptions.MaxBytes (%d): keep ids in state and load rows in the action, or give it brick: {store: <a server store>}", ErrStateValue, inst.Name, len(handle), s.opts.MaxBytes)
	}
	return handle, nil
}

// Get opens a blob. Anything that does not open — a changed byte, a
// retired key, another store or session, an expired blob, an unknown
// version — is "not found" (ok=false, nil error): the component recovers
// in place, and a prober learns nothing about why. A KeyRing failure (a
// KMS that is down) is an error, so the action answers 503.
func (s *SealedStates) Get(ctx context.Context, handle string) (*Instance, bool, error) {
	rest, ok := strings.CutPrefix(handle, sealedPrefix)
	if !ok || len(rest) > s.opts.MaxBytes {
		return nil, false, nil
	}
	// Strict: a handle has exactly one spelling. The lenient decoder ignores
	// the unused low bits of the last character, so a changed character
	// could still open the same blob; Put only ever writes the canonical
	// form, so strictness refuses nothing real.
	blob, err := base64.RawURLEncoding.Strict().DecodeString(rest)
	if err != nil {
		return nil, false, nil
	}
	scope := StateScopeFrom(ctx)
	msg, err := s.opts.Sealer.Open(ctx, blob, sealedStateAD(scope.Store, scope.Session))
	switch {
	case errors.Is(err, ErrSealOpen):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	case len(msg) < expSize:
		return nil, false, nil
	}
	if exp := int64(binary.BigEndian.Uint64(msg)); exp != 0 && s.opts.Clock().After(time.Unix(exp, 0).Add(sealedSkew)) {
		return nil, false, nil
	}
	inst, err := s.codec().Decode(msg[expSize:])
	if err != nil {
		return nil, false, nil
	}
	if s.opts.Revoked != nil && s.opts.Revoked(ctx, inst.Session, inst.Issued) {
		return nil, false, nil
	}
	inst.Handle = handle
	return inst, true, nil
}

// Delete is a no-op: the server keeps nothing to delete.
func (s *SealedStates) Delete(context.Context, string) error { return nil }

// sealedStateAD binds a blob to the store it was sealed for and the
// session it belongs to, without storing either: opened under another
// store name or another session, it fails authentication. session is the
// hashed id; at Put it is the instance's, at Get the live request's. The
// component path is not in it — an action names only the handle, so the
// path is not known before opening; it is inside the encrypted plaintext,
// so a blob always opens as the component that sealed it.
func sealedStateAD(store, session string) []byte {
	var b bytes.Buffer
	b.WriteString(sealedAAD)
	for _, part := range []string{store, session} {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(part)))
		b.Write(n[:])
		b.WriteString(part)
	}
	return b.Bytes()
}
