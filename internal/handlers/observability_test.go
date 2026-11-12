//go:build integration

package handlers_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/moidalam/scheduler/internal/handlers"
)

// parseLogLines decodes every JSON line the stack wrote.
func parseLogLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &m), sc.Text())
		out = append(out, m)
	}
	return out
}

// findLine returns the one line with this message, failing if there is none.
func findLine(t *testing.T, lines []map[string]any, msg string) map[string]any {
	t.Helper()
	for _, l := range lines {
		if l["msg"] == msg {
			return l
		}
	}
	require.Failf(t, "missing log line", "no line with msg %q in:\n%v", msg, lines)
	return nil
}

// A rejected booking is the case that matters: the controller decides it, and
// the edge reports it. Both lines must carry the same request id and trace id,
// and the caller must be able to read the trace id from the response.
func TestOneRequestSharesOneTraceAcrossLayers(t *testing.T) {
	s := newStack(t)
	require.Equal(t, http.StatusCreated, s.do(t, "POST", "/v1/bookings", "1", bookingBody(1, nineAM, nineThirty), nil).Status)
	s.logs.Reset()

	r := s.do(t, "POST", "/v1/bookings", "1", bookingBody(2, nineAM, nineThirty), nil)
	require.Equal(t, http.StatusConflict, r.Status)

	lines := parseLogLines(t, s.logs.String())
	edge := findLine(t, lines, "request")
	rejected := findLine(t, lines, "request rejected")

	assert.Equal(t, float64(409), edge["status"])
	assert.Equal(t, "slot_unavailable", edge["error_code"])
	assert.Equal(t, "CreateBooking", edge["op"])
	assert.Equal(t, "CreateBooking", rejected["op"])
	assert.Equal(t, "slot_unavailable", rejected["error_code"])
	assert.Equal(t, "INFO", rejected["level"], "a 409 is an expected outcome, not an error")

	traceID := edge["trace_id"]
	require.NotEmpty(t, traceID, "the edge line carries a trace id")
	assert.Equal(t, traceID, rejected["trace_id"], "the controller line is in the same trace")
	assert.Equal(t, edge["request_id"], rejected["request_id"], "and the same request")
	assert.Equal(t, float64(1), edge["tenant_id"])

	assert.Equal(t, traceID, r.Header.Get(handlers.TraceIDHeader), "the response names the trace")
	assert.NotEmpty(t, r.Header.Get(handlers.RequestIDHeader))
	assert.Equal(t, edge["request_id"], r.Header.Get(handlers.RequestIDHeader))
}

// Each layer's failure is logged once: the edge line says the request failed,
// and the layer that decided it has already written its own line.
func TestNotFoundIsInfoAtEveryLayer(t *testing.T) {
	s := newStack(t)
	s.logs.Reset()

	r := s.do(t, "GET", "/v1/bookings/999999", "1", nil, nil)
	require.Equal(t, http.StatusNotFound, r.Status)

	lines := parseLogLines(t, s.logs.String())
	assert.Equal(t, "INFO", findLine(t, lines, "request")["level"])
	assert.Equal(t, "INFO", findLine(t, lines, "request rejected")["level"])
	for _, l := range lines {
		assert.NotEqual(t, "ERROR", l["level"], "an expected 404 must not reach the error stream: %v", l)
	}
}

// A database failure is logged once at its origin, and the edge reports the
// same request as a 500 under the same trace. Closing the pool makes every
// query fail, and the tenant lookup is the first query a request makes.
func TestDatabaseFailureIsLoggedOnceAtItsOriginAndAtTheEdge(t *testing.T) {
	s := newStack(t)
	s.pool.Close()
	s.logs.Reset()

	r := s.do(t, "GET", "/v1/bookings/1", "1", nil, nil)
	require.Equal(t, http.StatusInternalServerError, r.Status)
	assert.Equal(t, "internal_error", r.code(t))

	lines := parseLogLines(t, s.logs.String())
	origin := findLine(t, lines, "tenant lookup failed")
	edge := findLine(t, lines, "request")

	assert.Equal(t, "ERROR", origin["level"])
	assert.Equal(t, "ERROR", edge["level"])
	assert.Equal(t, float64(500), edge["status"])
	assert.Equal(t, "internal_error", edge["error_code"])
	assert.Equal(t, edge["trace_id"], origin["trace_id"], "origin and edge share a trace")
	assert.Equal(t, edge["request_id"], origin["request_id"], "origin and edge share a request")

	count := 0
	for _, l := range lines {
		if l["level"] == "ERROR" {
			count++
		}
	}
	assert.Equal(t, 2, count, "one line at the origin, one at the edge, nothing else: %v", lines)
}

// A failure inside a query is logged by the store, the layer that made the
// query, and the controller must not log it again. Renaming the bookings table
// lets the tenant lookup succeed and then fails the booking query.
func TestStoreFailureIsLoggedAtTheStoreNotTheController(t *testing.T) {
	s := newStack(t)
	_, err := s.pool.Exec(context.Background(), `ALTER TABLE scheduler.bookings RENAME TO bookings_unavailable`)
	require.NoError(t, err)
	s.logs.Reset()

	r := s.do(t, "GET", "/v1/bookings/1", "1", nil, nil)
	require.Equal(t, http.StatusInternalServerError, r.Status)

	lines := parseLogLines(t, s.logs.String())
	origin := findLine(t, lines, "database operation failed")
	assert.Equal(t, "GetBooking", origin["op"])
	assert.Equal(t, "other", origin["sqlstate"])
	assert.Equal(t, "ERROR", origin["level"])

	edge := findLine(t, lines, "request")
	assert.Equal(t, float64(500), edge["status"])
	assert.Equal(t, origin["trace_id"], edge["trace_id"])

	errors := 0
	for _, l := range lines {
		if l["level"] == "ERROR" {
			errors++
		}
	}
	assert.Equal(t, 2, errors, "the store line and the edge line, and the controller stays quiet: %v", lines)
}
