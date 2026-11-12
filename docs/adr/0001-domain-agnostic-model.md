# ADR-0001 — Model the domain as Resource / Requester / Service / Booking

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

The first product is trainer↔client video check-ins. Naming the schema and API
after it (`trainers`, `clients`, `appointments`) is the shortest path, but
scheduling is a generic capability: the same mechanics serve clinicians,
advisors, rooms and equipment. Renaming tables and public endpoints after
adoption is expensive and tends not to happen.

## Decision

The core model is domain-neutral:

- **Tenant** — the customer that owns a set of data (ADR-0008).
- **Resource** — the entity whose time is consumed: a trainer, a room, a device.
- **Requester** — the party asking for a booking: a client.
- **Service** — the bookable offering, carrying duration and start granularity.
- **Availability rule** — recurring local-time windows per resource.
- **Booking** — a requester's confirmed reservation of a resource's time.

Domain language lives at the edge, not in the core. A `resource_type`
discriminator and a `metadata` JSONB column carry domain specifics (a trainer's
specialties) without schema churn. The API speaks `resource_id` and
`requester_id`; the brief's `trainer_id` and `user_id` map onto them
(see the glossary).

## Alternatives considered

| Option | Why not |
|---|---|
| Trainer-specific schema now, generalise later | The rename touches the database, the API contract and every client. Historically deferred indefinitely. |
| Fully generic "calendar entity" with attribute rows (EAV) | Loses type safety and query-ability for no benefit at this size. |
| Separate service per domain | Duplicates the hard part (overlap, timezones, DST) N times. |

## Consequences

**Positive** — one correctness-critical implementation; a second domain is
configuration plus a thin adapter; vocabulary in code, docs and tickets is
consistent.

**Negative / accepted costs** — one layer of indirection for the first product;
readers who think in trainers must learn the mapping. The glossary pays that
down.

**Revisit when** — a second domain needs a rule that cannot be expressed as
configuration, such as one booking spanning several resources.
