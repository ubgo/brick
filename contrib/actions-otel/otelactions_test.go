package otelactions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/ubgo/brick"
)

// recorder wires the middleware to an in-memory span recorder and a
// manual metric reader: no network, no global provider.
type recorder struct {
	spans  *tracetest.SpanRecorder
	tp     *sdktrace.TracerProvider
	reader *sdkmetric.ManualReader
	mw     brick.ActionMiddleware
}

func newRecorder(t *testing.T, opts ...Option) *recorder {
	t.Helper()
	r := &recorder{spans: tracetest.NewSpanRecorder(), reader: sdkmetric.NewManualReader()}
	r.tp = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(r.spans))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r.reader))
	r.mw = Middleware(r.tp, append([]Option{WithMeterProvider(mp)}, opts...)...)
	return r
}

func (r *recorder) only(t *testing.T) sdktrace.ReadOnlySpan {
	t.Helper()
	ended := r.spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("want 1 span, got %d", len(ended))
	}
	return ended[0]
}

func (r *recorder) metrics(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	return rm
}

func attrs(kvs []attribute.KeyValue) map[attribute.Key]attribute.Value {
	m := map[attribute.Key]attribute.Value{}
	for _, kv := range kvs {
		m[kv.Key] = kv.Value
	}
	return m
}

func call() *brick.ActionCall {
	return &brick.ActionCall{Component: "Counter", Path: "components/Counter.html", Store: "default", Action: "save"}
}

func okNext(context.Context, *brick.ActionCall) (*brick.ActionResult, error) {
	return &brick.ActionResult{HTML: []byte("<p>ok</p>"), Version: 2}, nil
}

func TestSpanAndMetricsOnSuccess(t *testing.T) {
	t.Parallel()
	r := newRecorder(t)
	if _, err := r.mw(okNext)(context.Background(), call()); err != nil {
		t.Fatal(err)
	}
	s := r.only(t)
	if s.Name() != "Counter.save" || s.SpanKind() != trace.SpanKindServer || s.Status().Code != codes.Unset {
		t.Fatalf("span %q kind %v status %v", s.Name(), s.SpanKind(), s.Status())
	}
	a := attrs(s.Attributes())
	if a[AttrComponent].AsString() != "Counter" || a[AttrComponentPath].AsString() != "components/Counter.html" ||
		a[AttrAction].AsString() != "save" || a[AttrStore].AsString() != "default" || a[AttrStatus].AsInt64() != http.StatusOK {
		t.Fatalf("attributes %v", a)
	}
	if _, ok := a[AttrRenderless]; ok {
		t.Fatal("renderless set on a rendering action")
	}

	rm := r.metrics(t)
	var sawHist, sawCount bool
	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name != ScopeName {
			t.Fatalf("scope %q", sm.Scope.Name)
		}
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Histogram[float64]:
				if m.Name != MetricDuration || m.Unit != "s" || len(d.DataPoints) != 1 || d.DataPoints[0].Count != 1 {
					t.Fatalf("histogram %+v", m)
				}
				sawHist = true
			case metricdata.Sum[int64]:
				if m.Name != MetricCount || len(d.DataPoints) != 1 || d.DataPoints[0].Value != 1 {
					t.Fatalf("count %+v", m)
				}
				pa := d.DataPoints[0].Attributes
				if v, _ := pa.Value(AttrStatus); v.AsInt64() != http.StatusOK {
					t.Fatalf("count attributes %v", pa.ToSlice())
				}
				if _, ok := pa.Value(AttrComponentPath); ok {
					t.Fatal("the path is span-only, never a metric dimension")
				}
				sawCount = true
			}
		}
	}
	if !sawHist || !sawCount {
		t.Fatalf("metrics missing: histogram %v count %v", sawHist, sawCount)
	}
}

// Under a request span (otelhttp) the action is a child step, not a
// second server span.
func TestChildOfRequestSpan(t *testing.T) {
	t.Parallel()
	r := newRecorder(t)
	ctx, parent := r.tp.Tracer("host").Start(context.Background(), "POST /_brick/action")
	if _, err := r.mw(okNext)(ctx, call()); err != nil {
		t.Fatal(err)
	}
	parent.End()
	var child sdktrace.ReadOnlySpan
	for _, s := range r.spans.Ended() {
		if s.Name() == "Counter.save" {
			child = s
		}
	}
	if child == nil || child.SpanKind() != trace.SpanKindInternal || child.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("child %v", child)
	}
}

func TestFailureStatuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want int
	}{
		{&brick.ActionError{Status: http.StatusTooManyRequests, Message: "slow down"}, 429},
		{&brick.ActionError{Status: 200}, 403},
		{fmt.Errorf("load: %w", brick.ErrStoreUnavailable), 503},
		{brick.ErrGone, 410},
		{brick.ErrForbidden, 403},
		{brick.ErrUnknownAction, 400},
		{brick.ErrUploadTooLarge, 413},
		{brick.ErrUploadType, 415},
		{brick.ErrBadValue, 422},
		{errors.New("script failed"), 422},
		{context.Canceled, StatusClientClosed},
	}
	for _, tc := range cases {
		r := newRecorder(t)
		fail := func(context.Context, *brick.ActionCall) (*brick.ActionResult, error) { return nil, tc.err }
		if _, err := r.mw(fail)(context.Background(), call()); !errors.Is(err, tc.err) {
			t.Fatalf("the error must pass through unchanged: %v", err)
		}
		s := r.only(t)
		if got := attrs(s.Attributes())[AttrStatus].AsInt64(); got != int64(tc.want) {
			t.Errorf("%v: status %d, want %d", tc.err, got, tc.want)
		}
		if s.Status().Code != codes.Error || len(s.Events()) != 1 || s.Events()[0].Name != "exception" {
			t.Errorf("%v: span status %v, events %v", tc.err, s.Status(), s.Events())
		}
	}
}

func TestWithStatus(t *testing.T) {
	t.Parallel()
	r := newRecorder(t, WithStatus(func(error) int { return 418 }))
	fail := func(context.Context, *brick.ActionCall) (*brick.ActionResult, error) { return nil, brick.ErrForbidden }
	_, _ = r.mw(fail)(context.Background(), call())
	if got := attrs(r.only(t).Attributes())[AttrStatus].AsInt64(); got != 418 {
		t.Fatalf("status %d", got)
	}
}

// An action name the engine refused never becomes a span name or a series.
func TestUnknownActionCollapses(t *testing.T) {
	t.Parallel()
	r := newRecorder(t)
	fail := func(context.Context, *brick.ActionCall) (*brick.ActionResult, error) {
		return nil, fmt.Errorf("%w: zzz-%d", brick.ErrUnknownAction, 1)
	}
	c := call()
	c.Action = "zzz-client-invented"
	_, _ = r.mw(fail)(context.Background(), c)
	s := r.only(t)
	if s.Name() != "Counter."+UnknownAction || attrs(s.Attributes())[AttrAction].AsString() != UnknownAction {
		t.Fatalf("span %q %v", s.Name(), s.Attributes())
	}
	for _, sm := range r.metrics(t).ScopeMetrics {
		for _, m := range sm.Metrics {
			if d, ok := m.Data.(metricdata.Sum[int64]); ok {
				if v, _ := d.DataPoints[0].Attributes.Value(AttrAction); v.AsString() != UnknownAction {
					t.Fatalf("metric action %q", v.AsString())
				}
			}
		}
	}
}

// fakeStream records what reached the transport.
type fakeStream struct{ got []string }

func (f *fakeStream) Write(ref, text string, _ bool) error {
	f.got = append(f.got, ref+":"+text)
	return nil
}

func TestRenderlessAndStreamed(t *testing.T) {
	t.Parallel()
	r := newRecorder(t)
	next := func(ctx context.Context, _ *brick.ActionCall) (*brick.ActionResult, error) {
		s := brick.StreamFrom(ctx)
		_ = s.Write("out", "hel", false)
		_ = s.Write("out", "lo", false)
		return &brick.ActionResult{SkipRender: true, Handle: "h", Token: "t"}, nil
	}
	fs := &fakeStream{}
	if _, err := r.mw(next)(brick.WithActionStream(context.Background(), fs), call()); err != nil {
		t.Fatal(err)
	}
	a := attrs(r.only(t).Attributes())
	if !a[AttrRenderless].AsBool() || a[AttrStreamChunks].AsInt64() != 2 {
		t.Fatalf("attributes %v", a)
	}
	if strings.Join(fs.got, ",") != "out:hel,out:lo" {
		t.Fatalf("the transport's stream must still get every chunk: %v", fs.got)
	}
}

// secret marks every value that is user data or a secret. BV: add
// AttrStore.String(a.Session) (or any field below) to the span and this
// fails.
const secret = "S3CRET"

func TestNoUserDataInTelemetry(t *testing.T) {
	t.Parallel()
	r := newRecorder(t)
	c := call()
	c.Session = secret + "-session"
	c.Args = []any{secret + "-arg", map[string]any{"name": secret + "-file"}}
	c.State = map[string]any{"password": secret + "-state"}
	c.Sets = []brick.FieldSet{{Path: "email", Value: secret + "-set"}}
	next := func(ctx context.Context, _ *brick.ActionCall) (*brick.ActionResult, error) {
		_ = brick.StreamFrom(ctx).Write("out", secret+"-chunk", false)
		return &brick.ActionResult{HTML: []byte(secret + "-html"), SkipRender: true, Handle: secret + "-handle", Token: secret + "-token", Reply: secret + "-reply"}, nil
	}
	if _, err := r.mw(next)(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	allowed := map[attribute.Key]bool{AttrComponent: true, AttrComponentPath: true, AttrAction: true, AttrStore: true, AttrStatus: true, AttrRenderless: true, AttrStreamChunks: true}
	s := r.only(t)
	kvs := append([]attribute.KeyValue{attribute.String("span.name", s.Name())}, s.Attributes()...)
	for _, e := range s.Events() {
		kvs = append(kvs, e.Attributes...)
	}
	for _, kv := range s.Attributes() {
		if !allowed[kv.Key] {
			t.Errorf("unexpected span attribute %s", kv.Key)
		}
	}
	for _, sm := range r.metrics(t).ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Histogram[float64]:
				for _, p := range d.DataPoints {
					kvs = append(kvs, p.Attributes.ToSlice()...)
				}
			case metricdata.Sum[int64]:
				for _, p := range d.DataPoints {
					kvs = append(kvs, p.Attributes.ToSlice()...)
				}
			}
		}
	}
	for _, kv := range kvs {
		if strings.Contains(kv.Value.String(), secret) {
			t.Errorf("%s carries user data: %q", kv.Key, kv.Value.String())
		}
	}
}

// The real engine: an ordinary action and an upload ($upload) both pass
// through the chain and both get a span.
const uploader = `---
state:
  note: ""
  avatar: null
brick:
  uploads:
    avatar: {accept: [image/png], max_size: 1KB}
---
<div><i>{{ .State.note }}</i><input type="file" brick:upload="avatar"></div>`

var handleRE = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

func handleOf(t *testing.T, page []byte) brick.ActionRequest {
	t.Helper()
	m := handleRE.FindSubmatch(page)
	if m == nil {
		t.Fatalf("no handle in %s", page)
	}
	return brick.ActionRequest{Handle: string(m[1]), Token: string(m[2])}
}

type memSink struct{}

type memWriter struct {
	m   brick.UploadMeta
	buf bytes.Buffer
}

func (memSink) Open(_ context.Context, m brick.UploadMeta) (brick.UploadWriter, error) {
	return &memWriter{m: m}, nil
}
func (w *memWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *memWriter) Close() (brick.UploadRef, error) {
	return brick.UploadRef{ID: "f1", Name: w.m.Name, Type: w.m.Type, Size: int64(w.buf.Len())}, nil
}
func (w *memWriter) Abort(error) error { return nil }

func TestEngineActionsAndUploads(t *testing.T) {
	t.Parallel()
	r := newRecorder(t)
	e, err := brick.New(brick.Options{
		FS: fstest.MapFS{
			"components/Uploader.html": {Data: []byte(uploader)},
			"content/a.html":           {Data: []byte(`<Uploader />`)},
		},
		States:           brick.NewMemoryStates(0, 0),
		Uploads:          memSink{},
		ActionMiddleware: []brick.ActionMiddleware{r.mw},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	req := handleOf(t, page)
	req.Action, req.Args = "$set", []any{"note", "hi"}
	html, err := e.Action(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 24))
	if _, err := e.RunUpload(ctx, handleOf(t, html), brick.UploadFile{Field: "avatar", Name: "me.png", Type: "image/png", Size: int64(len(png)), Body: bytes.NewReader(png)}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range r.spans.Ended() {
		a := attrs(s.Attributes())
		if a[AttrStatus].AsInt64() != http.StatusOK || a[AttrComponentPath].AsString() == "" || a[AttrStore].AsString() == "" {
			t.Fatalf("%s attributes %v", s.Name(), a)
		}
		names = append(names, s.Name())
	}
	if strings.Join(names, ",") != "Uploader.$set,Uploader.$upload" {
		t.Fatalf("spans %v", names)
	}
}
