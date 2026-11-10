# ADR-0007 — Go service layout: hexagonal core, standard library HTTP

- **Status:** Accepted
- **Date:** 2026-11-10

## Context

The scheduling rules (grid alignment, business hours, DST, overlap) are the
valuable part and must be testable without a database or an HTTP server. Go's
ecosystem offers many frameworks; most of what they add is not needed here, and
`net/http` since Go 1.22 routes by method and path pattern natively.

## Decision

Ports-and-adapters layout:

```
cmd/schedulerd/            # entrypoint: config, wiring, graceful shutdown
internal/scheduler/        # domain core — pure, no I/O
  availability.go          #   slot generation from rules + grid
  booking.go               #   validation rules
  interval.go              #   half-open interval algebra
internal/http/             # inbound adapter — handlers, DTOs, problem+json
internal/storage/postgres/ # outbound adapter — repositories, sqlc-generated
internal/platform/         # config, logging, telemetry, health
migrations/                # forward-only, versioned SQL
docs/                      # product specs, ADRs, API contract
```

- The core defines interfaces (`BookingRepository`, `AvailabilityRepository`)
  and depends on nothing outside the standard library.
- Routing: `net/http` + `http.ServeMux`. No web framework.
- SQL: hand-written, type-checked via `sqlc`. No ORM — the queries here are few
  and the range predicates are ones an ORM obscures.
- Config: environment variables, validated at startup, fail-fast.
- Logging: `log/slog`, JSON, with a request id on every line.
- Tests: table-driven unit tests on the core (no database); integration tests
  against a real Postgres via testcontainers, including a concurrency test for
  ADR-0004 and DST fixtures for ADR-0003.
- Delivery: multi-stage Dockerfile (distroless final image) plus a Compose file
  that brings up the API, Postgres, migrations, and the seed load in one command.

## Alternatives considered

| Option | Why not |
|---|---|
| Flat `main.go` + handlers | Fast to write; the scheduling rules end up untestable without HTTP and a database. |
| Gin / Echo / Chi | Convenience over `net/http` that this surface does not need; an added dependency on the request path. |
| GORM or another ORM | Hides the range/exclusion semantics that are the heart of the system. |
| Strict Clean Architecture with many layers | Ceremony out of proportion to a four-endpoint service. |

## Consequences

**Positive** — the rules are unit-testable in milliseconds; Postgres and HTTP
are swappable at the edges; a reviewer can run everything with
`docker compose up`.

**Negative / accepted costs** — more packages and interfaces than a single-file
version; some hand-wiring in `main` that a framework would do. Both are
deliberate.

**Revisit when** — the service grows a second transport (gRPC, queue consumer),
which this layout already accommodates as another adapter.
