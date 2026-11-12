package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/moidalam/scheduler/internal/observability"
)

// RequestIDHeader and TraceIDHeader are returned to the caller so a report can
// quote an id that finds the request in the logs (request id) and the trace
// (trace id).
const (
	RequestIDHeader = "X-Request-Id"
	TraceIDHeader   = "X-Trace-Id"
)

// observeRequests is the HTTP edge. It starts the request record, returns the
// correlation ids, and after the response is written records the request
// metric and logs one summary line. Every layer below logs with the same
// context, so every line for this request shares its request and trace ids.
func observeRequests(log *slog.Logger, ops *observability.OpTracker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &observability.Record{RequestID: newRequestID()}
			ctx := observability.WithRecord(r.Context(), rec)

			w.Header().Set(RequestIDHeader, rec.RequestID)
			if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
				w.Header().Set(TraceIDHeader, sc.TraceID().String())
			}

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r.WithContext(ctx))

			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			op := rec.Op
			if op == "" {
				op = "unrouted"
			}
			elapsed := time.Since(start)

			tags := []attribute.KeyValue{attribute.String("status_family", statusFamily(status))}
			if rec.ErrorCode != "" {
				tags = append(tags, attribute.String("error_code", rec.ErrorCode))
			}
			ops.Record(ctx, op, resultForStatus(status), elapsed, tags...)

			level := observability.LevelForStatus(status)
			if r.URL.Path == "/healthz" && level == slog.LevelInfo {
				level = slog.LevelDebug // health probes are routine; keep them out of Info
			}
			attrs := []any{
				slog.String("op", op),
				slog.String("method", r.Method),
				slog.String("route", routeOf(r)),
				slog.Int("status", status),
				slog.Float64("duration_ms", float64(elapsed.Microseconds())/1000),
			}
			if rec.ErrorCode != "" {
				attrs = append(attrs, slog.String("error_code", rec.ErrorCode))
			}
			log.Log(ctx, level, "request", attrs...)
		})
	}
}

// routeOf returns the request path as the caller sent it. The metric uses the
// op name instead, so ids in the path never reach a metric label.
func routeOf(r *http.Request) string {
	return r.URL.Path
}

// newRequestID returns 16 random bytes in hex. It is not derived from the
// trace, so a caller can't predict it.
func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func statusFamily(code int) string {
	return strconv.Itoa(code/100) + "xx"
}

func resultForStatus(code int) observability.Result {
	switch {
	case code >= 200 && code < 400:
		return observability.ResultSuccess
	case code == http.StatusNotFound:
		return observability.ResultNotFound
	case code == http.StatusConflict:
		return observability.ResultConflict
	case code >= 500:
		return observability.ResultError
	default:
		return observability.ResultRejected
	}
}
