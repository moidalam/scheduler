# Documentation index

## Product
- [Glossary](product/glossary.md): the domain vocabulary and the trainer→generic mapping. **Read first.**
- [PRD](product/prd.md): problem, goals, non-goals, user stories with acceptance criteria, business rules, risks.

## Technical
- [Data model](architecture/data-model.md): tables, constraints, seeding.
- [OpenAPI contract](../openapi/spec.yaml): the API source of truth.
- [Risk & known-issue register](architecture/known-issues.md): what the design leaves undone, with severities. **Read before any real deployment.**

## Decisions (ADRs)

| # | Decision |
|---|---|
| [0001](adr/0001-domain-agnostic-model.md) | Resource / Requester / Service / Booking, not Trainer / Appointment |
| [0002](adr/0002-postgres-as-datastore.md) | PostgreSQL 17 with pgx, goose and sqlc |
| [0003](adr/0003-time-representation-and-timezones.md) | UTC at rest, business rules in the resource's local timezone |
| [0004](adr/0004-overlap-prevention.md) | Overlap prevented by an exclusion constraint, plus a per-resource lock to avoid deadlocks |
| [0005](adr/0005-slots-computed-not-stored.md) | Availability computed on read, not materialised |
| [0006](adr/0006-api-style-and-versioning.md) | OpenAPI-first REST/JSON, `/v1`, problem+json errors |
| [0007](adr/0007-go-service-layout.md) | Go layout and libraries, following payments-api |
| [0008](adr/0008-tenant-sharding-and-identifiers.md) | Tenant-scoped, shard-ready model with bigint identity ids |
| [0009](adr/0009-tenant-context-via-header.md) | Tenant read from the `X-Tenant-ID` header |

New ADRs start from [the template](adr/0000-template.md) and are numbered in
sequence. An accepted ADR is not edited to change its decision; a new one
supersedes it. (The set was reset on 2026-11-12 to the tenant-sharded design,
before any code existed; earlier iterations are in git history.)

## Open questions

Tracked in [PRD §9](product/prd.md): the seed-data policy (R-3, now five records
not one), who may act as a requester (R-4), booking eligibility (R-5), and whether
tenants are many and bounded (R-6).
