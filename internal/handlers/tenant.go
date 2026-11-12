package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/moidalam/scheduler/internal/entity"
)

// TenantHeader carries the tenant a request is scoped to (ADR-0009).
const TenantHeader = "X-Tenant-ID"

type tenantKey struct{}

// TenantFrom returns the tenant TenantMiddleware stored on the context.
func TenantFrom(ctx context.Context) int64 {
	id, _ := ctx.Value(tenantKey{}).(int64)
	return id
}

// TenantLookup is the slice of the store the middleware needs.
type TenantLookup interface {
	GetTenant(ctx context.Context, id int64) (entity.Tenant, error)
}

// TenantMiddleware resolves the request's tenant and stores it on the context.
// It is the only place the tenant is read: every query takes it from the
// context, never from the path or body, so a caller cannot send a body that
// disagrees with the header.
//
// The header is caller-asserted, not authenticated. This is routing, not tenant
// isolation (known-issues API-8). defaultTenant, when non-zero, is used if the
// header is absent; otherwise a missing header is a 400.
func TenantMiddleware(lookup TenantLookup, defaultTenant int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := defaultTenant
			if raw := r.Header.Get(TenantHeader); raw != "" {
				parsed, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || parsed < 1 {
					writeProblem(w, http.StatusBadRequest, "missing_tenant", TenantHeader+" must be a positive integer")
					return
				}
				id = parsed
			}
			if id == 0 {
				writeProblem(w, http.StatusBadRequest, "missing_tenant", TenantHeader+" header is required")
				return
			}

			t, err := lookup.GetTenant(r.Context(), id)
			if errors.Is(err, entity.ErrNotFound) {
				writeProblem(w, http.StatusNotFound, "unknown_tenant", "no such tenant")
				return
			}
			if err != nil {
				writeProblem(w, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
				return
			}
			if t.Status != "active" {
				writeProblem(w, http.StatusForbidden, "tenant_suspended", "this tenant is suspended")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tenantKey{}, id)))
		})
	}
}
