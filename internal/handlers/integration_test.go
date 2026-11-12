//go:build integration

package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/moidalam/scheduler/internal/controllers"
	"github.com/moidalam/scheduler/internal/db"
	dbgen "github.com/moidalam/scheduler/internal/db/gen"
	"github.com/moidalam/scheduler/internal/gen/api"
	"github.com/moidalam/scheduler/internal/handlers"
	"github.com/moidalam/scheduler/internal/observability"
	"github.com/moidalam/scheduler/internal/testdb"
)

// The server's clock is fixed, so every date below is deterministic.
// Monday 2030-01-07 is a business day in Pacific time.
var fixedNow = time.Date(2030, time.January, 1, 12, 0, 0, 0, time.UTC)

type stack struct {
	pool *pgxpool.Pool
	ctl  *controllers.Controller
	http http.Handler
	logs *bytes.Buffer // every JSON log line the stack wrote
}

// newStack builds the real application over a fresh database, with two tenants.
// Tenant 1 has one trainer (resource 1, Mon-Fri 08:00-17:00 Pacific) and one
// requester; tenant 2 has its own resource and requester with the same ids.
func newStack(t *testing.T) *stack {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()

	for _, sql := range []string{
		`INSERT INTO scheduler.tenants (id, name) VALUES (1, 'one'), (2, 'two')`,
		`INSERT INTO scheduler.services (tenant_id, id, name) VALUES (1, 1, '30m'), (2, 1, '30m')`,
		`INSERT INTO scheduler.resources (tenant_id, id, type, display_name, timezone, default_service_id)
		   VALUES (1, 1, 'trainer', 'T1', 'America/Los_Angeles', 1), (2, 1, 'trainer', 'T2', 'America/Los_Angeles', 1)`,
		`INSERT INTO scheduler.requesters (tenant_id, id) VALUES (1, 1), (1, 2), (2, 1)`,
	} {
		_, err := pool.Exec(ctx, sql)
		require.NoError(t, err)
	}
	for tenant := 1; tenant <= 2; tenant++ {
		for wd := 1; wd <= 5; wd++ {
			_, err := pool.Exec(ctx, `INSERT INTO scheduler.availability_rules
			    (tenant_id, resource_id, weekday, starts_at_local, ends_at_local) VALUES ($1, 1, $2, '08:00', '17:00')`, tenant, wd)
			require.NoError(t, err)
		}
	}

	// Real telemetry, with no exporter: spans get real ids, and logs go to a buffer.
	logs := &bytes.Buffer{}
	log, shutdown, err := observability.Setup(ctx, observability.Config{ServiceName: "scheduler-test"}, logs, slog.LevelDebug)
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	store := db.NewStore(pool)
	observed, err := db.NewObservedStore(store, log)
	require.NoError(t, err)
	ctl, err := controllers.New(observed, func() time.Time { return fixedNow }, log)
	require.NoError(t, err)
	edge, err := observability.NewOpTracker("handler", "http")
	require.NoError(t, err)
	router := handlers.NewRouter(handlers.RouterDeps{
		Handlers: handlers.New(ctl), Tenants: store, DB: store, DefaultTenant: 0, Log: log, Metrics: edge,
	})
	return &stack{pool: pool, ctl: ctl, http: router, logs: logs}
}

type resp struct {
	Status int
	Body   []byte
	Header http.Header
}

func (r resp) json(t *testing.T, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(r.Body, v), string(r.Body))
}

func (r resp) code(t *testing.T) string {
	t.Helper()
	var p api.Problem
	r.json(t, &p)
	return p.Code
}

func (s *stack) do(t *testing.T, method, path, tenant string, body any, hdr map[string]string) resp {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tenant != "" {
		req.Header.Set(handlers.TenantHeader, tenant)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.http.ServeHTTP(rec, req)
	return resp{Status: rec.Code, Body: rec.Body.Bytes(), Header: rec.Header()}
}

func mondayWindow() string {
	q := url.Values{}
	q.Set("starts_at", "2030-01-07T00:00:00-08:00")
	q.Set("ends_at", "2030-01-08T00:00:00-08:00")
	return q.Encode()
}

func bookingBody(requester int, start, end string) map[string]any {
	return map[string]any{"resource_id": 1, "requester_id": requester, "starts_at": start, "ends_at": end}
}

const (
	nineAM     = "2030-01-07T09:00:00-08:00"
	nineThirty = "2030-01-07T09:30:00-08:00"
)

func TestAvailabilityAndBooking(t *testing.T) {
	s := newStack(t)

	t.Run("lists every slot of a business day", func(t *testing.T) {
		r := s.do(t, "GET", "/v1/resources/1/availability?"+mondayWindow(), "1", nil, nil)
		require.Equal(t, http.StatusOK, r.Status, string(r.Body))
		var a api.Availability
		r.json(t, &a)
		assert.Len(t, a.Slots, 18)
		assert.Equal(t, "America/Los_Angeles", a.Timezone)
	})

	t.Run("booking succeeds and removes the slot", func(t *testing.T) {
		r := s.do(t, "POST", "/v1/bookings", "1", bookingBody(1, nineAM, nineThirty), nil)
		require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
		var b api.Booking
		r.json(t, &b)
		assert.Equal(t, api.BookingStatus("confirmed"), b.Status)

		got := s.do(t, "GET", "/v1/resources/1/availability?"+mondayWindow(), "1", nil, nil)
		var a api.Availability
		got.json(t, &a)
		assert.Len(t, a.Slots, 17)
		for _, sl := range a.Slots {
			assert.False(t, sl.StartsAt.Equal(time.Date(2030, 1, 7, 17, 0, 0, 0, time.UTC)), "the booked 09:00 PT slot is gone")
		}

		fetched := s.do(t, "GET", "/v1/bookings/"+itoa(b.Id), "1", nil, nil)
		assert.Equal(t, http.StatusOK, fetched.Status)
	})

	t.Run("the same slot again is a conflict", func(t *testing.T) {
		r := s.do(t, "POST", "/v1/bookings", "1", bookingBody(2, nineAM, nineThirty), nil)
		assert.Equal(t, http.StatusConflict, r.Status)
		assert.Equal(t, "slot_unavailable", r.code(t))
	})

	t.Run("an adjacent slot is fine: intervals are half-open", func(t *testing.T) {
		r := s.do(t, "POST", "/v1/bookings", "1", bookingBody(2, nineThirty, "2030-01-07T10:00:00-08:00"), nil)
		assert.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	})

	t.Run("rule violations are 422 with a stable code", func(t *testing.T) {
		cases := map[string]struct{ start, end, code string }{
			"off the grid":   {"2030-01-07T10:15:00-08:00", "2030-01-07T10:45:00-08:00", "misaligned_start"},
			"wrong length":   {"2030-01-07T11:00:00-08:00", "2030-01-07T12:00:00-08:00", "invalid_duration"},
			"before opening": {"2030-01-07T07:30:00-08:00", "2030-01-07T08:00:00-08:00", "outside_business_hours"},
			"at closing":     {"2030-01-07T17:00:00-08:00", "2030-01-07T17:30:00-08:00", "outside_business_hours"},
			"weekend":        {"2030-01-05T10:00:00-08:00", "2030-01-05T10:30:00-08:00", "outside_business_hours"},
			"in the past":    {"2029-12-31T10:00:00-08:00", "2029-12-31T10:30:00-08:00", "starts_in_past"},
		}
		for name, c := range cases {
			r := s.do(t, "POST", "/v1/bookings", "1", bookingBody(1, c.start, c.end), nil)
			assert.Equal(t, http.StatusUnprocessableEntity, r.Status, name)
			assert.Equal(t, c.code, r.code(t), name)
		}
	})

	t.Run("an unknown requester or resource is a 404", func(t *testing.T) {
		r := s.do(t, "POST", "/v1/bookings", "1", bookingBody(99, "2030-01-07T13:00:00-08:00", "2030-01-07T13:30:00-08:00"), nil)
		assert.Equal(t, http.StatusNotFound, r.Status)
		assert.Equal(t, "requester_not_found", r.code(t))

		r = s.do(t, "GET", "/v1/resources/99/availability?"+mondayWindow(), "1", nil, nil)
		assert.Equal(t, http.StatusNotFound, r.Status)
		assert.Equal(t, "resource_not_found", r.code(t))
	})

	t.Run("bad windows are 400", func(t *testing.T) {
		q := url.Values{"starts_at": {"2030-01-08T00:00:00-08:00"}, "ends_at": {"2030-01-07T00:00:00-08:00"}}
		r := s.do(t, "GET", "/v1/resources/1/availability?"+q.Encode(), "1", nil, nil)
		assert.Equal(t, http.StatusBadRequest, r.Status)
		assert.Equal(t, "invalid_window", r.code(t))

		q = url.Values{"starts_at": {"2030-01-07T00:00:00-08:00"}, "ends_at": {"2030-06-07T00:00:00-08:00"}}
		r = s.do(t, "GET", "/v1/resources/1/availability?"+q.Encode(), "1", nil, nil)
		assert.Equal(t, "window_too_large", r.code(t))

		r = s.do(t, "GET", "/v1/resources/1/availability", "1", nil, nil)
		assert.Equal(t, http.StatusBadRequest, r.Status, "starts_at and ends_at are required")
	})
}

func TestTenantIsolation(t *testing.T) {
	s := newStack(t)

	r := s.do(t, "POST", "/v1/bookings", "1", bookingBody(1, nineAM, nineThirty), nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var b api.Booking
	r.json(t, &b)

	t.Run("the same slot in another tenant is free", func(t *testing.T) {
		r := s.do(t, "POST", "/v1/bookings", "2", bookingBody(1, nineAM, nineThirty), nil)
		assert.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	})
	t.Run("one tenant cannot read another's booking", func(t *testing.T) {
		// Booking ids are global identity values, so tenant 2's own booking has a
		// different id; tenant 1's id must be invisible to tenant 2.
		r := s.do(t, "GET", "/v1/bookings/"+itoa(b.Id), "2", nil, nil)
		assert.Equal(t, http.StatusNotFound, r.Status)
	})
	t.Run("tenant 2's schedule lists only its own bookings", func(t *testing.T) {
		r := s.do(t, "GET", "/v1/resources/1/bookings", "2", nil, nil)
		var l api.BookingList
		r.json(t, &l)
		require.Len(t, l.Bookings, 1)
		assert.NotEqual(t, b.Id, l.Bookings[0].Id)
	})
	t.Run("a missing header fails closed", func(t *testing.T) {
		r := s.do(t, "GET", "/v1/resources/1/availability?"+mondayWindow(), "", nil, nil)
		assert.Equal(t, http.StatusBadRequest, r.Status)
		assert.Equal(t, "missing_tenant", r.code(t))
	})
	t.Run("an unknown tenant is a 404", func(t *testing.T) {
		r := s.do(t, "GET", "/v1/resources/1/availability?"+mondayWindow(), "99", nil, nil)
		assert.Equal(t, http.StatusNotFound, r.Status)
		assert.Equal(t, "unknown_tenant", r.code(t))
	})
}

func TestIdempotency(t *testing.T) {
	s := newStack(t)
	key := map[string]string{"Idempotency-Key": "abc-123"}

	first := s.do(t, "POST", "/v1/bookings", "1", bookingBody(1, nineAM, nineThirty), key)
	require.Equal(t, http.StatusCreated, first.Status, string(first.Body))
	var a api.Booking
	first.json(t, &a)

	t.Run("a retry returns the original, not a conflict", func(t *testing.T) {
		again := s.do(t, "POST", "/v1/bookings", "1", bookingBody(1, nineAM, nineThirty), key)
		assert.Equal(t, http.StatusOK, again.Status, string(again.Body))
		var b api.Booking
		again.json(t, &b)
		assert.Equal(t, a.Id, b.Id)
	})
	t.Run("the key reused for a different request is rejected", func(t *testing.T) {
		other := s.do(t, "POST", "/v1/bookings", "1", bookingBody(1, "2030-01-07T10:00:00-08:00", "2030-01-07T10:30:00-08:00"), key)
		assert.Equal(t, http.StatusUnprocessableEntity, other.Status)
		assert.Equal(t, "idempotency_key_reuse", other.code(t))
	})
	t.Run("only one booking exists", func(t *testing.T) {
		var n int
		require.NoError(t, s.pool.QueryRow(context.Background(), `SELECT count(*) FROM scheduler.bookings`).Scan(&n))
		assert.Equal(t, 1, n)
	})
}

func TestListBookingsPagination(t *testing.T) {
	s := newStack(t)
	for _, slot := range [][2]string{
		{"2030-01-07T09:00:00-08:00", "2030-01-07T09:30:00-08:00"},
		{"2030-01-07T10:00:00-08:00", "2030-01-07T10:30:00-08:00"},
		{"2030-01-07T11:00:00-08:00", "2030-01-07T11:30:00-08:00"},
	} {
		r := s.do(t, "POST", "/v1/bookings", "1", bookingBody(1, slot[0], slot[1]), nil)
		require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	}

	var seen []int64
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		path := "/v1/resources/1/bookings?limit=2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		r := s.do(t, "GET", path, "1", nil, nil)
		require.Equal(t, http.StatusOK, r.Status, string(r.Body))
		var l api.BookingList
		r.json(t, &l)
		for _, b := range l.Bookings {
			seen = append(seen, b.Id)
		}
		if l.NextCursor == nil {
			break
		}
		cursor = *l.NextCursor
	}
	assert.Len(t, seen, 3, "every booking is returned exactly once across pages")
	assert.Len(t, unique(seen), 3)

	r := s.do(t, "GET", "/v1/resources/1/bookings?cursor=not-a-cursor", "1", nil, nil)
	assert.Equal(t, http.StatusBadRequest, r.Status)
	assert.Equal(t, "invalid_cursor", r.code(t))
}

// The guarantee ADR-0004 exists for: however many requests race for one slot,
// exactly one wins and the rest are told 409. Never two.
func TestConcurrentBookingsOfOneSlot(t *testing.T) {
	s := newStack(t)

	const racers = 25
	var created, conflicts, other atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := s.ctl.CreateBooking(context.Background(), 1, controllers.CreateBookingInput{
				ResourceID: 1, RequesterID: 1,
				StartsAt: time.Date(2030, 1, 7, 17, 0, 0, 0, time.UTC),
				EndsAt:   time.Date(2030, 1, 7, 17, 30, 0, 0, time.UTC),
			}, nil)
			var e *controllers.Error
			switch {
			case err == nil:
				created.Add(1)
			case asError(err, &e) && e.Code == "slot_unavailable":
				conflicts.Add(1)
			default:
				other.Add(1)
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.EqualValues(t, 1, created.Load(), "exactly one booking wins")
	assert.EqualValues(t, racers-1, conflicts.Load(), "every other racer gets a 409")
	assert.Zero(t, other.Load())

	var n int
	require.NoError(t, s.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM scheduler.bookings WHERE status = 'confirmed'`).Scan(&n))
	assert.Equal(t, 1, n, "the database holds a single booking")
}

// The application's pre-check is only an optimisation. This bypasses it and
// inserts directly, proving the database itself refuses an overlap: the
// guarantee does not depend on the application being correct.
func TestDatabaseRefusesOverlapWithoutTheApplication(t *testing.T) {
	s := newStack(t)
	q := dbgen.New(s.pool)
	ctx := context.Background()

	insert := func(start, end time.Time) error {
		_, err := q.InsertBooking(ctx, dbgen.InsertBookingParams{
			TenantID: 1, ResourceID: 1, RequesterID: 1, ServiceID: 1,
			StartsAt: start, EndsAt: end, Metadata: []byte(`{}`),
		})
		return err
	}
	base := time.Date(2030, 1, 7, 17, 0, 0, 0, time.UTC)

	require.NoError(t, insert(base, base.Add(30*time.Minute)))

	err := insert(base.Add(15*time.Minute), base.Add(45*time.Minute))
	require.Error(t, err)
	assert.True(t, db.IsExclusionViolation(err), "an overlapping insert is rejected by the exclusion constraint, got: %v", err)

	assert.NoError(t, insert(base.Add(30*time.Minute), base.Add(60*time.Minute)), "an adjacent insert is allowed")

	// Cancelling frees the slot.
	_, err = s.pool.Exec(ctx, `UPDATE scheduler.bookings SET status = 'cancelled' WHERE tenant_id = 1 AND starts_at = $1`, base)
	require.NoError(t, err)
	assert.NoError(t, insert(base, base.Add(30*time.Minute)), "a cancelled booking no longer blocks its slot")
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func asError(err error, target **controllers.Error) bool {
	e, ok := err.(*controllers.Error)
	if ok {
		*target = e
	}
	return ok
}

func unique(ids []int64) map[int64]struct{} {
	m := map[int64]struct{}{}
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

// The constraint alone, with no application lock, still never lets two
// overlapping bookings exist. What it does not promise is a tidy failure for the
// losers: concurrent inserts into an exclusion constraint may be aborted with a
// deadlock instead of a plain exclusion violation. That is why CreateBooking
// takes a per-resource advisory lock (ADR-0004); this test pins both facts.
func TestConstraintAloneNeverAllowsTwoButMayDeadlock(t *testing.T) {
	s := newStack(t)
	q := dbgen.New(s.pool)
	ctx := context.Background()
	base := time.Date(2030, 1, 7, 17, 0, 0, 0, time.UTC)

	const racers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := q.InsertBooking(ctx, dbgen.InsertBookingParams{
				TenantID: 1, ResourceID: 1, RequesterID: 1, ServiceID: 1,
				StartsAt: base, EndsAt: base.Add(30 * time.Minute), Metadata: []byte(`{}`),
			})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	var won int
	for err := range errs {
		if err == nil {
			won++
			continue
		}
		var pgErr *pgconn.PgError
		require.True(t, errors.As(err, &pgErr), "unexpected non-database error: %v", err)
		assert.Contains(t, []string{"23P01", "40P01"}, pgErr.Code,
			"a loser must fail with an exclusion violation or a deadlock, got %s", pgErr.Code)
	}
	assert.Equal(t, 1, won, "exactly one insert wins")

	var n int
	require.NoError(t, s.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler.bookings`).Scan(&n))
	assert.Equal(t, 1, n, "the database never holds two overlapping bookings")
}
