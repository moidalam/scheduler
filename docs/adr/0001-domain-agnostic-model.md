# ADR-0001 — Model the domain as Resource / Service / Booking, not Trainer / Appointment

- **Status:** Accepted
- **Date:** 2026-11-10

## Context

The first product is trainer↔client video check-ins. Naming the schema and API
after that product (`trainers`, `clients`, `appointments`) would be the shortest
path, but scheduling is a generic capability: the same mechanics serve
clinicians, advisors, rooms, and equipment. Renaming tables and public endpoints
after adoption is expensive and tends not to happen.

## Decision

The core model is domain-neutral:

- **Resource** — the entity whose time is consumed (a trainer, a room, a device).
- **Service** — the bookable offering, carrying duration and granularity.
- **Booking** — a requester's confirmed reservation of a resource's time.
- **Availability rule** — recurring local-time windows per resource.

Domain language lives in a thin presentation concern, not in the core. A
trainer-flavoured API, if ever needed, is an adapter over `/v1/resources`.

A `resource_type` discriminator and a free-form `metadata` JSONB column carry
domain specifics (e.g. a trainer's specialties) without schema churn.

## Alternatives considered

| Option | Why not |
|---|---|
| Trainer-specific schema now, generalise later | The rename touches the database, the API contract, and every client. Historically deferred indefinitely. |
| Fully generic "calendar entity" with EAV attributes | Loses type safety and query-ability for no benefit at this size. Over-correction. |
| Separate service per domain | Duplicates the hard part (overlap, timezones, DST) N times. |

## Consequences

**Positive** — one correctness-critical implementation; a second domain is
configuration plus a thin adapter; the vocabulary in code, docs, and tickets is
consistent (see the glossary).

**Negative / accepted costs** — one layer of indirection for the first product;
readers who think in trainers must learn the mapping. The glossary exists to pay
that down.

**Revisit when** — a second domain needs a rule that cannot be expressed as
configuration (e.g. bookings spanning multiple resources simultaneously).
