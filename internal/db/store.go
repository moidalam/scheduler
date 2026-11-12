package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "github.com/moidalam/scheduler/internal/db/gen"
	"github.com/moidalam/scheduler/internal/entity"
)

// Store is the Postgres implementation of the persistence contract the
// controllers declare. Everything storage-specific lives here: the connection
// pool, transactions, the advisory lock, SQLSTATE handling, the sqlc models
// and the mapping to package entity. Nothing above this package imports pgx or
// the generated code.
type Store struct {
	pool *pgxpool.Pool
	q    dbgen.Querier
}

// NewStore returns a Store over pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: dbgen.New(pool)}
}

// Ping reports whether the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Close releases the connection pool.
func (s *Store) Close() { s.pool.Close() }

// GetTenant returns the tenant, or entity.ErrNotFound.
func (s *Store) GetTenant(ctx context.Context, id int64) (entity.Tenant, error) {
	t, err := s.q.GetTenant(ctx, id)
	if err != nil {
		return entity.Tenant{}, wrap("get tenant", err)
	}
	return entity.Tenant{ID: t.ID, Name: t.Name, Status: t.Status}, nil
}

func (s *Store) GetResource(ctx context.Context, tenantID, id int64) (entity.Resource, error) {
	r, err := s.q.GetResource(ctx, dbgen.GetResourceParams{TenantID: tenantID, ID: id})
	if err != nil {
		return entity.Resource{}, wrap("get resource", err)
	}
	return entity.Resource{
		ID: r.ID, Type: r.Type, DisplayName: r.DisplayName, Timezone: r.Timezone,
		DefaultServiceID: r.DefaultServiceID, Active: r.Active,
	}, nil
}

func (s *Store) GetService(ctx context.Context, tenantID, id int64) (entity.Service, error) {
	v, err := s.q.GetService(ctx, dbgen.GetServiceParams{TenantID: tenantID, ID: id})
	if err != nil {
		return entity.Service{}, wrap("get service", err)
	}
	return entity.Service{
		ID: v.ID, Name: v.Name,
		DurationMinutes: int(v.DurationMinutes), GranularityMinutes: int(v.GranularityMinutes),
		MinNoticeMinutes: int(v.MinNoticeMinutes), MaxHorizonDays: int(v.MaxHorizonDays),
	}, nil
}

func (s *Store) RequesterExists(ctx context.Context, tenantID, id int64) (bool, error) {
	ok, err := s.q.RequesterExists(ctx, dbgen.RequesterExistsParams{TenantID: tenantID, ID: id})
	if err != nil {
		return false, wrap("check requester", err)
	}
	return ok, nil
}

func (s *Store) ListAvailabilityRules(ctx context.Context, tenantID, resourceID int64) ([]entity.AvailabilityRule, error) {
	rows, err := s.q.ListAvailabilityRules(ctx, dbgen.ListAvailabilityRulesParams{TenantID: tenantID, ResourceID: resourceID})
	if err != nil {
		return nil, wrap("list availability rules", err)
	}
	out := make([]entity.AvailabilityRule, len(rows))
	for i, r := range rows {
		out[i] = entity.AvailabilityRule{
			Weekday: time.Weekday(r.Weekday), StartMinute: int(r.StartMinute), EndMinute: int(r.EndMinute),
			EffectiveFrom: r.EffectiveFrom, EffectiveTo: r.EffectiveTo,
		}
	}
	return out, nil
}

func (s *Store) ListConfirmedBookingsInRange(ctx context.Context, tenantID, resourceID int64, from, to time.Time) ([]entity.Interval, error) {
	rows, err := s.q.ListConfirmedBookingsInRange(ctx, dbgen.ListConfirmedBookingsInRangeParams{
		TenantID: tenantID, ResourceID: resourceID, RangeStart: from, RangeEnd: to,
	})
	if err != nil {
		return nil, wrap("list bookings in range", err)
	}
	out := make([]entity.Interval, len(rows))
	for i, r := range rows {
		out[i] = entity.Interval{Start: r.StartsAt, End: r.EndsAt}
	}
	return out, nil
}

func (s *Store) GetBooking(ctx context.Context, tenantID, id int64) (entity.Booking, error) {
	b, err := s.q.GetBooking(ctx, dbgen.GetBookingParams{TenantID: tenantID, ID: id})
	if err != nil {
		return entity.Booking{}, wrap("get booking", err)
	}
	return bookingFrom(b)
}

func (s *Store) GetBookingByIdempotencyKey(ctx context.Context, tenantID int64, key string) (entity.Booking, error) {
	b, err := s.q.GetBookingByIdempotencyKey(ctx, dbgen.GetBookingByIdempotencyKeyParams{TenantID: tenantID, IdempotencyKey: &key})
	if err != nil {
		return entity.Booking{}, wrap("get booking by idempotency key", err)
	}
	return bookingFrom(b)
}

func (s *Store) ListBookings(ctx context.Context, tenantID int64, f entity.BookingFilter) ([]entity.Booking, error) {
	rows, err := s.q.ListResourceBookings(ctx, dbgen.ListResourceBookingsParams{
		TenantID: tenantID, ResourceID: f.ResourceID,
		Status: f.Status, FromTs: f.From, ToTs: f.To,
		AfterStartsAt: f.AfterStartsAt, AfterID: f.AfterID,
		RowLimit: int32(f.Limit),
	})
	if err != nil {
		return nil, wrap("list bookings", err)
	}
	return bookingsFrom(rows)
}

// CreateBooking inserts the booking and its audit event in one transaction.
func (s *Store) CreateBooking(ctx context.Context, tenantID int64, nb entity.NewBooking) (entity.Booking, error) {
	md := nb.Metadata
	if md == nil {
		md = map[string]any{}
	}
	metadata, err := json.Marshal(md)
	if err != nil {
		return entity.Booking{}, fmt.Errorf("marshal booking metadata: %w", err)
	}

	var created dbgen.SchedulerBooking
	err = s.withTx(ctx, func(q dbgen.Querier) error {
		// Queue behind any other booking for this resource. Without this, racing
		// inserts can deadlock on the exclusion constraint instead of failing
		// cleanly (ADR-0004). The constraint still guards correctness.
		if err := q.LockResourceBookings(ctx, dbgen.LockResourceBookingsParams{TenantID: tenantID, ResourceID: nb.ResourceID}); err != nil {
			return err
		}
		// Holding the lock, nothing can slip in between this check and the insert.
		taken, err := q.ListConfirmedBookingsInRange(ctx, dbgen.ListConfirmedBookingsInRangeParams{
			TenantID: tenantID, ResourceID: nb.ResourceID, RangeStart: nb.StartsAt, RangeEnd: nb.EndsAt,
		})
		if err != nil {
			return err
		}
		if len(taken) > 0 {
			return entity.ErrSlotTaken
		}
		created, err = q.InsertBooking(ctx, dbgen.InsertBookingParams{
			TenantID: tenantID, ResourceID: nb.ResourceID, RequesterID: nb.RequesterID, ServiceID: nb.ServiceID,
			StartsAt: nb.StartsAt, EndsAt: nb.EndsAt, MeetingUrl: nb.MeetingURL,
			IdempotencyKey: nb.IdempotencyKey, Metadata: metadata,
		})
		if err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{"starts_at": created.StartsAt, "ends_at": created.EndsAt})
		if err != nil {
			return err
		}
		return q.InsertBookingEvent(ctx, dbgen.InsertBookingEventParams{
			TenantID: tenantID, BookingID: created.ID, Actor: "api", Action: "created", Payload: payload,
		})
	})
	switch {
	case err == nil:
		return bookingFrom(created)
	case errors.Is(err, entity.ErrSlotTaken):
		return entity.Booking{}, err
	case IsExclusionViolation(err):
		// Defence in depth: the lock should make this unreachable, but the
		// constraint is the authority, so a violation still means "taken".
		return entity.Booking{}, entity.ErrSlotTaken
	case nb.IdempotencyKey != nil && IsUniqueViolation(err):
		return entity.Booking{}, entity.ErrDuplicateIdempotencyKey
	default:
		return entity.Booking{}, fmt.Errorf("create booking: %w", err)
	}
}

// withTx runs fn in one transaction, rolling back if it returns an error.
func (s *Store) withTx(ctx context.Context, fn func(q dbgen.Querier) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(dbgen.New(tx))
	})
}

// wrap turns the driver's "no rows" into entity.ErrNotFound and annotates
// everything else.
func wrap(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ErrNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}

func bookingsFrom(rows []dbgen.SchedulerBooking) ([]entity.Booking, error) {
	out := make([]entity.Booking, len(rows))
	for i, r := range rows {
		b, err := bookingFrom(r)
		if err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}

func bookingFrom(b dbgen.SchedulerBooking) (entity.Booking, error) {
	var md map[string]any
	if len(b.Metadata) > 0 {
		if err := json.Unmarshal(b.Metadata, &md); err != nil {
			return entity.Booking{}, fmt.Errorf("decode metadata of booking %d: %w", b.ID, err)
		}
	}
	return entity.Booking{
		ID: b.ID, ResourceID: b.ResourceID, RequesterID: b.RequesterID, ServiceID: b.ServiceID,
		StartsAt: b.StartsAt, EndsAt: b.EndsAt, Status: b.Status,
		MeetingURL: b.MeetingUrl, IdempotencyKey: b.IdempotencyKey, Metadata: md,
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
	}, nil
}
