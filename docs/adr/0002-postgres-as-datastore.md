# ADR-0002 — PostgreSQL as the datastore, with pgx, goose and sqlc

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

The system's hardest invariant is "no two confirmed bookings for one resource
overlap", under concurrency. It also needs range queries over time. Volume is
small today. We want to follow the stack already used by `payments-api` so the
team has one set of conventions rather than two.

## Decision

- **PostgreSQL 17.** Native range types and exclusion constraints are what make
  ADR-0004 possible.
- **`jackc/pgx/v5` with `pgxpool`** for runtime access.
- **`pressly/goose/v3`** for migrations: plain SQL files, embedded in the binary
  with `//go:embed`, applied at startup under a Postgres session advisory lock so
  concurrent instances take turns. There is no separate migration step.
- **`sqlc`** generates type-safe Go from hand-written SQL in `queries.sql`.
  No ORM.
- All tables live in a dedicated `scheduler` schema.

## Alternatives considered

| Option | Why not |
|---|---|
| SQLite | No `tstzrange` exclusion constraints; the deployment story diverges from production. |
| MySQL | No native range type or exclusion constraint; overlap would fall back to locks. |
| MongoDB / DynamoDB | No multi-row constraint over ranges; the invariant moves into application code. |
| An ORM (GORM) | Hides the range and exclusion semantics that are the heart of the system. |
| Separate migration job | One more moving part; advisory-locked startup migration is already proven in `payments-api`. |

## Consequences

**Positive** — the invariant is enforced where it cannot be bypassed; one
toolchain across services; queries are checked at generate time.

**Negative / accepted costs** — the application needs rights to run DDL; a
running database in development, mitigated by Docker Compose.

**Revisit when** — write volume per resource makes a single-row pattern a
bottleneck, which is far beyond foreseeable scale.
