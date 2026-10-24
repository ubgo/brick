package statessql

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/statetest"
)

// endLog records what a watcher was told, safe for the concurrent prunes
// below.
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

// testEnds pins the end reports: the reasons the table can honestly see,
// each expired or revoked row reported once with its bytes, a row saved
// again in time never reported, and Bytes decoding the report into the
// instance an engine hook gets.
func testEnds(t *testing.T, open backend) {
	ctx := context.Background()
	t.Run("Reasons", func(t *testing.T) {
		got := open(t, nil).WatchEndsBytes(nil)
		if !slices.Equal(got, []brick.EndReason{brick.EndExpired, brick.EndRevoked}) {
			t.Fatalf("a table sees expiry (at Prune) and revocation, never eviction: %v", got)
		}
	})
	t.Run("PruneReportsEachExpiredRowOnce", func(t *testing.T) {
		clock := newClock()
		s := open(t, clock.Now)
		var log endLog
		s.WatchEndsBytes(log.fn)
		for _, k := range []string{"a", "b", "renewed"} {
			if err := s.Save(ctx, k, []byte("v-"+k), time.Minute, ""); err != nil {
				t.Fatal(err)
			}
		}
		clock.Add(2 * time.Minute)
		if err := s.Save(ctx, "renewed", []byte("v2"), time.Minute, ""); err != nil {
			t.Fatal(err)
		}
		n, err := s.Prune(ctx, clock.Now())
		if err != nil || n != 2 {
			t.Fatalf("Prune = %d, %v; want 2", n, err)
		}
		if got, want := log.sorted(), []string{"expired a v-a", "expired b v-b"}; !slices.Equal(got, want) {
			t.Fatalf("reported %q, want %q", got, want)
		}
		if n, err := s.Prune(ctx, clock.Now()); n != 0 || err != nil || len(log.sorted()) != 2 {
			t.Fatalf("a second Prune must find and report nothing: %d %v %q", n, err, log.sorted())
		}
		if _, ok, _ := s.Load(ctx, "renewed"); !ok {
			t.Fatal("the renewed row must survive")
		}
	})
	t.Run("TwoServersPruningReportEachRowOnce", func(t *testing.T) {
		clock := newClock()
		a := open(t, clock.Now)
		b, err := New(Options{DB: a.db, Dialect: a.dialect, Table: a.table, Clock: clock.Now})
		if err != nil {
			t.Fatal(err)
		}
		var log endLog
		a.WatchEndsBytes(log.fn)
		b.WatchEndsBytes(log.fn)
		const rows = eachBatch + 7 // more than one batch
		for i := range rows {
			if err := a.Save(ctx, keyN(i), []byte("x"), time.Minute, ""); err != nil {
				t.Fatal(err)
			}
		}
		clock.Add(2 * time.Minute)
		var wg sync.WaitGroup
		var na, nb int
		var ea, eb error
		wg.Add(2)
		go func() { defer wg.Done(); na, ea = a.Prune(ctx, clock.Now()) }()
		go func() { defer wg.Done(); nb, eb = b.Prune(ctx, clock.Now()) }()
		wg.Wait()
		if ea != nil || eb != nil || na+nb != rows {
			t.Fatalf("prunes removed %d+%d (%v, %v), want %d in all", na, nb, ea, eb, rows)
		}
		got := log.sorted()
		if len(got) != rows || len(slices.Compact(got)) != rows {
			t.Fatalf("every row must be reported exactly once: %d reports, %d distinct", len(log.sorted()), len(got))
		}
	})
	t.Run("RevokeReportsTheSessionOnly", func(t *testing.T) {
		s := open(t, nil)
		var log endLog
		s.WatchEndsBytes(log.fn)
		save := func(k, sess string) {
			if err := s.Save(ctx, k, []byte("v-"+k), time.Minute, sess); err != nil {
				t.Fatal(err)
			}
		}
		save("a1", "sa")
		save("a2", "sa")
		save("b1", "sb")
		if err := s.RevokeSession(ctx, "sa"); err != nil {
			t.Fatal(err)
		}
		if got, want := log.sorted(), []string{"revoked a1 v-a1", "revoked a2 v-a2"}; !slices.Equal(got, want) {
			t.Fatalf("reported %q, want %q", got, want)
		}
		if _, ok, _ := s.Load(ctx, "b1"); !ok {
			t.Fatal("another session's row must survive")
		}
	})
	t.Run("NoWatcherKeepsTheBulkPath", func(t *testing.T) {
		clock := newClock()
		s := open(t, clock.Now)
		var log endLog
		s.WatchEndsBytes(log.fn)
		s.WatchEndsBytes(nil)
		if err := s.Save(ctx, "a", []byte("v"), time.Minute, ""); err != nil {
			t.Fatal(err)
		}
		clock.Add(2 * time.Minute)
		if n, err := s.Prune(ctx, clock.Now()); n != 1 || err != nil || len(log.sorted()) != 0 {
			t.Fatalf("a removed watcher must hear nothing: %d %v %q", n, err, log.sorted())
		}
	})
	t.Run("BytesDecodesForTheEngine", func(t *testing.T) {
		clock := newClock()
		st := brick.Bytes(open(t, clock.Now), brick.BytesOptions{TTL: time.Minute})
		var mu sync.Mutex
		var got []*brick.Instance
		reasons := st.WatchEnds(func(_ context.Context, inst *brick.Instance, r brick.EndReason) {
			mu.Lock()
			defer mu.Unlock()
			if r == brick.EndExpired {
				got = append(got, inst)
			}
		})
		if len(reasons) != 2 {
			t.Fatalf("Bytes must pass the table's reasons through: %v", reasons)
		}
		h, err := st.Put(ctx, statetest.Sample(""))
		if err != nil {
			t.Fatal(err)
		}
		clock.Add(2 * time.Minute)
		if _, err := st.Prune(ctx, clock.Now()); err != nil {
			t.Fatal(err)
		}
		want := statetest.Sample("")
		if len(got) != 1 || got[0].Handle != h || got[0].Name != want.Name || got[0].ID != want.ID {
			t.Fatalf("the hook must get the decoded instance: %+v", got)
		}
	})
}

func keyN(i int) string { return "k" + string(rune('a'+i/26)) + string(rune('a'+i%26)) }
