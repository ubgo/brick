package keysvault

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/vault/api"
	"github.com/ubgo/brick"
)

// The integration tests run against a real Vault only when
// BRICK_VAULT_TEST=1 (task test:keys:vault starts a throwaway dev server
// from the repo's compose.yml and removes it after). BRICK_VAULT_ADDR and
// BRICK_VAULT_TOKEN override the defaults, which match compose.yml.
const (
	integrationEnv = "BRICK_VAULT_TEST"
	defaultAddr    = "http://127.0.0.1:18200"
	defaultToken   = "brick-test-root"
)

func liveVault(t *testing.T) *api.Client {
	t.Helper()
	if os.Getenv(integrationEnv) != "1" {
		t.Skipf("set %s=1 to run against a real Vault", integrationEnv)
	}
	cfg := api.DefaultConfig()
	cfg.Address = defaultAddr
	if a := os.Getenv("BRICK_VAULT_ADDR"); a != "" {
		cfg.Address = a
	}
	c, err := api.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tok := os.Getenv("BRICK_VAULT_TOKEN")
	if tok == "" {
		tok = defaultToken
	}
	c.SetToken(tok)
	// A dev server has no transit mount; enabling it twice is an error
	// worth ignoring only when it is "already in use".
	if err := c.Sys().Mount("transit", &api.MountInput{Type: "transit"}); err != nil && !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("enable transit: %v", err)
	}
	return c
}

func transitKey(t *testing.T, c *api.Client, name string, derived bool) {
	t.Helper()
	if _, err := c.Logical().Write("transit/keys/"+name, map[string]any{"derived": derived}); err != nil {
		t.Fatalf("create transit key: %v", err)
	}
}

// The whole flow against Vault: make wrapped keys, start a ring, seal
// under the newest, open a blob sealed under the older one; then rotate
// the transit key itself and show that keys wrapped by v1 still unwrap.
func TestIntegrationEnvelopeRoundTrip(t *testing.T) {
	c := liveVault(t)
	ctx := context.Background()
	transitKey(t, c, "brick-roundtrip", false)
	opts := Options{KeyName: "brick-roundtrip"}

	oldW, err := NewWrappedKey(ctx, c.Logical(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Logical().Write("transit/keys/brick-roundtrip/rotate", nil); err != nil {
		t.Fatal(err)
	}
	newW, err := NewWrappedKey(ctx, c.Logical(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(oldW, "vault:v1:") || !strings.HasPrefix(newW, "vault:v2:") {
		t.Fatalf("transit versions: %.9s %.9s", oldW, newW)
	}

	oldOpts := opts
	oldOpts.Wrapped = []string{oldW}
	oldRing, err := New(ctx, c.Logical(), oldOpts)
	if err != nil {
		t.Fatal(err)
	}
	sealedOld, err := brick.NewAESGCMSealer(oldRing, nil).Seal(ctx, []byte("sealed before rotation"), []byte("ad"))
	if err != nil {
		t.Fatal(err)
	}
	opts.Wrapped, err = ParseWrapped(newW + "," + oldW)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := New(ctx, c.Logical(), opts)
	if err != nil {
		t.Fatal(err)
	}
	s := brick.NewAESGCMSealer(ring, nil)
	got, err := s.Open(ctx, sealedOld, []byte("ad"))
	if err != nil || string(got) != "sealed before rotation" {
		t.Fatalf("old blob after rotation: %q %v", got, err)
	}
	sealed, err := s.Seal(ctx, []byte("new"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := brick.NewAESGCMSealer(oldRing, nil).Open(ctx, sealed, nil); err == nil {
		t.Fatal("a blob sealed after rotation opened with only the old key: the ring is not sealing with the first key")
	}
}

// Real transit refuses the wrong key, the wrong derivation context and a
// token without permission, and New turns each into a refusal to start.
func TestIntegrationRefusesToStart(t *testing.T) {
	c := liveVault(t)
	ctx := context.Background()
	transitKey(t, c, "brick-a", false)
	transitKey(t, c, "brick-b", false)
	transitKey(t, c, "brick-derived", true)
	w, err := NewWrappedKey(ctx, c.Logical(), Options{KeyName: "brick-a"})
	if err != nil {
		t.Fatal(err)
	}
	dw, err := NewWrappedKey(ctx, c.Logical(), Options{KeyName: "brick-derived", Context: []byte("app=shop")})
	if err != nil {
		t.Fatal(err)
	}
	// A dev server accepts the root token for everything, so a token with
	// only the default policy stands in for "not allowed to decrypt".
	sec, err := c.Auth().Token().Create(&api.TokenCreateRequest{Policies: []string{"default"}, NoDefaultPolicy: false})
	if err != nil {
		t.Fatal(err)
	}
	weak, err := c.Clone()
	if err != nil {
		t.Fatal(err)
	}
	weak.SetToken(sec.Auth.ClientToken)

	for name, tc := range map[string]struct {
		vault Writer
		opts  Options
	}{
		"other transit key": {c.Logical(), Options{KeyName: "brick-b", Wrapped: []string{w}}},
		"wrong context":     {c.Logical(), Options{KeyName: "brick-derived", Wrapped: []string{dw}, Context: []byte("app=blog")}},
		"missing context":   {c.Logical(), Options{KeyName: "brick-derived", Wrapped: []string{dw}}},
		"no permission":     {weak.Logical(), Options{KeyName: "brick-a", Wrapped: []string{w}}},
		"no such key":       {c.Logical(), Options{KeyName: "brick-missing", Wrapped: []string{w}}},
	} {
		t.Run(name, func(t *testing.T) {
			ring, err := New(ctx, tc.vault, tc.opts)
			if err == nil || ring != nil {
				t.Fatalf("started: ring=%v err=%v", ring, err)
			}
			if strings.Contains(err.Error(), w) || strings.Contains(err.Error(), dw) {
				t.Fatalf("error carries the wrapped key: %v", err)
			}
			t.Logf("refused: %v", err)
		})
	}
	// The derived key with the right context does start.
	if _, err := New(ctx, c.Logical(), Options{KeyName: "brick-derived", Wrapped: []string{dw}, Context: []byte("app=shop")}); err != nil {
		t.Fatalf("derived key with its context: %v", err)
	}
}
