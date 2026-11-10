# ADR-0004 — Prevent overlap with a database exclusion constraint

- **Status:** Accepted
- **Date:** 2026-11-10
- **Relates to:** ADR-0002

## Context

Two requesters may book the same slot at the same instant. A read ("is this
free?") followed by a write ("book it") is not atomic at the default isolation
level: both reads see a free slot, both writes succeed, and the resource is
double-booked. This is the system's one invariant that must never fail.

## Decision

Enforce the invariant in PostgreSQL with an exclusion constraint over a
`tstzrange`, scoped to the resource and to live bookings only:

```sql
ALTER TABLE bookings ADD CONSTRAINT bookings_no_overlap
  EXCLUDE USING gist (
    resource_id WITH =,
    tstzrange(starts_at, ends_at, '[)') WITH &&
  ) WHERE (status = 'confirmed');
```

The application still performs a pre-check, so the common case returns a clean,
informative 409 without relying on an error path. But the constraint is the
authority: a unique-violation/exclusion error (SQLSTATE `23P01`) from the insert
is translated to the same 409. Correctness does not depend on the pre-check.

Idempotency is separate and complementary: a unique index on
`(resource_id, idempotency_key)` makes a retried request return the original
booking rather than a conflict.

## Alternatives considered

| Option | Why not |
|---|---|
| Application-level check-then-insert | Racy. Fails under exactly the load that matters. |
| `SERIALIZABLE` isolation | Works, but costs retries on all traffic to protect one write path, and the failure mode (serialization errors) is harder to attribute. |
| `SELECT ... FOR UPDATE` on a resource row | Serialises all bookings for a resource and depends on every future writer remembering to take the lock. |
| Distributed lock (Redis) | Adds a component and a new failure mode to solve what the database already solves correctly. |
| Pre-materialised slot rows with `UPDATE ... WHERE free` | Workable, but requires generating and garbage-collecting slot rows forever, and couples the grid to storage. |

## Consequences

**Positive** — the invariant cannot be violated by any writer, including future
services, batch jobs, or a careless migration; the guarantee is declarative and
visible in the schema.

**Negative / accepted costs** — requires the `btree_gist` extension; the
driver-level error code must be mapped carefully (a bare "internal error" on
23P01 would be a poor experience); overlap semantics are now expressed in two
places (constraint and pre-check) and must stay in agreement — covered by an
integration test that fires concurrent identical bookings and asserts exactly
one 201 and one 409.

**Revisit when** — bookings need to span multiple resources, which changes the
shape of the constraint.
