// Package observability is the telemetry seam: request correlation, log
// enrichment, metric instruments and the OpenTelemetry setup. Business code
// depends on this package only, never on an exporter.
package observability

import (
	"context"
	"log/slog"
)

// Record is the per-request correlation state. The edge middleware creates it,
// and each later layer fills in what only it knows: the tenant once it is
// resolved, the operation once it is routed, the error code once it is rendered.
// The logger reads it from the context at the moment each line is written, so
// every line for one request carries the same request id.
type Record struct {
	RequestID string
	Op        string
	TenantID  int64
	ErrorCode string
}

type recordKey struct{}

// WithRecord stores rec on ctx.
func WithRecord(ctx context.Context, rec *Record) context.Context {
	return context.WithValue(ctx, recordKey{}, rec)
}

// RecordFrom returns the request record on ctx, or nil outside a request.
func RecordFrom(ctx context.Context) *Record {
	rec, _ := ctx.Value(recordKey{}).(*Record)
	return rec
}

// SetOp names the operation this request performs. Handlers call it with the
// generated operation id, which is a bounded set.
func SetOp(ctx context.Context, op string) {
	if rec := RecordFrom(ctx); rec != nil {
		rec.Op = op
	}
}

// LevelForStatus maps an HTTP status to a log level by who owns the outcome
// (ADR-0002): our failures are Error, caller mistakes Warn, and expected
// outcomes such as not-found, conflict and rule violations are Info.
func LevelForStatus(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status == 400, status == 401, status == 403, status == 429:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
