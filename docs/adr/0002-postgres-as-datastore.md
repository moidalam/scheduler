# ADR-0002 — PostgreSQL as the datastore

- **Status:** Accepted
- **Date:** 2026-11-10

## Context

The system's single hardest invariant is "no two confirmed bookings for one
resource overlap", under concurrency. It also needs range queries over time and
is small in volume (thousands of bookings, not billions).

## Decision

PostgreSQL 16, accessed over `database/sql` with `pgx` as the driver. Schema
changes are versioned, forward-only migrations checked into the repository and
applied by a dedicated migration step, never by the application at boot.

The overlap invariant is enforced by the database — see ADR-0004 — which is the
decisive reason for Postgres over the alternatives.

## Alternatives considered

| Option | Why not |
|---|---|
| SQLite | Fine for a take-home, but no `tstzrange` exclusion constraints, and the deployment story diverges from production. |
| MySQL | No native range type or exclusion constraint; overlap would fall back to advisory/row locks. |
| In-memory store | Does not survive restart; concurrency correctness becomes bespoke code. |
| MongoDB / DynamoDB | No multi-row constraint over ranges; the invariant moves into application code or a lock service. |

## Consequences

**Positive** — the invariant is enforced where it cannot be bypassed, including
by a future second writer; `tstzrange` + GiST makes window queries direct;
standard operational tooling.

**Negative / accepted costs** — a running database in development. Mitigated by
Docker Compose with a seeded, ready-to-run container.

**Revisit when** — write volume per resource makes a single-row-lock pattern a
bottleneck, which is far beyond foreseeable scale.
