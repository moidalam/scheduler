# CLAUDE.md: scheduler

## What this project is

A Go service that lets a requester discover a resource's free time and book it,
with overlap prevented in the database. Domain-agnostic: a trainer is a
`resource`, a client a `requester`. Multi-tenant: every request is scoped to a
tenant. See [docs/product/glossary.md](docs/product/glossary.md) first.

Layout and libraries follow `payments-api` (ADR-0007): the same conventions, not
copied code, and none of its private shared modules.

## Where to look

- [docs/README.md](docs/README.md): index of product specs and ADRs.
- [openapi/spec.yaml](openapi/spec.yaml): **the endpoint list.** Not duplicated here, because a copy drifts.
- [docs/architecture/data-model.md](docs/architecture/data-model.md): tables and the seeding plan.
- [docs/architecture/known-issues.md](docs/architecture/known-issues.md): accepted risks.

## Commands

```
make dev               # Postgres + API + seed loader, all in Docker
make test              # unit tests (-race)
make test-integration  # integration tests against real Postgres, in Docker
make generate          # regenerate API stubs and sqlc after editing the spec / queries
make corp-ca           # macOS: let the Docker build trust a TLS-inspecting proxy
```

## Rules that matter

- **Telemetry goes through `internal/observability` only.** Log with the context
  you were given, so `request_id`, `tenant_id` and `trace_id` attach themselves.
  Never log `metadata`, `meeting_url` or idempotency keys. Metric tags are bounded
  values only (`error_code`, `result`, `sqlstate`): never ids. See
  [ADR-0010](docs/adr/0010-telemetry-opentelemetry.md).
- **Only `internal/db` knows the database.** Controllers depend on the `Store`
  interface and `internal/entity` types; no `pgx`, `dbgen`, transaction or SQLSTATE
  outside `internal/db` (tests and `cmd/seed` aside). Another database means a new
  `Store`, nothing else.
- **Every query is scoped by `tenant_id`**, taken from the request context, never
  the path or body (ADR-0009). Every key and unique constraint leads with it; every
  foreign key is composite. This is what keeps the schema shardable (ADR-0008).
  Do not add a table or query that breaks it.
- **The exclusion constraint is the authority on overlap** (ADR-0004). The
  advisory lock in `CreateBooking` exists only to avoid deadlocks; never remove
  the constraint because the lock "covers it".
- **`internal/schedule` is pure.** No I/O, standard library only. A slot
  `GenerateSlots` offers must always pass `ValidateBooking`; a test asserts it.
- **Business hours are local time** (ADR-0003). Never compute them from a fixed
  UTC offset. DST tests use a horizon long enough to reach the dates, or they
  pass vacuously.
- **Integers are never exposed as meaningful across tenants.** Ids are unique per
  tenant only.
- **OpenAPI-first:** edit `openapi/spec.yaml`, run `make generate`. Generated code
  (`internal/gen/`, `internal/db/gen/`) is committed and must not be hand-edited.
- **Migrations are embedded goose SQL** applied at startup, forward-only. The
  migration connection pins `search_path=public`; see the regression test
  `TestRunMigrationsIsIdempotent` for why.
- **A skipped integration test means UNVERIFIED**, not passing. `testdb` skips
  when `TEST_DATABASE_URL` is unset.
- Run `make test-integration` after changing `internal/db` or booking logic. The
  Docker test image is built from the working tree, so rebuild it (`--build`)
  after adding tests.

## Local-only secrets

`.env.example` holds throwaway credentials for a container on your own machine.
Never reuse them elsewhere. `.env` and `certs/*.crt` are gitignored.
