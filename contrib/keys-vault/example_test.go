package keysvault_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing/fstest"

	"github.com/hashicorp/vault/api"
	"github.com/ubgo/brick"
	keysvault "github.com/ubgo/brick/contrib/keys-vault"
)

// exampleKeyName is the transit key the data keys are wrapped with.
const exampleKeyName = "brick-state"

// localTransit stands in for client.Logical() so the example runs offline:
// it wraps as "vault:v1:<key name>:<base64 plaintext>" and refuses a
// ciphertext made under another key name, the check New relies on. In a
// real program the Writer is an authenticated *api.Client's Logical().
type localTransit struct{}

func (localTransit) WriteWithContext(_ context.Context, path string, data map[string]any) (*api.Secret, error) {
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	prefix := "vault:v1:" + name + ":"
	if strings.Contains(path, "/datakey/wrapped/") {
		k, err := brick.NewStateKey()
		if err != nil {
			return nil, err
		}
		return &api.Secret{Data: map[string]any{"ciphertext": prefix + base64.StdEncoding.EncodeToString(k)}}, nil
	}
	ct, _ := data["ciphertext"].(string)
	if !strings.HasPrefix(ct, prefix) {
		return nil, errors.New("Error making API request. Code: 400. Errors: * cipher: message authentication failed")
	}
	return &api.Secret{Data: map[string]any{"plaintext": strings.TrimPrefix(ct, prefix)}}, nil
}

// stateAttr finds the sealed instance a stateful component carries.
var stateAttr = regexp.MustCompile(`data-brick-state="([^"]+)"`)

// counterTheme is a page with one stateful component, so the example shows
// the ring sealing real state into the page.
var counterTheme = fstest.MapFS{
	"components/Counter.html": {Data: []byte("---\nstate:\n  n: 0\n---\n<p>{{ .State.n }}</p>\n")},
	"content/index.html":      {Data: []byte("<Counter />\n")},
}

// The ring is built once at start by unwrapping data keys with transit,
// then sealing runs locally: wire it into brick.Options.States through
// NewAESGCMSealer and NewSealedStates.
func Example() {
	ctx := context.Background()
	vault := localTransit{}
	opts := keysvault.Options{KeyName: exampleKeyName}

	// Once, offline: ask transit for a wrapped data key ("vault:v1:…") and
	// store it anywhere; it is ciphertext, not a secret.
	w, err := keysvault.NewWrappedKey(ctx, vault, opts)
	if err != nil {
		fmt.Println(err)
		return
	}

	// At start: unwrap the data keys once and build the ring.
	opts.Wrapped, err = keysvault.ParseWrapped(w) // e.g. os.Getenv("BRICK_WRAPPED_KEYS")
	if err != nil {
		fmt.Println(err)
		return
	}
	keys, err := keysvault.New(ctx, vault, opts)
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

// A value that is not a transit ciphertext is refused before Vault is
// asked, naming its position and never its bytes.
func ExampleNew_notCiphertext() {
	_, err := keysvault.New(context.Background(), localTransit{}, keysvault.Options{KeyName: exampleKeyName, Wrapped: []string{"c2VjcmV0"}})
	fmt.Println(err)
	// Output:
	// keys-vault: data key 1 is not a transit ciphertext (vault:v…)
}
