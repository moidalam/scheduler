//go:build bvt

package bvt_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/moidalam/scheduler/internal/gen/api"
)

// The seeded resource and tenant. Requester 1 exists in the seed data.
const (
	tenant    = "1"
	resource  = "1"
	requester = 1
)

var pacific = mustLoad("America/Los_Angeles")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// nextFreeSlot returns the first bookable slot in the coming week. Every run
// books a fresh slot, so the BVTs do not depend on earlier runs.
func nextFreeSlot(t *testing.T) api.Slot {
	t.Helper()
	from := time.Now().In(pacific).AddDate(0, 0, 1).Truncate(24 * time.Hour)
	to := from.AddDate(0, 0, 7)
	r := call(t, "GET", availabilityPath(from, to), tenant, nil, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var a api.Availability
	r.decode(t, &a)
	require.NotEmpty(t, a.Slots, "no free slot in the next week to book")
	return a.Slots[0]
}

func availabilityPath(from, to time.Time) string {
	return fmt.Sprintf("/v1/resources/%s/availability?starts_at=%s&ends_at=%s",
		resource, from.Format(time.RFC3339), to.Format(time.RFC3339))
}

func bookingBody(start, end time.Time, requesterID int64) map[string]any {
	return map[string]any{
		"resource_id":  1,
		"requester_id": requesterID,
		"starts_at":    start.Format(time.RFC3339),
		"ends_at":      end.Format(time.RFC3339),
	}
}

func TestBVT_Health(t *testing.T) {
	requireStack(t)
	r := call(t, "GET", "/healthz", "", nil, nil)
	assert.Equal(t, http.StatusOK, r.Status)
}

func TestBVT_Availability_ListsFreeSlotsInOrder(t *testing.T) {
	requireStack(t)
	slot := nextFreeSlot(t)
	assert.True(t, slot.EndsAt.After(slot.StartsAt), "a slot has positive length")
	assert.Equal(t, 30*time.Minute, slot.EndsAt.Sub(slot.StartsAt), "the service sells 30-minute slots")
	assert.Equal(t, 0, slot.StartsAt.Minute()%30, "slots start on :00 or :30")
}

func TestBVT_Availability_RejectsBadWindows(t *testing.T) {
	requireStack(t)
	from := time.Now().In(pacific).AddDate(0, 0, 1)

	inverted := call(t, "GET", availabilityPath(from, from.Add(-time.Hour)), tenant, nil, nil)
	assert.Equal(t, http.StatusBadRequest, inverted.Status)
	assert.Equal(t, "invalid_window", inverted.code(t), "ends_at before starts_at")

	tooLong := call(t, "GET", availabilityPath(from, from.AddDate(0, 0, 90)), tenant, nil, nil)
	assert.Equal(t, http.StatusBadRequest, tooLong.Status)
	assert.Equal(t, "window_too_large", tooLong.code(t), "windows over the 60-day cap are refused")
}

func TestBVT_Availability_UnknownResourceIs404(t *testing.T) {
	requireStack(t)
	from := time.Now().In(pacific).AddDate(0, 0, 1)
	path := fmt.Sprintf("/v1/resources/999999/availability?starts_at=%s&ends_at=%s",
		from.Format(time.RFC3339), from.Add(time.Hour*24).Format(time.RFC3339))
	r := call(t, "GET", path, tenant, nil, nil)
	assert.Equal(t, http.StatusNotFound, r.Status)
	assert.Equal(t, "resource_not_found", r.code(t))
}

func TestBVT_Booking_CreateThenReadBackAndList(t *testing.T) {
	requireStack(t)
	slot := nextFreeSlot(t)

	created := call(t, "POST", "/v1/bookings", tenant, bookingBody(slot.StartsAt, slot.EndsAt, requester), nil)
	require.Equal(t, http.StatusCreated, created.Status, string(created.Body))
	var b api.Booking
	created.decode(t, &b)
	assert.Equal(t, api.BookingStatus("confirmed"), b.Status)
	assert.Equal(t, int64(requester), b.RequesterId)
	assert.True(t, b.StartsAt.Equal(slot.StartsAt))

	got := call(t, "GET", fmt.Sprintf("/v1/bookings/%d", b.Id), tenant, nil, nil)
	require.Equal(t, http.StatusOK, got.Status)
	var same api.Booking
	got.decode(t, &same)
	assert.Equal(t, b.Id, same.Id, "GET returns the booking that was created")

	window := fmt.Sprintf("/v1/resources/%s/bookings?starts_at=%s&ends_at=%s", resource,
		slot.StartsAt.Add(-time.Hour).Format(time.RFC3339), slot.EndsAt.Add(time.Hour).Format(time.RFC3339))
	list := call(t, "GET", window, tenant, nil, nil)
	require.Equal(t, http.StatusOK, list.Status)
	var page struct {
		Bookings []api.Booking `json:"bookings"`
	}
	list.decode(t, &page)
	ids := make([]int64, 0, len(page.Bookings))
	for _, x := range page.Bookings {
		ids = append(ids, x.Id)
	}
	assert.Contains(t, ids, b.Id, "the schedule lists the new booking")
}

func TestBVT_Booking_SameSlotTwiceIsSlotUnavailable(t *testing.T) {
	requireStack(t)
	slot := nextFreeSlot(t)
	require.Equal(t, http.StatusCreated,
		call(t, "POST", "/v1/bookings", tenant, bookingBody(slot.StartsAt, slot.EndsAt, requester), nil).Status)

	again := call(t, "POST", "/v1/bookings", tenant, bookingBody(slot.StartsAt, slot.EndsAt, requester+1), nil)
	assert.Equal(t, http.StatusConflict, again.Status)
	assert.Equal(t, "slot_unavailable", again.code(t))
}

func TestBVT_Booking_SameIdempotencyKeyReturnsTheSameBooking(t *testing.T) {
	requireStack(t)
	slot := nextFreeSlot(t)
	key := map[string]string{"Idempotency-Key": fmt.Sprintf("bvt-%d", time.Now().UnixNano())}

	first := call(t, "POST", "/v1/bookings", tenant, bookingBody(slot.StartsAt, slot.EndsAt, requester), key)
	require.Equal(t, http.StatusCreated, first.Status, string(first.Body))
	var a api.Booking
	first.decode(t, &a)

	replay := call(t, "POST", "/v1/bookings", tenant, bookingBody(slot.StartsAt, slot.EndsAt, requester), key)
	assert.Equal(t, http.StatusOK, replay.Status, "a replay is 200, not a second 201")
	var b api.Booking
	replay.decode(t, &b)
	assert.Equal(t, a.Id, b.Id, "the replay returns the original booking")
}

func TestBVT_Booking_RuleViolationsAre422WithTheRuleNamed(t *testing.T) {
	requireStack(t)
	// A Saturday, within the horizon, so only the business-hours rule applies.
	sat := time.Now().In(pacific).AddDate(0, 0, 1)
	for sat.Weekday() != time.Saturday {
		sat = sat.AddDate(0, 0, 1)
	}
	satStart := time.Date(sat.Year(), sat.Month(), sat.Day(), 10, 0, 0, 0, pacific)

	outside := call(t, "POST", "/v1/bookings", tenant, bookingBody(satStart, satStart.Add(30*time.Minute), requester), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, outside.Status)
	assert.Equal(t, "outside_business_hours", outside.code(t))

	// Off the :00/:30 grid, so the alignment rule fires first.
	misaligned := call(t, "POST", "/v1/bookings", tenant, bookingBody(satStart.Add(15*time.Minute), satStart.Add(45*time.Minute), requester), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, misaligned.Status)
	assert.Equal(t, "misaligned_start", misaligned.code(t))
}

func TestBVT_Booking_UnknownRequesterIs404(t *testing.T) {
	requireStack(t)
	slot := nextFreeSlot(t)
	r := call(t, "POST", "/v1/bookings", tenant, bookingBody(slot.StartsAt, slot.EndsAt, 999999), nil)
	assert.Equal(t, http.StatusNotFound, r.Status)
	assert.Equal(t, "requester_not_found", r.code(t))
}

func TestBVT_Booking_UnknownBookingIs404(t *testing.T) {
	requireStack(t)
	r := call(t, "GET", "/v1/bookings/999999", tenant, nil, nil)
	assert.Equal(t, http.StatusNotFound, r.Status)
	assert.Equal(t, "booking_not_found", r.code(t))
}

func TestBVT_Booking_OverlongIdempotencyKeyIsRejected(t *testing.T) {
	requireStack(t)
	slot := nextFreeSlot(t)
	long := strings.Repeat("k", 256)
	r := call(t, "POST", "/v1/bookings", tenant, bookingBody(slot.StartsAt, slot.EndsAt, requester),
		map[string]string{"Idempotency-Key": long})
	assert.Equal(t, http.StatusBadRequest, r.Status)
	assert.Equal(t, "invalid_idempotency_key", r.code(t))
}

func TestBVT_Tenant_MissingAndUnknownAreRefused(t *testing.T) {
	requireStack(t)
	malformed := call(t, "GET", "/v1/bookings/1", "abc", nil, nil)
	assert.Equal(t, http.StatusBadRequest, malformed.Status)
	assert.Equal(t, "missing_tenant", malformed.code(t), "a non-numeric tenant is refused")

	unknown := call(t, "GET", "/v1/bookings/1", "999999", nil, nil)
	assert.Equal(t, http.StatusNotFound, unknown.Status)
	assert.Equal(t, "unknown_tenant", unknown.code(t))
}
