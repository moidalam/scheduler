# Scheduler

A domain-agnostic scheduling API: requesters discover a resource's free slots
and reserve one, with overlap made impossible at the datastore level.

The first product on it is trainer↔client video check-ins — 30-minute sessions
on the hour and half hour, Mon–Fri 08:00–17:00 Pacific — but nothing
trainer-specific lives in the core. See
[docs/product/glossary.md](docs/product/glossary.md) for the mapping.

**Repository:** https://github.com/moidalam/scheduler

**Status:** specification. No code yet.

## Where things are

```
docs/
  README.md                 # documentation index — start here
  product/glossary.md       # domain vocabulary
  product/prd.md            # product spec
  architecture/data-model.md
  api/openapi.yaml          # API contract
  adr/                      # architecture decision records
data/
  appointments.seed.json    # supplied historical appointments
```

## Planned layout (ADR-0007)

```
cmd/schedulerd/            # entrypoint
internal/scheduler/        # domain core — pure, no I/O
internal/http/             # handlers, DTOs, problem+json
internal/storage/postgres/ # repositories
internal/platform/         # config, logging, telemetry
migrations/                # forward-only SQL
```

## The three endpoints from the brief

| Brief | This API |
|---|---|
| GET available times for a trainer between two dates | `GET /v1/resources/{resource_id}/availability?starts_at=&ends_at=` |
| POST a new appointment | `POST /v1/bookings` |
| GET scheduled appointments for a trainer | `GET /v1/resources/{resource_id}/bookings` |

## Design decisions worth knowing before reading code

- Availability is **computed on read**, never stored ([ADR-0005](docs/adr/0005-slots-computed-not-stored.md)).
- Overlap is prevented by a Postgres **exclusion constraint**, not by application logic ([ADR-0004](docs/adr/0004-overlap-prevention.md)).
- Instants are stored in UTC; business hours are evaluated in the resource's **IANA timezone**, so DST is handled rather than approximated ([ADR-0003](docs/adr/0003-time-representation-and-timezones.md)).
- A taken slot is `409`; a request that breaks a scheduling rule is `422`. They mean different things to a client ([ADR-0006](docs/adr/0006-api-style-and-versioning.md)).
