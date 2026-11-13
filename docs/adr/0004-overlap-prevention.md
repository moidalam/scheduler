# ADR-0004 — Prevent overlap with a database exclusion constraint

- **Status:** Accepted
- **Date:** 2026-11-12
- **Relates to:** ADR-0002, ADR-0005, ADR-0008

## Context

Two requesters may book the same slot at the same instant. A read ("is this
free?") followed by a write ("book it") is not atomic at the default isolation
level: both reads see a free slot, both writes succeed, and the resource is
double-booked. This is the one invariant that must never fail.

## Decision

Enforce it in PostgreSQL with an exclusion constraint over a `tstzrange`,
scoped to the tenant and resource and to live bookings only:

```sql
ALTER TABLE scheduler.bookings ADD CONSTRAINT bookings_no_overlap
  EXCLUDE USING gist (
    tenant_id   WITH =,
    resource_id WITH =,
    tstzrange(starts_at, ends_at, '[)') WITH &&
  ) WHERE (status = 'confirmed');
```

`tenant_id` leads because it is the shard key (ADR-0008): every booking that
could conflict with another lives on the same shard, so this stays a local
constraint.

The application still pre-checks, so the common case returns a clean 409 without
relying on an error path. But the constraint is the authority: an exclusion
violation (SQLSTATE `23P01`) from the insert maps to the same 409. Correctness
does not depend on the pre-check.

### Concurrency finding: take a per-resource lock too

The first implementation relied on the constraint alone, and a test with 25
simultaneous bookings of one slot showed a flaw. The invariant held (exactly one
booking was created) but the other 24 requests did not fail cleanly. Postgres
aborted them with **deadlock detected** (`40P01`), each after a one-second
detection delay. Concurrent inserts into an exclusion constraint each wait on the
other's uncommitted row, which, unlike a unique index, can deadlock.

So the booking transaction first takes a transaction-scoped advisory lock keyed on
`(tenant_id, resource_id)`, re-checks for overlap while holding it, then inserts.
Conflicting bookings for one resource queue instead of colliding, and the losers
get an immediate, ordinary 409. Bookings for different resources never contend.

The lock is an optimisation for failure *behaviour*, not for correctness. If a
writer forgets it, the constraint still prevents the double booking; the worst
case is a deadlock error instead of a 409. Two integration tests pin both facts:
one fires 25 concurrent bookings through the application and expects one 201 and
24 409s; the other bypasses the application entirely and expects the database to
admit exactly one row.

Idempotency is separate: a unique index on `(tenant_id, idempotency_key)` makes
a retried request return the original booking rather than a conflict.

## Alternatives considered

| Option | Why not |
|---|---|
| Check-then-insert in the application | Racy. Fails under exactly the load that matters. |
| `SERIALIZABLE` isolation | Costs retries on all traffic to protect one write path. |
| A lock *instead of* the constraint (`SELECT ... FOR UPDATE`, advisory lock) | Correctness would depend on every writer, present and future, remembering to take it. We use an advisory lock *in addition*, only to avoid deadlocks. |
| Optimistic locking (version on bookings) | Slots are computed, not stored (ADR-0005), so there's no row to version. Booking validation spans multiple tables (requester, service, availability rules, existing bookings); multi-condition checks cannot be atomically compare-and-swapped. Implementing it would require materializing slots, reintroducing the garbage collection problem. High retry rate under concurrent load. |
| Distributed lock (Redis) | A new component and failure mode to solve what the database already solves. |
| Pre-materialised slot rows | Needs a generator and garbage collection forever. |

## Consequences

**Positive** — no writer can violate the invariant, including future services
and batch jobs; the guarantee is declarative and visible in the schema.

**Negative / accepted costs** — requires `btree_gist`; the driver error must be
mapped carefully; overlap semantics live in two places (constraint and
pre-check) and a concurrency integration test keeps them in agreement; bookings
for one resource are serialised by the advisory lock, which is cheap but means a
very hot resource queues rather than runs in parallel.

**Unverified** — whether a distributed Postgres accepts an exclusion constraint
on a distributed table. Tracked as API-12 in the risk register.

**Revisit when** — a booking must span multiple resources, which changes the
shape of the constraint.
