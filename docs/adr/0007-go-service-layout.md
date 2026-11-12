# ADR-0007 — Go service layout and libraries, following payments-api

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

The scheduling rules (grid alignment, business hours, DST, overlap) are the
valuable part and must be testable without a database or an HTTP server.
`payments-api` already settles most library and layout questions for this
team, and consistency across services is worth more than local preference.

## Decision

Layering follows `payments-api`, with one addition: a pure domain package.

```
cmd/server/main.go        # config, migrations, pool, router, graceful shutdown
cmd/seed/main.go          # one-shot loader for the supplied appointments.json
openapi/                  # spec.yaml (source of truth) + oapi-codegen config
internal/
  config/                 # viper-based static config
  db/                     # the only package that knows Postgres: migrations, sqlc, pool, transactions, Store
  entity/                 # storage-agnostic domain types and store errors
  schedule/               # pure domain: slot generation, validation, intervals (no I/O)
  controllers/            # use cases: idempotency, error mapping; talks to a Store interface
  handlers/               # implements the generated strict-server interface; tenant middleware
  gen/api/                # generated OpenAPI code (do not edit)
docs/                     # product specs, ADRs, API notes
```

| Concern | Choice |
|---|---|
| Router | `go-chi/chi/v5` |
| Database | `jackc/pgx/v5` with `pgxpool` |
| Migrations | `pressly/goose/v3`, embedded, applied at startup (ADR-0002) |
| Queries | `sqlc` |
| API codegen | `oapi-codegen` strict server |
| Config | `spf13/viper` |
| Logging | `log/slog`, JSON, request id on every line |
| Tests | `testify`; table-driven unit tests on `schedule/`; integration tests against real Postgres |

- `internal/schedule` depends on nothing outside the standard library and defines
  the rules once; controllers call it and then touch the database.
- Controllers depend on a `Store` interface they declare themselves, expressed in
  `entity` types and `entity` errors. `db.Store` implements it for Postgres and is
  the only place that knows about connections, transactions, the advisory lock,
  SQLSTATE codes or sqlc models. Replacing the database means writing another
  `Store`; no controller, handler or rule changes.
- Dependency direction: `handlers → controllers → entity`, `controllers →
  schedule`, and `db → entity`. `cmd/server` wires `db.Store` into the
  controller. Nothing depends on `handlers`, and nothing above `db` imports
  `pgx` or the generated query code.
- Only the standard library is used for errors and context; no private shared
  modules, so the repository builds anywhere.
- Delivery is Docker-first: a multi-stage Dockerfile and a Compose file that
  brings up Postgres, the API and the seed load.

## Alternatives considered

| Option | Why not |
|---|---|
| Flat `main.go` plus handlers | The rules become untestable without HTTP and a database. |
| Gin or Echo | A different router from the rest of the team's services for no gain. |
| GORM | Hides the range and exclusion semantics at the heart of this system. |
| Controllers using sqlc's `Querier` and a transaction helper directly | Leaks pgx, SQLSTATE and sqlc models into business code, so a different database means rewriting the controller. One `Store` interface keeps the seam at the use-case level. |

## Consequences

**Positive** — one convention across services; the rules are unit-testable in
milliseconds; a reviewer runs everything with one command.

**Negative / accepted costs** — a code generation step; more packages than a
single-file version.

**Revisit when** — the service grows a second transport such as a queue
consumer, which this layout accommodates as another entrypoint.
