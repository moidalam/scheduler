package controllers

import (
	"context"
	"time"

	"github.com/moidalam/scheduler/internal/entity"
	"github.com/moidalam/scheduler/internal/schedule"
)

// Store is everything the controller needs from persistence. It is defined
// here, beside its consumer, and speaks only in package entity types and
// entity errors: no transactions, connections, SQL or driver errors cross it.
// Package db implements it for Postgres; another database would implement the
// same interface and nothing in this package would change.
//
// Every method takes the tenant explicitly (ADR-0009) and returns
// entity.ErrNotFound when a single row does not exist.
type Store interface {
	GetResource(ctx context.Context, tenantID, id int64) (entity.Resource, error)
	GetService(ctx context.Context, tenantID, id int64) (entity.Service, error)
	RequesterExists(ctx context.Context, tenantID, id int64) (bool, error)
	ListAvailabilityRules(ctx context.Context, tenantID, resourceID int64) ([]entity.AvailabilityRule, error)

	// ListConfirmedBookingsInRange returns the intervals of confirmed bookings
	// overlapping [from, to).
	ListConfirmedBookingsInRange(ctx context.Context, tenantID, resourceID int64, from, to time.Time) ([]entity.Interval, error)
	GetBooking(ctx context.Context, tenantID, id int64) (entity.Booking, error)
	GetBookingByIdempotencyKey(ctx context.Context, tenantID int64, key string) (entity.Booking, error)
	ListBookings(ctx context.Context, tenantID int64, f entity.BookingFilter) ([]entity.Booking, error)

	// CreateBooking persists a confirmed booking and its audit record
	// atomically. It returns entity.ErrSlotTaken if the interval is occupied
	// and entity.ErrDuplicateIdempotencyKey if the key was already used. How it
	// serialises concurrent writers is its own concern (ADR-0004).
	CreateBooking(ctx context.Context, tenantID int64, nb entity.NewBooking) (entity.Booking, error)
}

// Availability is the result of an availability query.
type Availability struct {
	ResourceID int64
	ServiceID  int64
	Timezone   string
	Slots      []schedule.Slot
}

// CreateBookingInput is a request to book a slot.
type CreateBookingInput struct {
	ResourceID  int64
	RequesterID int64
	ServiceID   *int64
	StartsAt    time.Time
	EndsAt      time.Time
	MeetingURL  *string
	Metadata    map[string]any
}

// ListBookingsInput filters and pages a resource's bookings.
type ListBookingsInput struct {
	ResourceID int64
	Status     *string
	From, To   *time.Time
	Limit      int
	Cursor     *string
}

// BookingPage is one page of bookings.
type BookingPage struct {
	Bookings   []entity.Booking
	NextCursor *string
}
