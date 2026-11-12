// Package controllers orchestrates the use cases: it loads data through the
// Store, applies the pure rules in package schedule, and persists changes
// through the Store. It knows nothing about HTTP or about the database;
// failures come back as *Error and the handler layer renders them.
package controllers

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/moidalam/scheduler/internal/entity"
	"github.com/moidalam/scheduler/internal/schedule"
)

// Controller implements the scheduling use cases.
type Controller struct {
	store Store
	now   func() time.Time
	log   *slog.Logger
}

// New builds a Controller. now is injectable so tests control the clock.
func New(store Store, now func() time.Time, log *slog.Logger) *Controller {
	return &Controller{store: store, now: now, log: log}
}

// ListAvailability returns the bookable slots for a resource in [from, to).
func (c *Controller) ListAvailability(ctx context.Context, tenantID, resourceID int64, serviceID *int64, from, to time.Time) (*Availability, error) {
	res, err := c.resource(ctx, tenantID, resourceID)
	if err != nil {
		return nil, err
	}
	svc, err := c.serviceFor(ctx, tenantID, res, serviceID)
	if err != nil {
		return nil, err
	}
	policy := policyOf(svc)
	if v := schedule.ValidateWindow(from, to, policy); v != nil {
		return nil, badRequest(v.Code, v.Detail)
	}

	out := &Availability{ResourceID: res.ID, ServiceID: svc.ID, Timezone: res.Timezone, Slots: []schedule.Slot{}}
	if !res.Active {
		return out, nil // an inactive resource offers nothing, which is not an error
	}

	loc, err := time.LoadLocation(res.Timezone)
	if err != nil {
		return nil, fmt.Errorf("resource %d has an invalid timezone %q: %w", res.ID, res.Timezone, err)
	}
	rules, err := c.rules(ctx, tenantID, resourceID)
	if err != nil {
		return nil, err
	}
	busy, err := c.busy(ctx, tenantID, resourceID, from, to)
	if err != nil {
		return nil, err
	}

	slots := schedule.Subtract(schedule.GenerateSlots(loc, rules, policy, from, to, c.now()), busy)
	if slots == nil {
		slots = []schedule.Slot{}
	}
	out.Slots = slots
	return out, nil
}

// CreateBooking books a slot. replay is true when idempotencyKey matched an
// earlier identical request and the original booking is returned unchanged.
func (c *Controller) CreateBooking(ctx context.Context, tenantID int64, in CreateBookingInput, idempotencyKey *string) (b entity.Booking, replay bool, err error) {
	if idempotencyKey != nil {
		if *idempotencyKey == "" || len(*idempotencyKey) > 255 {
			return b, false, badRequest("invalid_idempotency_key", "Idempotency-Key must be 1 to 255 characters")
		}
		if prior, ok, err := c.priorBooking(ctx, tenantID, *idempotencyKey, in); err != nil {
			return b, false, err
		} else if ok {
			return prior, true, nil
		}
	}

	res, err := c.resource(ctx, tenantID, in.ResourceID)
	if err != nil {
		return b, false, err
	}
	if !res.Active {
		return b, false, unprocessable("resource_inactive", "this resource is not accepting bookings")
	}
	svc, err := c.serviceFor(ctx, tenantID, res, in.ServiceID)
	if err != nil {
		return b, false, err
	}
	if exists, err := c.store.RequesterExists(ctx, tenantID, in.RequesterID); err != nil {
		return b, false, fmt.Errorf("check requester: %w", err)
	} else if !exists {
		return b, false, notFound("requester_not_found", "no such requester")
	}

	loc, err := time.LoadLocation(res.Timezone)
	if err != nil {
		return b, false, fmt.Errorf("resource %d has an invalid timezone %q: %w", res.ID, res.Timezone, err)
	}
	rules, err := c.rules(ctx, tenantID, in.ResourceID)
	if err != nil {
		return b, false, err
	}
	if v := schedule.ValidateBooking(loc, rules, policyOf(svc), in.StartsAt, in.EndsAt, c.now()); v != nil {
		return b, false, unprocessable(v.Code, v.Detail)
	}

	// The pre-check gives the common conflict a clean 409 without an error
	// path. The store re-checks atomically and the database constraint remains
	// the authority (ADR-0004).
	busy, err := c.busy(ctx, tenantID, in.ResourceID, in.StartsAt, in.EndsAt)
	if err != nil {
		return b, false, err
	}
	if len(busy) > 0 {
		return b, false, slotUnavailable()
	}

	created, err := c.store.CreateBooking(ctx, tenantID, entity.NewBooking{
		ResourceID: in.ResourceID, RequesterID: in.RequesterID, ServiceID: svc.ID,
		StartsAt: in.StartsAt, EndsAt: in.EndsAt, MeetingURL: in.MeetingURL,
		IdempotencyKey: idempotencyKey, Metadata: in.Metadata,
	})
	switch {
	case err == nil:
		return created, false, nil
	case errors.Is(err, entity.ErrSlotTaken):
		return b, false, slotUnavailable()
	case errors.Is(err, entity.ErrDuplicateIdempotencyKey) && idempotencyKey != nil:
		// A concurrent request with the same key won; return its booking.
		if prior, ok, perr := c.priorBooking(ctx, tenantID, *idempotencyKey, in); perr != nil {
			return b, false, perr
		} else if ok {
			return prior, true, nil
		}
		return b, false, fmt.Errorf("create booking: %w", err)
	default:
		return b, false, fmt.Errorf("create booking: %w", err)
	}
}

// GetBooking fetches one booking.
func (c *Controller) GetBooking(ctx context.Context, tenantID, bookingID int64) (entity.Booking, error) {
	b, err := c.store.GetBooking(ctx, tenantID, bookingID)
	if errors.Is(err, entity.ErrNotFound) {
		return b, notFound("booking_not_found", "no such booking")
	}
	if err != nil {
		return b, fmt.Errorf("get booking: %w", err)
	}
	return b, nil
}

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// ListBookings returns a resource's bookings in ascending start order.
func (c *Controller) ListBookings(ctx context.Context, tenantID int64, in ListBookingsInput) (*BookingPage, error) {
	limit := in.Limit
	if limit == 0 {
		limit = defaultPageSize
	}
	if limit < 1 || limit > maxPageSize {
		return nil, badRequest("invalid_limit", fmt.Sprintf("limit must be between 1 and %d", maxPageSize))
	}
	if _, err := c.resource(ctx, tenantID, in.ResourceID); err != nil {
		return nil, err
	}

	filter := entity.BookingFilter{
		ResourceID: in.ResourceID, Status: in.Status, From: in.From, To: in.To,
		Limit: limit + 1, // one extra row tells us whether another page exists
	}
	if in.Cursor != nil {
		at, id, err := decodeCursor(*in.Cursor)
		if err != nil {
			return nil, badRequest("invalid_cursor", "cursor is not valid")
		}
		filter.AfterStartsAt, filter.AfterID = &at, &id
	}

	rows, err := c.store.ListBookings(ctx, tenantID, filter)
	if err != nil {
		return nil, fmt.Errorf("list bookings: %w", err)
	}
	page := &BookingPage{Bookings: rows}
	if len(rows) > limit {
		page.Bookings = rows[:limit]
		last := page.Bookings[limit-1]
		cur := encodeCursor(last.StartsAt, last.ID)
		page.NextCursor = &cur
	}
	if page.Bookings == nil {
		page.Bookings = []entity.Booking{}
	}
	return page, nil
}

// --- helpers ---

func (c *Controller) resource(ctx context.Context, tenantID, id int64) (entity.Resource, error) {
	r, err := c.store.GetResource(ctx, tenantID, id)
	if errors.Is(err, entity.ErrNotFound) {
		return r, notFound("resource_not_found", "no such resource")
	}
	if err != nil {
		return r, fmt.Errorf("get resource: %w", err)
	}
	return r, nil
}

// serviceFor resolves the requested service, or the resource's default.
func (c *Controller) serviceFor(ctx context.Context, tenantID int64, res entity.Resource, requested *int64) (entity.Service, error) {
	id := requested
	if id == nil {
		id = res.DefaultServiceID
	}
	if id == nil {
		return entity.Service{}, unprocessable("service_required", "this resource has no default service; pass service_id")
	}
	s, err := c.store.GetService(ctx, tenantID, *id)
	if errors.Is(err, entity.ErrNotFound) {
		return s, notFound("service_not_found", "no such service")
	}
	if err != nil {
		return s, fmt.Errorf("get service: %w", err)
	}
	return s, nil
}

func (c *Controller) rules(ctx context.Context, tenantID, resourceID int64) ([]schedule.Rule, error) {
	rows, err := c.store.ListAvailabilityRules(ctx, tenantID, resourceID)
	if err != nil {
		return nil, fmt.Errorf("list availability rules: %w", err)
	}
	out := make([]schedule.Rule, len(rows))
	for i, r := range rows {
		out[i] = schedule.Rule{
			Weekday: r.Weekday, StartMinute: r.StartMinute, EndMinute: r.EndMinute,
			EffectiveFrom: r.EffectiveFrom, EffectiveTo: r.EffectiveTo,
		}
	}
	return out, nil
}

func (c *Controller) busy(ctx context.Context, tenantID, resourceID int64, from, to time.Time) ([]schedule.Slot, error) {
	rows, err := c.store.ListConfirmedBookingsInRange(ctx, tenantID, resourceID, from, to)
	if err != nil {
		return nil, fmt.Errorf("list bookings in range: %w", err)
	}
	out := make([]schedule.Slot, len(rows))
	for i, r := range rows {
		out[i] = schedule.Slot{Start: r.Start, End: r.End}
	}
	return out, nil
}

// priorBooking returns the booking created under key, if any. A key reused
// with a different request is a client error, not a replay.
func (c *Controller) priorBooking(ctx context.Context, tenantID int64, key string, in CreateBookingInput) (entity.Booking, bool, error) {
	prior, err := c.store.GetBookingByIdempotencyKey(ctx, tenantID, key)
	if errors.Is(err, entity.ErrNotFound) {
		return prior, false, nil
	}
	if err != nil {
		return prior, false, fmt.Errorf("get booking by idempotency key: %w", err)
	}
	if prior.ResourceID != in.ResourceID || prior.RequesterID != in.RequesterID ||
		!prior.StartsAt.Equal(in.StartsAt) || !prior.EndsAt.Equal(in.EndsAt) {
		return prior, false, unprocessable("idempotency_key_reuse", "this Idempotency-Key was already used for a different request")
	}
	return prior, true, nil
}

func policyOf(s entity.Service) schedule.Policy {
	return schedule.Policy{
		DurationMinutes:    s.DurationMinutes,
		GranularityMinutes: s.GranularityMinutes,
		MinNoticeMinutes:   s.MinNoticeMinutes,
		MaxHorizonDays:     s.MaxHorizonDays,
	}
}

func slotUnavailable() *Error {
	return conflict("slot_unavailable", "another booking already occupies this interval")
}

// The cursor is opaque to clients: base64("<RFC3339Nano starts_at>|<id>").
func encodeCursor(startsAt time.Time, id int64) string {
	raw := startsAt.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (time.Time, int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, 0, err
	}
	ts, idStr, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, 0, errors.New("malformed cursor")
	}
	at, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, 0, err
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return time.Time{}, 0, err
	}
	return at, id, nil
}
