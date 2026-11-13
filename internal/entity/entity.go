// Package entity holds the storage-agnostic domain types the controllers and
// the persistence layer exchange. It imports nothing from the database layer,
// so replacing Postgres changes package db and nothing above it.
package entity

import (
	"errors"
	"time"
)

// Errors a Store returns. Controllers match on these with errors.Is and never
// see a driver error type or a SQLSTATE.
var (
	// ErrNotFound means the row does not exist in the caller's tenant.
	ErrNotFound = errors.New("not found")
	// ErrSlotTaken means a confirmed booking already occupies part of the
	// requested interval for that resource.
	ErrSlotTaken = errors.New("slot taken")
	// ErrDuplicateIdempotencyKey means a booking already exists under the key.
	ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
)

// Tenant is the top-level scope every other record belongs to.
type Tenant struct {
	ID     int64
	Name   string
	Status string
}

// Service defines how long a booking is and how it may be placed.
type Service struct {
	ID                 int64
	Name               string
	DurationMinutes    int
	GranularityMinutes int
	MinNoticeMinutes   int
	MaxHorizonDays     int
}

// Resource is the thing that is booked, such as a trainer.
type Resource struct {
	ID               int64
	Type             string
	DisplayName      string
	Timezone         string // IANA name
	DefaultServiceID *int64
	Active           bool
}

// AvailabilityRule is one recurring weekly window in the resource's local time.
type AvailabilityRule struct {
	Weekday       time.Weekday
	StartMinute   int // minutes after local midnight, inclusive
	EndMinute     int // minutes after local midnight, exclusive
	EffectiveFrom *time.Time
	EffectiveTo   *time.Time
}

// Interval is a half-open [Start, End) span of time.
type Interval struct {
	Start, End time.Time
}

// Booking is a reserved interval on a resource.
type Booking struct {
	ID             int64
	ResourceID     int64
	RequesterID    int64
	ServiceID      int64
	StartsAt       time.Time
	EndsAt         time.Time
	Status         string
	MeetingURL     *string
	IdempotencyKey *string
	Metadata       map[string]any
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewBooking is what a Store needs to create a confirmed booking.
type NewBooking struct {
	ResourceID     int64
	RequesterID    int64
	ServiceID      int64
	StartsAt       time.Time
	EndsAt         time.Time
	MeetingURL     *string
	IdempotencyKey *string
	Metadata       map[string]any
}

// BookingFilter selects one page of a resource's bookings, in ascending
// (StartsAt, ID) order. Nil fields are not applied.
type BookingFilter struct {
	ResourceID    int64
	Status        *string
	From, To      *time.Time
	AfterStartsAt *time.Time // keyset cursor: return rows after this position
	AfterID       *int64
	Limit         int
}

// RateLimitConfig is the per-tenant rate limit configuration.
// Scope is "global" for tenant-wide limits or an API endpoint name (e.g., "create_booking").
type RateLimitConfig struct {
	TenantID          int64
	Scope             string
	RequestsPerSecond int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
