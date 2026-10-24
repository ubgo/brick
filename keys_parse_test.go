package brick

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// ParseStateKeys reads the BRICK_SECRET format: comma-separated keys,
// newest first, each standard or raw-URL base64 of exactly StateKeySize
// bytes. Blank entries are skipped, but a value with no key at all is an
// error; anything else wrong is an error that names the entry and never
// echoes key material. BV: dropping the length check accepts the short
// and long rows.
func TestParseStateKeys(t *testing.T) {
	t.Parallel()
	k1 := bytes.Repeat([]byte{1}, StateKeySize)
	k2 := bytes.Repeat([]byte{0xfb}, StateKeySize) // 0xfb encodes with + and / in standard base64
	std, raw := EncodeStateKey(k1), base64.RawURLEncoding.EncodeToString(k2)

	got, err := ParseStateKeys(" " + std + " , ," + raw + ",")
	if err != nil || len(got) != 2 || !bytes.Equal(got[0], k1) || !bytes.Equal(got[1], k2) {
		t.Fatalf("ParseStateKeys(std, raw-url) = %d keys, %v; want both, newest first", len(got), err)
	}
	if got, err := ParseStateKeys(" , "); err == nil {
		t.Errorf("only blanks = %d keys and no error; want an error, since a ring needs a key", len(got))
	}
	short := base64.StdEncoding.EncodeToString(k1[:StateKeySize-1])
	long := base64.StdEncoding.EncodeToString(append(bytes.Clone(k1), 0))
	for in, want := range map[string]string{
		"not base64!":     "state key 1 is not base64",
		std + "," + short: fmt.Sprintf("state key 2 is %d bytes", StateKeySize-1),
		long:              fmt.Sprintf("state key 1 is %d bytes", StateKeySize+1),
	} {
		_, err := ParseStateKeys(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseStateKeys(%q) error = %v; want %q", in, err, want)
		}
		if err != nil && strings.Contains(err.Error(), std) {
			t.Errorf("the error for %q echoes a key: %v", in, err)
		}
	}
	// EncodeStateKey and ParseStateKeys are inverses.
	if got, err := ParseStateKeys(EncodeStateKey(k2)); err != nil || !bytes.Equal(got[0], k2) {
		t.Errorf("round trip = %v, %v", got, err)
	}
}

// ValidPresenceTopic is ValidTopic plus a length bound, so the engine's
// control topic built from it (brick:presence:<topic>) is still well formed
// and a Broadcaster carries it.
func TestValidPresenceTopic(t *testing.T) {
	t.Parallel()
	ok := strings.Repeat("r", MaxPresenceTopicLen)
	if !ValidPresenceTopic(ok) || !ValidBroadcastTopic(presenceControl(ok)) {
		t.Errorf("a topic of the maximum length (%d) is refused, or its control topic is not a topic", MaxPresenceTopicLen)
	}
	for _, bad := range []string{ok + "r", "", "has space"} {
		if ValidPresenceTopic(bad) {
			t.Errorf("ValidPresenceTopic(%q) = true", bad)
		}
	}
}

// StateScopeFrom returns the zero scope for a context the engine never
// touched, and the scope WithStateScope set otherwise.
func TestStateScopeFrom(t *testing.T) {
	t.Parallel()
	if s := StateScopeFrom(context.Background()); s != (StateScope{}) {
		t.Errorf("bare context scope = %+v, want zero", s)
	}
	want := StateScope{Store: "default", Session: "abcd"}
	if s := StateScopeFrom(WithStateScope(context.Background(), want)); s != want {
		t.Errorf("scope = %+v, want %+v", s, want)
	}
}
