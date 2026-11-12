package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"
)

func TestLevelForStatusFollowsWhoOwnsTheOutcome(t *testing.T) {
	cases := map[int]slog.Level{
		200: slog.LevelInfo,
		404: slog.LevelInfo, // expected
		409: slog.LevelInfo, // expected: slot taken
		422: slog.LevelInfo, // expected: rule violation
		400: slog.LevelWarn, // caller mistake
		429: slog.LevelWarn,
		500: slog.LevelError, // ours
		503: slog.LevelError,
	}
	for status, want := range cases {
		assert.Equal(t, want, LevelForStatus(status), "status %d", status)
	}
}

func TestContextHandlerAddsCorrelationFromTheContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&buf, nil)))

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 2, 3}, SpanID: trace.SpanID{9}, TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	ctx = WithRecord(ctx, &Record{RequestID: "req-1", Op: "CreateBooking", TenantID: 7})

	logger.InfoContext(ctx, "hello", slog.String("layer", "controller"))

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	assert.Equal(t, "req-1", line["request_id"])
	assert.Equal(t, float64(7), line["tenant_id"])
	assert.Equal(t, sc.TraceID().String(), line["trace_id"])
	assert.Equal(t, sc.SpanID().String(), line["span_id"])
	assert.Equal(t, "controller", line["layer"], "the caller's own attributes are kept")
}

func TestContextHandlerOmitsWhatTheContextDoesNotHave(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&buf, nil)))
	logger.InfoContext(context.Background(), "bare")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	for _, k := range []string{"request_id", "trace_id", "span_id", "tenant_id"} {
		assert.NotContains(t, line, k)
	}
}

func TestMarkedErrorsAreRecognisedThroughWrapping(t *testing.T) {
	base := errors.New("boom")
	assert.False(t, IsLogged(base))
	marked := MarkLogged(base)
	assert.True(t, IsLogged(marked))
	assert.True(t, IsLogged(errors.Join(errors.New("other"), marked)))
	assert.ErrorIs(t, marked, base, "marking does not hide the cause")
	assert.Nil(t, MarkLogged(nil))
}

func TestOpTrackerRecordsDurationInMillisecondsWithResult(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	tracker, err := NewOpTracker("test", "unit")
	require.NoError(t, err)
	tracker.Record(context.Background(), "CreateBooking", ResultConflict, 150*time.Millisecond)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	var sum float64
	var counted bool
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Histogram[float64]:
				if m.Name == "operation.duration" {
					sum = data.DataPoints[0].Sum
				}
			case metricdata.Sum[int64]:
				if m.Name == "operation.total" {
					counted = true
					assert.Equal(t, int64(1), data.DataPoints[0].Value)
				}
			}
		}
	}
	assert.InDelta(t, 150, sum, 0.001, "the histogram is in milliseconds")
	assert.True(t, counted, "operation.total was recorded")
}
