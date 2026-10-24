package statesredis

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ubgo/brick"
	"github.com/ubgo/brick/statetest"
)

// endLog records what a watcher was told.
type endLog struct {
	mu   sync.Mutex
	seen []string // "reason key data"
}

func (l *endLog) fn(_ context.Context, key string, data []byte, r brick.EndReason) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, string(r)+" "+key+" "+string(data))
}

func (l *endLog) sorted() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := slices.Clone(l.seen)
	slices.Sort(out)
	return out
}

// Redis says it reports revocation only: expiry deletes the value inside
// Redis, so claiming EndExpired would be a lie.
func TestEndReasonsAreRevokedOnly(t *testing.T) {
	s := New(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}), Options{})
	if got := s.WatchEndsBytes(nil); !slices.Equal(got, []brick.EndReason{brick.EndRevoked}) {
		t.Fatalf("reasons = %v, want [revoked]", got)
	}
}

func TestIntegrationEndsRevoked(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	s := New(c, Options{Prefix: prefix})
	var log endLog
	s.WatchEndsBytes(log.fn)
	for _, kv := range [][2]string{{"a1", "sa"}, {"a2", "sa"}, {"b1", "sb"}} {
		if err := s.Save(ctx, kv[0], []byte("v-"+kv[0]), testTTL, kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	// a2 moves to another session: sa's set still names it, and revoking sa
	// must neither delete nor report it.
	if err := s.Save(ctx, "a2", []byte("v-a2"), testTTL, "sb"); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeSession(ctx, "sa"); err != nil {
		t.Fatal(err)
	}
	if got, want := log.sorted(), []string{"revoked a1 v-a1"}; !slices.Equal(got, want) {
		t.Fatalf("reported %q, want %q", got, want)
	}
}

// Two servers revoking one session at once report each instance once: the
// script reads and deletes in one step.
func TestIntegrationEndsRevokedOnceAcrossServers(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	a, b := New(c, Options{Prefix: prefix}), New(c, Options{Prefix: prefix})
	var log endLog
	a.WatchEndsBytes(log.fn)
	b.WatchEndsBytes(log.fn)
	const n = scanCount + 9
	for i := range n {
		if err := a.Save(ctx, keyN(i), []byte("x"), testTTL, "s"); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, st := range []*Store{a, b} {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = st.RevokeSession(ctx, "s") }()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatal(errs)
	}
	got := log.sorted()
	if len(got) != n || len(slices.Compact(slices.Clone(got))) != n {
		t.Fatalf("each of %d instances must be reported once; got %d reports", n, len(got))
	}
}

// A real TTL expiry: the key is gone and nothing is reported, which is the
// documented limit, not a missed report.
func TestIntegrationExpiryIsNotReported(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	s := New(c, Options{Prefix: prefix})
	var log endLog
	s.WatchEndsBytes(log.fn)
	const short = 50 * time.Millisecond
	if err := s.Save(ctx, "k", []byte("v"), short, "s"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		n, err := c.Exists(ctx, s.stateKey("k")).Result()
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Redis did not expire the key")
		}
		time.Sleep(short)
	}
	if got := log.sorted(); len(got) != 0 {
		t.Fatalf("Redis cannot see expiry; nothing may be reported: %q", got)
	}
}

// Through brick.Bytes the engine gets the decoded instance.
func TestIntegrationEndsDecodedByBytes(t *testing.T) {
	c, prefix := integrationClient(t)
	ctx := context.Background()
	st := brick.Bytes(New(c, Options{Prefix: prefix}), brick.BytesOptions{TTL: testTTL})
	var got []*brick.Instance
	var reasons []brick.EndReason
	if r := st.WatchEnds(func(_ context.Context, inst *brick.Instance, r brick.EndReason) {
		got, reasons = append(got, inst), append(reasons, r)
	}); !slices.Equal(r, []brick.EndReason{brick.EndRevoked}) {
		t.Fatalf("Bytes must pass Redis's reasons through: %v", r)
	}
	h, err := st.Put(ctx, statetest.Sample("sess"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeSession(ctx, "sess"); err != nil {
		t.Fatal(err)
	}
	want := statetest.Sample("sess")
	if len(got) != 1 || reasons[0] != brick.EndRevoked || got[0].Handle != h || got[0].Name != want.Name || got[0].ID != want.ID {
		t.Fatalf("the hook must get the decoded instance: %+v %v", got, reasons)
	}
}

func keyN(i int) string { return "k" + string(rune('a'+i/26)) + string(rune('a'+i%26)) }
