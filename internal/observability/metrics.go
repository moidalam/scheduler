package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Result is the bounded outcome label on every operation. Keep this set small:
// each value is a metric series. Specific failures go in the error_code tag.
type Result string

const (
	ResultSuccess  Result = "success"
	ResultRejected Result = "rejected"  // expected caller error, 4xx other than the two below
	ResultNotFound Result = "not_found" // expected, 404
	ResultConflict Result = "conflict"  // expected, 409: slot taken, duplicate key
	ResultError    Result = "error"     // our failure, 5xx or unexpected
)

const meterName = "github.com/moidalam/scheduler/internal/observability"

// durationBucketsMS are in milliseconds, sized for an API whose responses take
// tens to hundreds of milliseconds. Percentile precision is set by these edges.
var durationBucketsMS = []float64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000, 10000}

// OpTracker emits operation.total (counter) and operation.duration_ms
// (histogram), labelled with the layer, component and op. One tracker is made
// per (layer, component), and every layer uses the same metric names, so one
// dashboard template covers all of them.
type OpTracker struct {
	total    metric.Int64Counter
	duration metric.Float64Histogram
	base     []attribute.KeyValue
}

// NewOpTracker registers the instruments on the global meter provider.
func NewOpTracker(layer, component string) (*OpTracker, error) {
	meter := otel.Meter(meterName)
	total, err := meter.Int64Counter("operation.total",
		metric.WithDescription("Operations, by layer, component, op and result"))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("operation.duration",
		metric.WithDescription("Operation duration in milliseconds"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(durationBucketsMS...))
	if err != nil {
		return nil, err
	}
	return &OpTracker{
		total:    total,
		duration: duration,
		base: []attribute.KeyValue{
			attribute.String("layer", layer),
			attribute.String("component", component),
		},
	}, nil
}

// Start opens a scope for one operation. Its result defaults to error until
// the code sets one, so a missed exit path counts as a failure.
func (t *OpTracker) Start(ctx context.Context, op string) *OpScope {
	return &OpScope{t: t, ctx: ctx, op: op, start: time.Now(), result: ResultError}
}

// OpScope is one in-flight operation. Not safe for concurrent use.
type OpScope struct {
	t      *OpTracker
	ctx    context.Context
	op     string
	start  time.Time
	result Result
	tags   []attribute.KeyValue
}

// SetResult sets the outcome.
func (s *OpScope) SetResult(r Result) { s.result = r }

// SetTag adds a bounded tag. Never pass ids, tokens or free text here.
func (s *OpScope) SetTag(key, value string) {
	s.tags = append(s.tags, attribute.String(key, value))
}

// End emits the count and the duration. Call it with defer.
func (s *OpScope) End() {
	s.t.record(s.ctx, s.op, s.result, time.Since(s.start), s.tags...)
}

// Record emits one operation without a scope, for the edge, which learns the
// op and status only after the request has run.
func (t *OpTracker) Record(ctx context.Context, op string, result Result, d time.Duration, tags ...attribute.KeyValue) {
	t.record(ctx, op, result, d, tags...)
}

func (t *OpTracker) record(ctx context.Context, op string, result Result, d time.Duration, tags ...attribute.KeyValue) {
	opAttr := attribute.String("op", op)
	counterAttrs := make([]attribute.KeyValue, 0, len(t.base)+2+len(tags))
	counterAttrs = append(counterAttrs, t.base...)
	counterAttrs = append(counterAttrs, opAttr, attribute.String("result", string(result)))
	counterAttrs = append(counterAttrs, tags...)
	t.total.Add(ctx, 1, metric.WithAttributes(counterAttrs...))

	durAttrs := make([]attribute.KeyValue, 0, len(t.base)+1+len(tags))
	durAttrs = append(durAttrs, t.base...)
	durAttrs = append(durAttrs, opAttr)
	durAttrs = append(durAttrs, tags...)
	t.duration.Record(ctx, float64(d.Microseconds())/1000, metric.WithAttributes(durAttrs...))
}
