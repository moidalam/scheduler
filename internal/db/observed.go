package db

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/moidalam/scheduler/internal/entity"
	"github.com/moidalam/scheduler/internal/observability"
)

const tracerName = "github.com/moidalam/scheduler/internal/db"

// ObservedStore wraps Store with one span and one metric per operation. It is
// the repository layer's observation point, so Store itself stays free of
// telemetry. Unexpected database errors are logged here, at their origin.
type ObservedStore struct {
	next   *Store
	ops    *observability.OpTracker
	tracer trace.Tracer
	log    *slog.Logger
}

// NewObservedStore wraps next. Call it after observability.Setup.
func NewObservedStore(next *Store, log *slog.Logger) (*ObservedStore, error) {
	ops, err := observability.NewOpTracker("repository", "postgres")
	if err != nil {
		return nil, err
	}
	return &ObservedStore{next: next, ops: ops, tracer: otel.Tracer(tracerName), log: log}, nil
}

func observe[T any](o *ObservedStore, ctx context.Context, op string, fn func(context.Context) (T, error)) (T, error) {
	ctx, span := o.tracer.Start(ctx, "db."+op)
	scope := o.ops.Start(ctx, op)
	v, err := fn(ctx)
	switch {
	case err == nil:
		scope.SetResult(observability.ResultSuccess)
	case errors.Is(err, entity.ErrNotFound):
		scope.SetResult(observability.ResultNotFound)
	case errors.Is(err, entity.ErrSlotTaken), errors.Is(err, entity.ErrDuplicateIdempotencyKey):
		scope.SetResult(observability.ResultConflict)
	default:
		scope.SetResult(observability.ResultError)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		attrs := []any{slog.String("op", op), slog.Any("error", err)}
		if class := sqlstateClass(err); class != "" {
			scope.SetTag("sqlstate", class)
			attrs = append(attrs, slog.String("sqlstate", class))
		}
		o.log.ErrorContext(ctx, "database operation failed", attrs...)
		err = observability.MarkLogged(err)
	}
	scope.End()
	span.End()
	return v, err
}

// sqlstateClass names the Postgres failure in a bounded set, for the metric
// tag and the log line. It returns "" for errors that did not come from Postgres.
func sqlstateClass(err error) string {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return ""
	}
	switch pgErr.Code {
	case "23P01":
		return "exclusion_violation"
	case "23505":
		return "unique_violation"
	case "23503":
		return "foreign_key_violation"
	case "40P01":
		return "deadlock"
	case "40001":
		return "serialization_failure"
	case "57014":
		return "statement_timeout"
	default:
		return "other"
	}
}

func (o *ObservedStore) GetResource(ctx context.Context, tenantID, id int64) (entity.Resource, error) {
	return observe(o, ctx, "GetResource", func(ctx context.Context) (entity.Resource, error) {
		return o.next.GetResource(ctx, tenantID, id)
	})
}

func (o *ObservedStore) GetService(ctx context.Context, tenantID, id int64) (entity.Service, error) {
	return observe(o, ctx, "GetService", func(ctx context.Context) (entity.Service, error) {
		return o.next.GetService(ctx, tenantID, id)
	})
}

func (o *ObservedStore) RequesterExists(ctx context.Context, tenantID, id int64) (bool, error) {
	return observe(o, ctx, "RequesterExists", func(ctx context.Context) (bool, error) {
		return o.next.RequesterExists(ctx, tenantID, id)
	})
}

func (o *ObservedStore) ListAvailabilityRules(ctx context.Context, tenantID, resourceID int64) ([]entity.AvailabilityRule, error) {
	return observe(o, ctx, "ListAvailabilityRules", func(ctx context.Context) ([]entity.AvailabilityRule, error) {
		return o.next.ListAvailabilityRules(ctx, tenantID, resourceID)
	})
}

func (o *ObservedStore) ListConfirmedBookingsInRange(ctx context.Context, tenantID, resourceID int64, from, to time.Time) ([]entity.Interval, error) {
	return observe(o, ctx, "ListConfirmedBookingsInRange", func(ctx context.Context) ([]entity.Interval, error) {
		return o.next.ListConfirmedBookingsInRange(ctx, tenantID, resourceID, from, to)
	})
}

func (o *ObservedStore) GetBooking(ctx context.Context, tenantID, id int64) (entity.Booking, error) {
	return observe(o, ctx, "GetBooking", func(ctx context.Context) (entity.Booking, error) {
		return o.next.GetBooking(ctx, tenantID, id)
	})
}

func (o *ObservedStore) GetBookingByIdempotencyKey(ctx context.Context, tenantID int64, key string) (entity.Booking, error) {
	return observe(o, ctx, "GetBookingByIdempotencyKey", func(ctx context.Context) (entity.Booking, error) {
		return o.next.GetBookingByIdempotencyKey(ctx, tenantID, key)
	})
}

func (o *ObservedStore) ListBookings(ctx context.Context, tenantID int64, f entity.BookingFilter) ([]entity.Booking, error) {
	return observe(o, ctx, "ListBookings", func(ctx context.Context) ([]entity.Booking, error) {
		return o.next.ListBookings(ctx, tenantID, f)
	})
}

func (o *ObservedStore) CreateBooking(ctx context.Context, tenantID int64, nb entity.NewBooking) (entity.Booking, error) {
	return observe(o, ctx, "CreateBooking", func(ctx context.Context) (entity.Booking, error) {
		return o.next.CreateBooking(ctx, tenantID, nb)
	})
}

func (o *ObservedStore) GetRateLimitConfig(ctx context.Context, tenantID int64, scope string) (int, error) {
	return observe(o, ctx, "GetRateLimitConfig", func(ctx context.Context) (int, error) {
		return o.next.GetRateLimitConfig(ctx, tenantID, scope)
	})
}
