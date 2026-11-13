-- Every query is scoped by tenant_id (ADR-0008). None reads across tenants.

-- name: GetTenant :one
SELECT id, name, status, created_at
FROM scheduler.tenants
WHERE id = $1;

-- name: GetResource :one
SELECT *
FROM scheduler.resources
WHERE tenant_id = $1 AND id = $2;

-- name: GetService :one
SELECT *
FROM scheduler.services
WHERE tenant_id = $1 AND id = $2;

-- name: RequesterExists :one
SELECT EXISTS (
    SELECT 1 FROM scheduler.requesters WHERE tenant_id = $1 AND id = $2
) AS exists;

-- Rule times come back as minutes since local midnight, which is what the
-- schedule package works in; no time-of-day type crosses the boundary.
-- name: ListAvailabilityRules :many
SELECT id,
       weekday::int                                                        AS weekday,
       (EXTRACT(HOUR FROM starts_at_local) * 60
         + EXTRACT(MINUTE FROM starts_at_local))::int                      AS start_minute,
       (EXTRACT(HOUR FROM ends_at_local) * 60
         + EXTRACT(MINUTE FROM ends_at_local))::int                        AS end_minute,
       effective_from,
       effective_to
FROM scheduler.availability_rules
WHERE tenant_id = $1 AND resource_id = $2
ORDER BY weekday, starts_at_local;

-- Confirmed bookings overlapping [range_start, range_end), half-open.
-- name: ListConfirmedBookingsInRange :many
SELECT starts_at, ends_at
FROM scheduler.bookings
WHERE tenant_id = $1
  AND resource_id = $2
  AND status = 'confirmed'
  AND starts_at < sqlc.arg(range_end)::timestamptz
  AND ends_at   > sqlc.arg(range_start)::timestamptz
ORDER BY starts_at;

-- name: InsertBooking :one
INSERT INTO scheduler.bookings (
    tenant_id, resource_id, requester_id, service_id,
    starts_at, ends_at, meeting_url, idempotency_key, metadata
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: GetBooking :one
SELECT *
FROM scheduler.bookings
WHERE tenant_id = $1 AND id = $2;

-- name: GetBookingByIdempotencyKey :one
SELECT *
FROM scheduler.bookings
WHERE tenant_id = $1 AND idempotency_key = $2;

-- Keyset pagination on (starts_at, id). Optional filters are NULL when unused.
-- name: ListResourceBookings :many
SELECT *
FROM scheduler.bookings
WHERE tenant_id = sqlc.arg(tenant_id)
  AND resource_id = sqlc.arg(resource_id)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(from_ts)::timestamptz IS NULL OR ends_at   > sqlc.narg(from_ts)::timestamptz)
  AND (sqlc.narg(to_ts)::timestamptz   IS NULL OR starts_at < sqlc.narg(to_ts)::timestamptz)
  AND (sqlc.narg(after_starts_at)::timestamptz IS NULL
       OR (starts_at, id) > (sqlc.narg(after_starts_at)::timestamptz, sqlc.narg(after_id)::bigint))
ORDER BY starts_at, id
LIMIT sqlc.arg(row_limit);

-- name: InsertBookingEvent :exec
INSERT INTO scheduler.booking_events (tenant_id, booking_id, actor, action, payload)
VALUES ($1, $2, $3, $4, $5);

-- Serialises bookings for one resource inside a transaction. This is not what
-- keeps the data correct (the exclusion constraint does that, ADR-0004). It
-- exists because concurrent inserts into an exclusion constraint can deadlock;
-- queuing them on this lock turns that into an ordinary, immediate 409.
-- name: LockResourceBookings :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(sqlc.arg(tenant_id)::bigint::text || ':' || sqlc.arg(resource_id)::bigint::text, 0)
);

-- Rate limit configuration queries.
-- name: GetRateLimitConfig :one
SELECT tenant_id, scope, requests_per_second, created_at, updated_at
FROM scheduler.rate_limits
WHERE tenant_id = $1 AND scope = $2;
