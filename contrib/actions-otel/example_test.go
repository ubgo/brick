package otelactions_test

import (
	"context"
	"fmt"
	"regexp"
	"testing/fstest"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/actionmw"
	otelactions "github.com/ubgo/brick/contrib/actions-otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// handleAttrs finds the handle and token a stateful component carries.
var handleAttrs = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

// counterTheme is a page with one stateful component; $set needs no
// script, so the example runs a real action through the engine.
var counterTheme = fstest.MapFS{
	"components/Counter.html": {Data: []byte("---\nstate:\n  n: 0\n---\n<p>{{ .State.n }}</p>\n")},
	"content/index.html":      {Data: []byte("<Counter />\n")},
}

// The host passes its own providers (here the SDK's in-memory recorder and
// a manual metric reader stand in for an exporter); the middleware never
// touches OpenTelemetry's globals. Every action becomes one span named
// <Component>.<action> plus a duration and a count by outcome.
func Example() {
	ctx := context.Background()
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	eng, err := brick.New(brick.Options{
		FS:     counterTheme,
		States: brick.NewMemoryStates(0, 0),
		ActionMiddleware: []brick.ActionMiddleware{
			actionmw.Recover(),
			otelactions.Middleware(tp, otelactions.WithMeterProvider(mp)),
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	html, err := eng.Render(ctx, "content/index.html", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	m := handleAttrs.FindSubmatch(html)
	if m == nil {
		fmt.Println("no stateful component")
		return
	}
	if _, err := eng.RunAction(ctx, brick.ActionRequest{Handle: string(m[1]), Token: string(m[2]), Action: "$set", Args: []any{"n", 5}}); err != nil {
		fmt.Println(err)
		return
	}

	for _, s := range spans.Ended() {
		fmt.Println("span:", s.Name())
		for _, a := range s.Attributes() {
			fmt.Printf("  %s = %s\n", a.Key, a.Value.String())
		}
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		fmt.Println(err)
		return
	}
	for _, sm := range rm.ScopeMetrics {
		for _, met := range sm.Metrics {
			fmt.Println("metric:", met.Name)
		}
	}
	// Output:
	// span: Counter.$set
	//   brick.component = Counter
	//   brick.component.path = components/Counter.html
	//   brick.action = $set
	//   brick.store = default
	//   brick.action.status = 200
	// metric: brick.action.duration
	// metric: brick.action.count
}
