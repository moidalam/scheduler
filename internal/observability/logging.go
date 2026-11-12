package observability

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// ContextHandler adds the correlation fields of the context to every record:
// request_id, tenant_id, and trace_id and span_id when a trace is active. The
// op is not added here, because each layer names its own operation, and a
// database line must not inherit the request's operation.
type ContextHandler struct{ inner slog.Handler }

// NewContextHandler wraps inner.
func NewContextHandler(inner slog.Handler) *ContextHandler {
	return &ContextHandler{inner: inner}
}

func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	var attrs []slog.Attr
	if rec := RecordFrom(ctx); rec != nil {
		attrs = append(attrs, slog.String("request_id", rec.RequestID))
		if rec.TenantID != 0 {
			attrs = append(attrs, slog.Int64("tenant_id", rec.TenantID))
		}
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		attrs = append(attrs,
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()))
	}
	if len(attrs) == 0 {
		return h.inner.Handle(ctx, r)
	}
	out := r.Clone()
	out.AddAttrs(attrs...)
	return h.inner.Handle(ctx, out)
}

func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{inner: h.inner.WithGroup(name)}
}

// loggedError marks an error whose origin layer has already written its log
// line. The edge uses it to avoid logging the same failure a second time.
type loggedError struct{ err error }

func (e loggedError) Error() string { return e.err.Error() }
func (e loggedError) Unwrap() error { return e.err }

// MarkLogged records that err has been logged at its origin.
func MarkLogged(err error) error {
	if err == nil {
		return nil
	}
	return loggedError{err: err}
}

// IsLogged reports whether err was marked by MarkLogged anywhere in its chain.
func IsLogged(err error) bool {
	var l loggedError
	return errors.As(err, &l)
}

// fanout sends each record to every handler, gated by one level.
type fanout struct {
	level    slog.Leveler
	handlers []slog.Handler
}

func (f *fanout) Enabled(_ context.Context, level slog.Level) bool {
	return level >= f.level.Level()
}

func (f *fanout) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range f.handlers {
		if err := h.Handle(ctx, r.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (f *fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &fanout{level: f.level, handlers: make([]slog.Handler, len(f.handlers))}
	for i, h := range f.handlers {
		next.handlers[i] = h.WithAttrs(attrs)
	}
	return next
}

func (f *fanout) WithGroup(name string) slog.Handler {
	next := &fanout{level: f.level, handlers: make([]slog.Handler, len(f.handlers))}
	for i, h := range f.handlers {
		next.handlers[i] = h.WithGroup(name)
	}
	return next
}
