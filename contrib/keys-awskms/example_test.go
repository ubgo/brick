package keysawskms_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing/fstest"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/ubgo/brick"
	keysawskms "github.com/ubgo/brick/contrib/keys-awskms"
)

// exampleKeyID is the KMS key the data keys are encrypted under.
const exampleKeyID = "alias/brick-state"

// localKMS stands in for *kms.Client so the example runs offline: it
// "encrypts" by prefixing the key id and XOR-ing each byte, and refuses a
// ciphertext made under another key id, the check New relies on. In a real
// program the client is kms.NewFromConfig(cfg).
type localKMS struct{}

// xorMask is the stand-in "encryption" of localKMS; it is not a secret.
const xorMask = 0x5a

func (localKMS) GenerateDataKey(_ context.Context, in *kms.GenerateDataKeyInput, _ ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error) {
	plain, err := brick.NewStateKey()
	if err != nil {
		return nil, err
	}
	blob := []byte(aws.ToString(in.KeyId) + "|")
	for _, b := range plain {
		blob = append(blob, b^xorMask)
	}
	return &kms.GenerateDataKeyOutput{Plaintext: plain, CiphertextBlob: blob}, nil
}

func (localKMS) Decrypt(_ context.Context, in *kms.DecryptInput, _ ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	prefix := []byte(aws.ToString(in.KeyId) + "|")
	if !bytes.HasPrefix(in.CiphertextBlob, prefix) {
		return nil, errors.New("IncorrectKeyException")
	}
	var plain []byte
	for _, b := range in.CiphertextBlob[len(prefix):] {
		plain = append(plain, b^xorMask)
	}
	return &kms.DecryptOutput{Plaintext: plain}, nil
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

	// Once, offline: make a wrapped data key and store its base64 anywhere
	// (an environment variable, config); it is ciphertext, not a secret.
	w, err := keysawskms.NewWrappedKey(ctx, client, exampleKeyID, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	stored := keysawskms.EncodeWrapped(w) // e.g. os.Getenv("BRICK_WRAPPED_KEYS")

	// At start: decrypt the data keys once and build the ring.
	wrapped, err := keysawskms.ParseWrapped(stored)
	if err != nil {
		fmt.Println(err)
		return
	}
	keys, err := keysawskms.New(ctx, client, keysawskms.Options{KeyID: exampleKeyID, Wrapped: wrapped})
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

// A data key encrypted under another KMS key is refused at start, so a
// server never runs with a key it was not meant to have.
func ExampleNew_wrongKey() {
	ctx := context.Background()
	w, err := keysawskms.NewWrappedKey(ctx, localKMS{}, "alias/other-app", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	_, err = keysawskms.New(ctx, localKMS{}, keysawskms.Options{KeyID: exampleKeyID, Wrapped: [][]byte{w}})
	fmt.Println(err)
	// Output:
	// keys-awskms: decrypt data key 1: IncorrectKeyException
}
