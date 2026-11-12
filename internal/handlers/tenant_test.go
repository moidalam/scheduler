package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/moidalam/scheduler/internal/entity"
)

type fakeTenants map[int64]string // id -> status

func (f fakeTenants) GetTenant(_ context.Context, id int64) (entity.Tenant, error) {
	status, ok := f[id]
	if !ok {
		return entity.Tenant{}, entity.ErrNotFound
	}
	return entity.Tenant{ID: id, Status: status}, nil
}

// serve runs one request through the middleware and reports the status and the
// tenant the downstream handler saw.
func serve(t *testing.T, tenants TenantLookup, defaultTenant int64, header string) (status int, seen int64) {
	t.Helper()
	h := TenantMiddleware(tenants, defaultTenant)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = TenantFrom(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
	if header != "" {
		req.Header.Set(TenantHeader, header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, seen
}

func TestTenantMiddleware(t *testing.T) {
	tenants := fakeTenants{1: "active", 2: "active", 3: "suspended"}

	cases := []struct {
		name          string
		defaultTenant int64
		header        string
		wantStatus    int
		wantTenant    int64
	}{
		{"header selects the tenant", 0, "2", http.StatusOK, 2},
		{"header beats the default", 1, "2", http.StatusOK, 2},
		{"default used when header absent", 1, "", http.StatusOK, 1},
		{"missing header with no default fails closed", 0, "", http.StatusBadRequest, 0},
		{"not a number", 0, "abc", http.StatusBadRequest, 0},
		{"zero", 0, "0", http.StatusBadRequest, 0},
		{"negative", 0, "-4", http.StatusBadRequest, 0},
		{"unknown tenant", 0, "99", http.StatusNotFound, 0},
		{"suspended tenant", 0, "3", http.StatusForbidden, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, seen := serve(t, tenants, c.defaultTenant, c.header)
			assert.Equal(t, c.wantStatus, status)
			assert.Equal(t, c.wantTenant, seen)
		})
	}
}

// A bad header must not fall through to the default: that would silently route
// a mistaken request to the wrong tenant.
func TestTenantMiddleware_malformedHeaderDoesNotUseDefault(t *testing.T) {
	status, seen := serve(t, fakeTenants{1: "active"}, 1, "garbage")
	require.Equal(t, http.StatusBadRequest, status)
	assert.Zero(t, seen)
}
