package keysgcpkms_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"regexp"
	"testing/fstest"

	"cloud.google.com/go/kms/apiv1/kmspb"
	"github.com/googleapis/gax-go/v2"
	"github.com/ubgo/brick"
	keysgcpkms "github.com/ubgo/brick/contrib/keys-gcpkms"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// exampleKey is the CryptoKey resource name the data keys are encrypted
// under.
const exampleKey = "projects/shop/locations/global/keyRings/brick/cryptoKeys/state"

// xorMask is the stand-in "encryption" of localKMS.
const xorMask = 0x5a

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// crc returns the CRC32C checksum field Cloud KMS attaches to its requests and
// responses, which the client under test verifies.
func crc(b []byte) *wrapperspb.Int64Value {
	return wrapperspb.Int64(int64(crc32.Checksum(b, castagnoli)))
}

// localKMS stands in for *kms.KeyManagementClient so the example runs
// offline: it "encrypts" as keyName|aad|plaintext XOR a mask, returns the
// CRC32C checksums Cloud KMS returns, and refuses another key or AAD. In a
// real program the client is kms.NewKeyManagementClient(ctx).
type localKMS struct{}

func (localKMS) Encrypt(_ context.Context, req *kmspb.EncryptRequest, _ ...gax.CallOption) (*kmspb.EncryptResponse, error) {
	ct := []byte(req.GetName() + "|" + string(req.GetAdditionalAuthenticatedData()) + "|")
	for _, b := range req.GetPlaintext() {
		ct = append(ct, b^xorMask)
	}
	return &kmspb.EncryptResponse{
		Ciphertext:              ct,
		CiphertextCrc32C:        crc(ct),
		VerifiedPlaintextCrc32C: req.GetPlaintextCrc32C().GetValue() == crc(req.GetPlaintext()).GetValue(),
		VerifiedAdditionalAuthenticatedDataCrc32C: req.GetAdditionalAuthenticatedData() != nil &&
			req.GetAdditionalAuthenticatedDataCrc32C().GetValue() == crc(req.GetAdditionalAuthenticatedData()).GetValue(),
	}, nil
}

func (localKMS) Decrypt(_ context.Context, req *kmspb.DecryptRequest, _ ...gax.CallOption) (*kmspb.DecryptResponse, error) {
	prefix := []byte(req.GetName() + "|" + string(req.GetAdditionalAuthenticatedData()) + "|")
	if !bytes.HasPrefix(req.GetCiphertext(), prefix) {
		return nil, errors.New("rpc error: code = InvalidArgument desc = Decryption failed")
	}
	var plain []byte
	for _, b := range req.GetCiphertext()[len(prefix):] {
		plain = append(plain, b^xorMask)
	}
	return &kmspb.DecryptResponse{Plaintext: plain, PlaintextCrc32C: crc(plain)}, nil
}

// stateAttr finds the sealed instance a stateful component carries.
var stateAttr = regexp.MustCompile(`data-brick-state="([^"]+)"`)

// counterTheme is a page with one stateful component, so the example shows
// the ring sealing real state into the page.
var counterTheme = fstest.MapFS{
	"components/Counter.html": {Data: []byte("---\nstate:\n  n: 0\n---\n<p>{{ .State.n }}</p>\n")},
	"content/index.html":      {Data: []byte("<Counter />\n")},
}

// The ring is built once at start from wrapped (encrypted) data keys, then
// sealing runs locally: wire it into brick.Options.States through
// NewAESGCMSealer and NewSealedStates.
func Example() {
	ctx := context.Background()
	client := localKMS{}
	aad := []byte("app=shop")

	// Once, offline: make a wrapped data key and store its base64 anywhere;
	// it is ciphertext, not a secret.
	w, err := keysgcpkms.NewWrappedKey(ctx, client, exampleKey, aad)
	if err != nil {
		fmt.Println(err)
		return
	}
	stored := keysgcpkms.EncodeWrapped(w) // e.g. os.Getenv("BRICK_WRAPPED_KEYS")

	// At start: decrypt the data keys once and build the ring.
	wrapped, err := keysgcpkms.ParseWrapped(stored)
	if err != nil {
		fmt.Println(err)
		return
	}
	keys, err := keysgcpkms.New(ctx, client, keysgcpkms.Options{KeyName: exampleKey, Wrapped: wrapped, AAD: aad})
	if err != nil {
		fmt.Println(err) // never start without the key
		return
	}
	states, err := brick.NewSealedStates(brick.SealedOptions{Sealer: brick.NewAESGCMSealer(keys, nil)})
	if err != nil {
		fmt.Println(err)
		return
	}
	eng, err := brick.New(brick.Options{FS: counterTheme, States: states})
	if err != nil {
		fmt.Println(err)
		return
	}
	html, err := eng.Render(ctx, "content/index.html", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	// The page carries the sealed instance; the same ring opens it again.
	m := stateAttr.FindSubmatch(html)
	if m == nil {
		fmt.Println("no stateful component")
		return
	}
	view, err := eng.Instance(ctx, string(m[1]))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(view.Component, view.Store, view.State["n"])
	// Output:
	// Counter default 0
}

// A data key wrapped with other additional authenticated data (another
// app's) is refused at start.
func ExampleNew_wrongAAD() {
	ctx := context.Background()
	w, err := keysgcpkms.NewWrappedKey(ctx, localKMS{}, exampleKey, []byte("app=blog"))
	if err != nil {
		fmt.Println(err)
		return
	}
	_, err = keysgcpkms.New(ctx, localKMS{}, keysgcpkms.Options{KeyName: exampleKey, Wrapped: [][]byte{w}, AAD: []byte("app=shop")})
	fmt.Println(err)
	// Output:
	// keys-gcpkms: decrypt data key 1: rpc error: code = InvalidArgument desc = Decryption failed
}
