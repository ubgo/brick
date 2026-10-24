package keysawskms

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/ubgo/brick"
)

// The integration tests run against a real KMS API only when
// BRICK_KMS_TEST=1 (task test:keys:aws starts a throwaway LocalStack from
// the repo's compose.yml and removes it after). BRICK_KMS_ENDPOINT
// overrides the endpoint; unset, the default matches compose.yml.
const (
	integrationEnv  = "BRICK_KMS_TEST"
	defaultEndpoint = "http://127.0.0.1:14566"
)

func liveClient(t *testing.T) *kms.Client {
	t.Helper()
	if os.Getenv(integrationEnv) != "1" {
		t.Skipf("set %s=1 to run against a real KMS API", integrationEnv)
	}
	endpoint := os.Getenv("BRICK_KMS_ENDPOINT")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	return kms.New(kms.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
	})
}

func liveKey(t *testing.T, c *kms.Client) string {
	t.Helper()
	out, err := c.CreateKey(context.Background(), &kms.CreateKeyInput{Description: aws.String("brick keys-awskms integration test")})
	if err != nil {
		t.Fatalf("create KMS key: %v", err)
	}
	return aws.ToString(out.KeyMetadata.Arn)
}

// The whole flow against a KMS: make wrapped keys, start a ring from them
// with an encryption context, seal under the newest, open a blob sealed
// under the older one.
func TestIntegrationEnvelopeRoundTrip(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	keyID := liveKey(t, c)
	ec := map[string]string{"app": "brick-test"}

	oldW, err := NewWrappedKey(ctx, c, keyID, ec)
	if err != nil {
		t.Fatal(err)
	}
	newW, err := NewWrappedKey(ctx, c, keyID, ec)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := ParseWrapped(EncodeWrapped(newW, oldW))
	if err != nil {
		t.Fatal(err)
	}

	oldRing, err := New(ctx, c, Options{KeyID: keyID, Wrapped: wrapped[1:], EncryptionContext: ec})
	if err != nil {
		t.Fatal(err)
	}
	sealedOld, err := brick.NewAESGCMSealer(oldRing, nil).Seal(ctx, []byte("sealed before rotation"), []byte("ad"))
	if err != nil {
		t.Fatal(err)
	}

	ring, err := New(ctx, c, Options{KeyID: keyID, Wrapped: wrapped, EncryptionContext: ec})
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

// Real KMS refuses the wrong encryption context and the wrong key, and
// New turns that into a refusal to start, without key bytes in the error.
func TestIntegrationRefusesToStart(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	keyID := liveKey(t, c)
	otherKey := liveKey(t, c)
	w, err := NewWrappedKey(ctx, c, keyID, map[string]string{"app": "shop"})
	if err != nil {
		t.Fatal(err)
	}
	for name, opts := range map[string]Options{
		"wrong context": {KeyID: keyID, Wrapped: [][]byte{w}, EncryptionContext: map[string]string{"app": "blog"}},
		"no context":    {KeyID: keyID, Wrapped: [][]byte{w}},
		"other key":     {KeyID: otherKey, Wrapped: [][]byte{w}, EncryptionContext: map[string]string{"app": "shop"}},
		"not a blob":    {KeyID: keyID, Wrapped: [][]byte{[]byte("not a ciphertext blob")}},
	} {
		t.Run(name, func(t *testing.T) {
			ring, err := New(ctx, c, opts)
			if err == nil || ring != nil {
				t.Fatalf("started: ring=%v err=%v", ring, err)
			}
			if strings.Contains(err.Error(), EncodeWrapped(w)) {
				t.Fatalf("error carries the wrapped key: %v", err)
			}
			t.Logf("refused: %v", err)
		})
	}
}
