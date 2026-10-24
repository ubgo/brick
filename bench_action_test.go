package brick

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Benchmarks for the per-request path: island Fill and the action
// endpoint. Template execution is out of scope (the engine renders a page
// once per file save), so these exist to watch the overhead
// around it: state encode and decode, sealing, store round trips, the JSON
// envelope and allocations. Run them with
//
//	testlock go test -run x -bench . -benchmem -count 5

// benchItems is the list a typical instance keeps: a page of rows, by id
// and a few short fields, as a list component holds them.
func benchItems() []any {
	items := make([]any, 10)
	for i := range items {
		items[i] = map[string]any{"id": int64(i + 1), "title": fmt.Sprintf("item %d", i+1), "done": i%2 == 0, "score": 1.5 * float64(i)}
	}
	return items
}

// benchCounter is a stateful component with a typical state: a counter,
// a query, a flag and a page of rows. Its template is deliberately small.
const benchCounter = "---\nstate:\n  count: 0\n  q: \"\"\n  open: false\n  items: []\nprops:\n  step: {type: int, default: 1}\n---\n<brick:script>bench</brick:script>\n<div class=\"c\"><span>{{ .State.count }}</span><button brick:click=\"bump\">+</button><i>{{ len .State.items }}</i></div>"

func benchScripting() ctxScripting {
	state := func(a []any) map[string]any { st, _ := a[0].(map[string]any); return st }
	return ctxScripting{"bench": {
		"bump": func(_ context.Context, a ...any) (any, error) {
			st := state(a)
			n, _ := toInt(st["count"])
			st["count"] = n + 1
			if items, _ := st["items"].([]any); len(items) == 0 {
				st["items"] = benchItems()
			}
			return st, nil
		},
		"suggest": func(ctx context.Context, a ...any) (any, error) {
			eff := EffectsFrom(ctx)
			if err := eff.SkipRender(); err != nil {
				return nil, err
			}
			if err := eff.Reply([]any{"ada", "bob"}); err != nil {
				return nil, err
			}
			st := state(a)
			n, _ := toInt(st["count"])
			st["count"] = n + 1
			return st, nil
		},
	}}
}

func benchEngine(b *testing.B, store StateStore, islands int) *Engine {
	b.Helper()
	page := strings.Repeat("<Counter step=\"2\" />\n", islands)
	e, err := New(Options{States: store, Scripting: benchScripting(), FS: theme(
		"components/Counter.html", benchCounter,
		"content/a.html", page,
	)})
	if err != nil {
		b.Fatal(err)
	}
	return e
}

func benchSealed(b *testing.B) *SealedStates {
	b.Helper()
	ring, err := StaticKeys(testKey(1))
	if err != nil {
		b.Fatal(err)
	}
	s, err := NewSealedStates(SealedOptions{Sealer: NewAESGCMSealer(ring, nil), TTL: time.Hour})
	if err != nil {
		b.Fatal(err)
	}
	return s
}

func benchStores(b *testing.B) map[string]func() StateStore {
	return map[string]func() StateStore{
		"memory": func() StateStore { return NewMemoryStates(0, 0) },
		"sealed": func() StateStore { return benchSealed(b) },
	}
}

var benchStateRE = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

// BenchmarkFill is a page view: Fill renders every island and saves its
// instance. The memory store is bounded by evictions, not by b.N.
func BenchmarkFill(b *testing.B) {
	for _, n := range []int{1, 20} {
		for _, name := range []string{"memory", "sealed"} {
			b.Run(fmt.Sprintf("%s/islands=%d", name, n), func(b *testing.B) {
				var store StateStore = NewMemoryStates(1000, time.Hour)
				if name == "sealed" {
					store = benchSealed(b)
				}
				e := benchEngine(b, store, n)
				ctx := context.Background()
				r, err := e.RenderChunks(ctx, "content/a.html", nil)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, err := e.Fill(ctx, r); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// benchHandle fills one page and returns its first instance's handle and
// token, after one bump so the state holds the rows.
func benchHandle(b *testing.B, e *Engine) (string, string) {
	b.Helper()
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/a.html", nil)
	if err != nil {
		b.Fatal(err)
	}
	page, err := e.Fill(ctx, r)
	if err != nil {
		b.Fatal(err)
	}
	m := benchStateRE.FindStringSubmatch(string(page))
	if m == nil {
		b.Fatalf("no state in %s", page)
	}
	return m[1], m[2]
}

// benchPost runs one action through ActionHandler with the envelope Accept
// and returns the answer.
func benchPost(h http.Handler, handle, token, action string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"handle":%q,"token":%q,"action":%q,"args":[]}`, handle, token, action)
	req := httptest.NewRequest(http.MethodPost, "/_brick/action", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	req.Header.Set("Accept", EffectsContentType)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// BenchmarkAction is one action round trip through the HTTP handler: load
// and check the instance, run the action, save it and re-render. A sealed
// store hands back a new blob each time, so the loop follows it.
func BenchmarkAction(b *testing.B) {
	for name, mk := range benchStores(b) {
		b.Run(name, func(b *testing.B) {
			e := benchEngine(b, mk(), 1)
			h := e.ActionHandler()
			handle, token := benchHandle(b, e)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				rec := benchPost(h, handle, token, "bump")
				if rec.Code != http.StatusOK {
					b.Fatalf("%d %s", rec.Code, rec.Body)
				}
				if name == "sealed" {
					var env struct {
						HTML string `json:"html"`
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
						b.Fatal(err)
					}
					m := benchStateRE.FindStringSubmatch(env.HTML)
					if m == nil {
						b.Fatalf("no state in %s", env.HTML)
					}
					handle = m[1]
				}
			}
		})
	}
}

// BenchmarkActionRenderless is a renderless action (brick.skip_render with
// a reply): no re-render, the answer carries the new handle.
func BenchmarkActionRenderless(b *testing.B) {
	for name, mk := range benchStores(b) {
		b.Run(name, func(b *testing.B) {
			e := benchEngine(b, mk(), 1)
			h := e.ActionHandler()
			handle, token := benchHandle(b, e)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				rec := benchPost(h, handle, token, "suggest")
				if rec.Code != http.StatusOK {
					b.Fatalf("%d %s", rec.Code, rec.Body)
				}
				var env struct {
					Handle string `json:"handle"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Handle == "" {
					b.Fatalf("%v %s", err, rec.Body)
				}
				handle = env.Handle
			}
		})
	}
}

// benchInstance is a typical stored instance.
func benchInstance() *Instance {
	return &Instance{
		Handle: "h", Token: strings.Repeat("ab", 16), Store: "default",
		Path: "components/Counter.html", Name: "Counter", ID: "c1",
		Props:   map[string]any{"step": int64(2), "label": "Add"},
		Ctx:     map[string]any{"theme": "dark"},
		Locale:  "en",
		State:   map[string]any{"count": int64(7), "q": "search text", "open": true, "items": benchItems()},
		Keys:    []string{"count", "items", "open", "q"},
		Session: strings.Repeat("0f", 32), Version: 3,
		Issued:   time.Unix(1_700_000_000, 0).UTC(),
		PagePath: "content/a.html",
	}
}

// BenchmarkCodec is the JSON codec's encode and decode of a typical
// instance, the work every sealed or bytes-store round trip does twice.
func BenchmarkCodec(b *testing.B) {
	inst := benchInstance()
	enc, err := EncodeInstance(inst)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := EncodeInstance(inst); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := DecodeInstance(enc); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkSealedStore is SealedStates Put and Get alone: codec, AEAD and
// base64, without the engine around them.
func BenchmarkSealedStore(b *testing.B) {
	s := benchSealed(b)
	inst := benchInstance()
	ctx := WithStateScope(context.Background(), StateScope{Store: "default", Session: inst.Session})
	handle, err := s.Put(ctx, inst)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("put", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := s.Put(ctx, inst); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("get", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, ok, err := s.Get(ctx, handle); err != nil || !ok {
				b.Fatal(ok, err)
			}
		}
	})
}
