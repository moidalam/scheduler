//go:build bvt

package bvt_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Metric selectors for the three layers, as the collector stores them.
const (
	edgeOK        = `operation_total{layer="handler",op="CreateBooking",result="success"}`
	edgeConflict  = `operation_total{layer="handler",op="CreateBooking",result="conflict"}`
	edgeNotFound  = `operation_total{layer="handler",op="GetBooking",result="not_found"}`
	edgeErrors    = `operation_total{layer="handler",result="error"}`
	createErrors  = `operation_total{layer="handler",op="CreateBooking",result="error"}`
	controllerOK  = `operation_total{layer="controller",op="CreateBooking",result="success"}`
	storeOK       = `operation_total{layer="repository",component="postgres",op="CreateBooking",result="success"}`
	edgeDurations = `operation_duration_milliseconds_count{layer="handler",op="CreateBooking"}`
)

// A booking that succeeds is visible in every layer: the response names a
// trace, the trace holds a span per layer, the logs record the outcome, and the
// metrics count it at each layer.
func TestBVT_Telemetry_SuccessfulBookingIsVisibleInEveryLayer(t *testing.T) {
	requireStack(t)
	slot := nextFreeSlot(t)

	r := call(t, "POST", "/v1/bookings", tenant, bookingBody(slot.StartsAt, slot.EndsAt, requester), nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	traceID, requestID := r.Header.Get("X-Trace-Id"), r.Header.Get("X-Request-Id")
	require.NotEmpty(t, traceID, "the response names its trace")
	require.NotEmpty(t, requestID, "the response names its request")

	// Logs: the edge writes one summary line, and it carries the same ids.
	edge := findLog(t, logsForTrace(t, traceID), "request")
	assert.Equal(t, "INFO", edge.Stream["severity_text"])
	assert.Equal(t, "201", edge.Stream["status"])
	assert.Equal(t, "CreateBooking", edge.Stream["op"])
	assert.Equal(t, requestID, edge.Stream["request_id"])
	assert.Equal(t, tenant, edge.Stream["tenant_id"])

	// Traces: the edge, the controller and the database are all in one trace.
	spans := spansForTrace(t, traceID)
	assert.True(t, contains(spans, "controller.CreateBooking"), "controller span, got %v", spans)
	assert.True(t, contains(spans, "db.CreateBooking"), "database span, got %v", spans)

	// Metrics: each layer counts the operation, and the edge records its duration.
	waitForMetric(t, edgeOK)
	waitForMetric(t, controllerOK)
	waitForMetric(t, storeOK)
	waitForMetric(t, edgeDurations)
}

// A slot taken by someone else is an expected outcome. It must be a 409 that
// is logged at Info and counted as a conflict, never as an error.
func TestBVT_Telemetry_ConflictIsInfoAndNotAnError(t *testing.T) {
	requireStack(t)
	slot := nextFreeSlot(t)
	require.Equal(t, http.StatusCreated,
		call(t, "POST", "/v1/bookings", tenant, bookingBody(slot.StartsAt, slot.EndsAt, requester), nil).Status)

	r := call(t, "POST", "/v1/bookings", tenant, bookingBody(slot.StartsAt, slot.EndsAt, requester+1), nil)
	require.Equal(t, http.StatusConflict, r.Status)
	require.Equal(t, "slot_unavailable", r.code(t))

	lines := logsForTrace(t, r.Header.Get("X-Trace-Id"))
	rejected := findLog(t, lines, "request rejected")
	assert.Equal(t, "INFO", rejected.Stream["severity_text"])
	assert.Equal(t, "slot_unavailable", rejected.Stream["error_code"])
	assert.Equal(t, "409", findLog(t, lines, "request").Stream["status"])
	for _, l := range lines {
		assert.NotContains(t, []string{"ERROR", "WARN"}, l.Stream["severity_text"], "no failure line for an expected conflict: %v", l)
	}

	waitForMetric(t, edgeConflict)
	requireNoMetric(t, createErrors)
}

// A 404 is an expected outcome too. It must not appear in the error metric.
func TestBVT_Telemetry_NotFoundIsNotAnError(t *testing.T) {
	requireStack(t)
	r := call(t, "GET", fmt.Sprintf("/v1/bookings/%d", 999999), tenant, nil, nil)
	require.Equal(t, http.StatusNotFound, r.Status)

	lines := logsForTrace(t, r.Header.Get("X-Trace-Id"))
	assert.Equal(t, "INFO", findLog(t, lines, "request rejected").Stream["severity_text"])

	waitForMetric(t, edgeNotFound)
	requireNoMetric(t, edgeErrors)
}

// Every response names its request and its trace, so a caller can always be
// pointed at the telemetry. Errors such as a malformed tenant included.
func TestBVT_Telemetry_EveryResponseNamesItsRequest(t *testing.T) {
	requireStack(t)
	for _, tc := range []struct {
		name, tenant string
	}{
		{"ok", tenant},
		{"malformed tenant", "abc"},
		{"unknown tenant", "999999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := call(t, "GET", "/v1/bookings/1", tc.tenant, nil, nil)
			assert.NotEmpty(t, r.Header.Get("X-Request-Id"))
			assert.Len(t, r.Header.Get("X-Trace-Id"), 32, "a 128-bit trace id, hex")
		})
	}
}
