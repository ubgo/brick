// Package otelactions traces and measures brick actions with OpenTelemetry
// . It is one brick.ActionMiddleware: put it in
// brick.Options.ActionMiddleware like any other. The host hands it its own
// TracerProvider and, optionally, MeterProvider; this module never reads
// or sets OpenTelemetry's global providers, so two engines in one process
// can report to different backends and a test needs no global reset.
//
// What it records is deliberately narrow: which component, which action,
// which store, how it ended and how long it took. State, arguments,
// deferred field values, handles, tokens and session ids never become an
// attribute, because they are user data or secrets and a tracing backend
// is usually readable by far more people than the database.
package otelactions

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/ubgo/brick"
)

// ScopeName is the instrumentation scope every tracer and meter is
// created under, so a backend can filter brick's telemetry by library.
const ScopeName = "github.com/ubgo/brick/contrib/actions-otel"

// Attribute keys, under a brick. namespace in the style of the semantic
// conventions. These are the only keys this package ever sets on a span,
// which is what the no-user-data test pins.
const (
	// AttrComponent is the component's tag name ("Counter").
	AttrComponent = attribute.Key("brick.component")
	// AttrComponentPath is the component's file, as the engine resolved it.
	// Span only: the tag already identifies the series in metrics.
	AttrComponentPath = attribute.Key("brick.component.path")
	// AttrAction is the action name ("save", "$set", "$upload"), or
	// UnknownAction when the engine refused the name.
	AttrAction = attribute.Key("brick.action")
	// AttrStore is the name of the state store holding the instance.
	AttrStore = attribute.Key("brick.store")
	// AttrStatus is the HTTP status the action endpoint answers with
	// (200 on success), so dashboards read the same numbers as access logs.
	AttrStatus = attribute.Key("brick.action.status")
	// AttrRenderless is true when the action called brick.skip_render.
	AttrRenderless = attribute.Key("brick.action.renderless")
	// AttrStreamChunks is how many chunks the action streamed; set only when it streamed at least one.
	AttrStreamChunks = attribute.Key("brick.action.stream.chunks")
)

// Metric instrument names.
const (
	// MetricDuration is a histogram of action durations in seconds.
	MetricDuration = "brick.action.duration"
	// MetricCount counts finished actions, by component, action, store and
	// status.
	MetricCount = "brick.action.count"
)

// UnknownAction replaces an action name the engine refused (400). The
// name comes from the client, and only the token check stands between a
// client and an unbounded set of names; letting each become its own span
// name and metric series would let one signed-in client grow the
// backend's memory without limit.
const UnknownAction = "_unknown"

// StatusClientClosed is recorded when the client went away mid-action
// (context.Canceled). The endpoint writes nothing then; 499 is nginx's
// number for the same thing, so it reads naturally next to proxy logs.
const StatusClientClosed = 499

// durationBuckets are seconds, from a cached $set to a slow script or a
// streamed completion; the SDK's default buckets assume milliseconds.
var durationBuckets = []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// Option configures Middleware.
type Option func(*config)

type config struct {
	mp     metric.MeterProvider
	status func(error) int
}

// WithMeterProvider records the duration histogram and the count. Without
// it no metrics are recorded: the host chooses where they go, never this
// package.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(c *config) { c.mp = mp }
}

// WithStatus replaces the error → status mapping, for a host whose
// Options.ActionErrors answers with other statuses than brick's default.
// It is called only for a non-nil error.
func WithStatus(f func(error) int) Option {
	return func(c *config) { c.status = f }
}

// Middleware returns the tracing and metrics middleware. A nil tp records
// no spans (metrics only). Place it innermost when spans should cover only
// actions the other middleware let through, outermost to see refusals too.
func Middleware(tp trace.TracerProvider, opts ...Option) brick.ActionMiddleware {
	c := config{status: Status}
	for _, o := range opts {
		o(&c)
	}
	if tp == nil {
		tp = tracenoop.NewTracerProvider()
	}
	if c.mp == nil {
		c.mp = metricnoop.NewMeterProvider()
	}
	tracer := tp.Tracer(ScopeName)
	meter := c.mp.Meter(ScopeName)
	// An instrument error still returns a usable (no-op) instrument; it is
	// reported through OpenTelemetry's error handler, where every other
	// instrumentation library reports the same failure.
	duration, err := meter.Float64Histogram(MetricDuration, metric.WithUnit("s"),
		metric.WithDescription("Duration of brick actions."),
		metric.WithExplicitBucketBoundaries(durationBuckets...))
	if err != nil {
		otel.Handle(err)
	}
	count, err := meter.Int64Counter(MetricCount, metric.WithUnit("{action}"),
		metric.WithDescription("Finished brick actions, by outcome status."))
	if err != nil {
		otel.Handle(err)
	}
	return func(next brick.ActionFunc) brick.ActionFunc {
		return func(ctx context.Context, a *brick.ActionCall) (*brick.ActionResult, error) {
			// Under an HTTP server span (otelhttp) this is a child step;
			// with no span in the context (Engine.Action called directly)
			// it is the server-side root of the action.
			kind := trace.SpanKindServer
			if trace.SpanContextFromContext(ctx).IsValid() {
				kind = trace.SpanKindInternal
			}
			ctx, span := tracer.Start(ctx, a.Component+"."+a.Action, trace.WithSpanKind(kind),
				trace.WithAttributes(AttrComponent.String(a.Component), AttrComponentPath.String(a.Path),
					AttrAction.String(a.Action), AttrStore.String(a.Store)))
			defer span.End()

			cs := &countingStream{next: brick.StreamFrom(ctx)}
			start := time.Now()
			res, err := next(brick.WithActionStream(ctx, cs), a)
			took := time.Since(start).Seconds()

			status := http.StatusOK
			if err != nil {
				status = c.status(err)
			}
			action := a.Action
			if status == http.StatusBadRequest && errors.Is(err, brick.ErrUnknownAction) {
				action = UnknownAction
				span.SetName(a.Component + "." + action)
				span.SetAttributes(AttrAction.String(action))
			}
			span.SetAttributes(AttrStatus.Int(status))
			if res != nil && res.SkipRender {
				span.SetAttributes(AttrRenderless.Bool(true))
			}
			if n := cs.chunks(); n > 0 {
				span.SetAttributes(AttrStreamChunks.Int(n))
			}
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, http.StatusText(status))
			}

			set := metric.WithAttributeSet(attribute.NewSet(AttrComponent.String(a.Component),
				AttrAction.String(action), AttrStore.String(a.Store), AttrStatus.Int(status)))
			// The caller's context may be cancelled already; the
			// measurement must still land.
			mctx := context.WithoutCancel(ctx)
			duration.Record(mctx, took, set)
			count.Add(mctx, 1, set)
			return res, err
		}
	}
}

// Status maps an action error to the status brick's action endpoint
// answers with: brick.ActionStatus, plus StatusClientClosed for a client
// that went away, which the endpoint never gets to answer. A host with its
// own ActionErrors passes WithStatus instead.
func Status(err error) int {
	var ae *brick.ActionError
	if !errors.As(err, &ae) && errors.Is(err, context.Canceled) {
		return StatusClientClosed
	}
	return brick.ActionStatus(err)
}

// countingStream passes chunks on to the transport's stream and counts
// them. The text itself is never recorded.
type countingStream struct {
	next brick.ActionStream
	mu   sync.Mutex
	n    int
}

func (s *countingStream) Write(ref, text string, replace bool) error {
	err := s.next.Write(ref, text, replace)
	if err == nil {
		s.mu.Lock()
		s.n++
		s.mu.Unlock()
	}
	return err
}

func (s *countingStream) chunks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}
