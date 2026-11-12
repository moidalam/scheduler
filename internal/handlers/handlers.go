// Package handlers is the HTTP layer. It implements the generated strict-server
// interface, translating between wire types and the controller, and owns the
// tenant middleware and error rendering.
package handlers

import (
	"context"
	"net/http"

	"github.com/moidalam/scheduler/internal/controllers"
	"github.com/moidalam/scheduler/internal/entity"
	"github.com/moidalam/scheduler/internal/gen/api"
)

// Handlers implements api.StrictServerInterface.
type Handlers struct {
	c *controllers.Controller
}

// New returns the API handlers backed by c.
func New(c *controllers.Controller) *Handlers { return &Handlers{c: c} }

var _ api.StrictServerInterface = (*Handlers)(nil)

func (h *Handlers) ListAvailability(ctx context.Context, req api.ListAvailabilityRequestObject) (api.ListAvailabilityResponseObject, error) {
	a, err := h.c.ListAvailability(ctx, TenantFrom(ctx), req.ResourceId, req.Params.ServiceId, req.Params.StartsAt, req.Params.EndsAt)
	if err != nil {
		return nil, err
	}
	slots := make([]api.Slot, len(a.Slots))
	for i, s := range a.Slots {
		slots[i] = api.Slot{StartsAt: s.Start, EndsAt: s.End}
	}
	return api.ListAvailability200JSONResponse{
		ResourceId: a.ResourceID, ServiceId: a.ServiceID, Timezone: a.Timezone, Slots: slots,
	}, nil
}

func (h *Handlers) CreateBooking(ctx context.Context, req api.CreateBookingRequestObject) (api.CreateBookingResponseObject, error) {
	if req.Body == nil {
		return nil, &controllers.Error{Status: http.StatusBadRequest, Code: "invalid_request", Detail: "a JSON request body is required"}
	}
	in := controllers.CreateBookingInput{
		ResourceID: req.Body.ResourceId, RequesterID: req.Body.RequesterId, ServiceID: req.Body.ServiceId,
		StartsAt: req.Body.StartsAt, EndsAt: req.Body.EndsAt, MeetingURL: req.Body.MeetingUrl,
	}
	if req.Body.Metadata != nil {
		in.Metadata = *req.Body.Metadata
	}

	b, replay, err := h.c.CreateBooking(ctx, TenantFrom(ctx), in, req.Params.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if replay {
		return api.CreateBooking200JSONResponse(toAPIBooking(b)), nil
	}
	return api.CreateBooking201JSONResponse(toAPIBooking(b)), nil
}

func (h *Handlers) GetBooking(ctx context.Context, req api.GetBookingRequestObject) (api.GetBookingResponseObject, error) {
	b, err := h.c.GetBooking(ctx, TenantFrom(ctx), req.BookingId)
	if err != nil {
		return nil, err
	}
	return api.GetBooking200JSONResponse(toAPIBooking(b)), nil
}

func (h *Handlers) ListResourceBookings(ctx context.Context, req api.ListResourceBookingsRequestObject) (api.ListResourceBookingsResponseObject, error) {
	in := controllers.ListBookingsInput{
		ResourceID: req.ResourceId, From: req.Params.StartsAt, To: req.Params.EndsAt, Cursor: req.Params.Cursor,
	}
	if req.Params.Status != nil {
		s := string(*req.Params.Status)
		in.Status = &s
	}
	if req.Params.Limit != nil {
		in.Limit = *req.Params.Limit
	}

	page, err := h.c.ListBookings(ctx, TenantFrom(ctx), in)
	if err != nil {
		return nil, err
	}
	out := make([]api.Booking, len(page.Bookings))
	for i, b := range page.Bookings {
		out[i] = toAPIBooking(b)
	}
	return api.ListResourceBookings200JSONResponse{Bookings: out, NextCursor: page.NextCursor}, nil
}

func toAPIBooking(b entity.Booking) api.Booking {
	out := api.Booking{
		Id: b.ID, ResourceId: b.ResourceID, RequesterId: b.RequesterID, ServiceId: b.ServiceID,
		StartsAt: b.StartsAt, EndsAt: b.EndsAt, Status: api.BookingStatus(b.Status),
		MeetingUrl: b.MeetingURL, CreatedAt: b.CreatedAt,
	}
	updated := b.UpdatedAt
	out.UpdatedAt = &updated

	if len(b.Metadata) > 0 {
		md := b.Metadata
		out.Metadata = &md
	}
	return out
}
