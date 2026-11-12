# Scheduler

A domain-agnostic scheduling API. A requester discovers when a resource is free
and books a slot, and the database makes double-booking impossible.

The first product on it is trainer↔client video check-ins: 30-minute sessions on
the hour and half hour, Monday–Friday 08:00–17:00 Pacific. Nothing trainer-specific
lives in the core, since a trainer is a `resource` and a client a `requester`.
See the [glossary](docs/product/glossary.md).

**Repository:** https://github.com/moidalam/scheduler

## Run it

Everything runs in Docker. You need Docker and `make`; Go is not required.

```bash
make dev
```

That starts Postgres 17, the API on `localhost:8080`, and a one-shot loader that
imports `data/appointments.seed.json`. It creates `.env` from `.env.example` on
first run (throwaway local values, gitignored).

```bash
make test-integration   # integration tests against real Postgres, in Docker
make test               # unit tests (needs Go)
make dev-down           # stop everything
make help               # all targets
```

### Behind a TLS-inspecting corporate proxy?

If the Docker build fails with `x509: certificate signed by unknown authority`,
your network re-signs HTTPS. Run `make corp-ca` once (macOS) to let the build
stage trust your proxy's CA. See [certs/README.md](certs/README.md). The CA stays
out of git and out of the runtime image.

## The three endpoints from the brief

| Brief | This API |
|---|---|
| Available times for a trainer between two dates | `GET /v1/resources/{resource_id}/availability?starts_at=&ends_at=` |
| Create an appointment | `POST /v1/bookings` |
| A trainer's scheduled appointments | `GET /v1/resources/{resource_id}/bookings` |

Plus `GET /v1/bookings/{booking_id}`. The contract is [openapi/spec.yaml](openapi/spec.yaml).
`resource_id` is the brief's `trainer_id`; `requester_id` is its `user_id`.

```bash
# Free slots for trainer 1 on a Monday
curl -G localhost:8080/v1/resources/1/availability \
  --data-urlencode 'starts_at=2026-11-16T00:00:00-08:00' \
  --data-urlencode 'ends_at=2026-11-17T00:00:00-08:00'

# Book one
curl -X POST localhost:8080/v1/bookings -H 'Content-Type: application/json' \
  -d '{"resource_id":1,"requester_id":1,"starts_at":"2026-11-16T10:00:00-08:00","ends_at":"2026-11-16T10:30:00-08:00"}'

# Trainer 1's schedule (includes the seeded 2019 appointments)
curl localhost:8080/v1/resources/1/bookings
```

| Outcome | Status | `code` |
|---|---|---|
| Created | `201` | |
| Same `Idempotency-Key` retried | `200` | |
| Slot already taken | `409` | `slot_unavailable` |
| Breaks a rule | `422` | `outside_business_hours`, `misaligned_start`, `invalid_duration`, `starts_in_past`, … |
| Bad request | `400` | `invalid_window`, `window_too_large`, `missing_tenant`, … |
| Unknown resource or tenant | `404` | `resource_not_found`, `unknown_tenant`, … |

A taken slot (409) and a request that breaks a rule (422) mean different things
to a client, so they are kept apart. Branch on `code`, not on the text.

Every request is scoped to a tenant by the `X-Tenant-ID` header. The Compose file
sets `DEFAULT_TENANT_ID=1` so plain `curl` works; leave that unset anywhere real.
The header is **not authenticated**, so this is routing, not isolation
([ADR-0009](docs/adr/0009-tenant-context-via-header.md), [known issues](docs/architecture/known-issues.md)).

## Things worth knowing before you read the code

- **Overlap is prevented by Postgres**, with an exclusion constraint, not by
  application logic. Racing bookings of one slot yield one `201` and the rest `409`,
  and there is a test that fires 25 at once
  ([ADR-0004](docs/adr/0004-overlap-prevention.md)).
- **Availability is computed on read**, never stored
  ([ADR-0005](docs/adr/0005-slots-computed-not-stored.md)).
- **Business hours are evaluated in the resource's timezone**, so DST is handled
  rather than approximated ([ADR-0003](docs/adr/0003-time-representation-and-timezones.md)).
- **The schema is shard-ready on one node.** Tenant is the top-level entity and
  leads every key, so it can later be distributed by `tenant_id`
  ([ADR-0008](docs/adr/0008-tenant-sharding-and-identifiers.md)).
- **The supplied data has surprises.** Five of the ten appointments break the
  stated rules (four are on a Saturday, and one is 60 minutes). They are loaded
  as-is and reported ([PRD R-3](docs/product/prd.md)).

## Observability

`make dev` also starts a Grafana stack. Open <http://localhost:3000>:

- **Explore → Loki:** `{service_name="scheduler"}`. Filter by `request_id`, `trace_id` or `op`.
- **Explore → Tempo:** search by `trace_id`, or open one from a log line. It shows the request's spans from the handler down through the controller to each database call.
- **Explore → Prometheus:** `operation_total` and `operation_duration_milliseconds_bucket`, by `layer`, `op` and `result`.

Dashboards and alert rules are not included yet. The stack keeps data only while its container runs.

## Layout

```
cmd/server/            # API entrypoint
cmd/seed/              # one-shot loader for appointments.json
openapi/spec.yaml      # the API contract (source of truth)
internal/
  schedule/            # pure rules: slots, validation, DST. No I/O
  controllers/         # use cases; depend on a Store interface, not on Postgres
  entity/              # storage-agnostic domain types and errors
  handlers/            # HTTP: generated strict-server impl, tenant middleware
  db/                  # the only Postgres-aware package: migrations, sqlc, pool, transactions, Store
  config/              # viper-based settings
docs/                  # product specs, ADRs, data model, risk register
```

The stack follows `payments-api`: chi, pgx, goose, sqlc, and an OpenAPI-first
strict server ([ADR-0007](docs/adr/0007-go-service-layout.md)). Generated code is
committed; run `make generate` after editing the spec or `internal/db/queries.sql`.

## Documentation

Start at [docs/README.md](docs/README.md).
