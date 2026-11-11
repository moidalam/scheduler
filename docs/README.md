# Documentation index

## Product
- [Glossary](product/glossary.md) — the domain vocabulary and the trainer→generic mapping. **Read first.**
- [PRD](product/prd.md) — problem, goals, non-goals, user stories with acceptance criteria, business rules, risks.

## Technical
- [Data model](architecture/data-model.md) — tables, constraints, seeding plan.
- [OpenAPI contract](api/openapi.yaml) — the API source of truth.
- [Risk & known-issue register](architecture/known-issues.md) — what the current spec gets wrong or leaves undone, with severities. **Read before implementation.**

## Decisions (ADRs)

| # | Decision | Status |
|---|---|---|
| [0001](adr/0001-domain-agnostic-model.md) | Resource / Service / Booking, not Trainer / Appointment | Accepted |
| [0002](adr/0002-postgres-as-datastore.md) | PostgreSQL as the datastore | Accepted |
| [0003](adr/0003-time-representation-and-timezones.md) | UTC at rest, policy in the resource's local timezone | Accepted |
| [0004](adr/0004-overlap-prevention.md) | Overlap prevented by a database exclusion constraint | Accepted |
| [0005](adr/0005-slots-computed-not-stored.md) | Availability computed on read, not materialised | Accepted |
| [0006](adr/0006-api-style-and-versioning.md) | REST/JSON, `/v1` path versioning, problem+json errors | Accepted |
| [0007](adr/0007-go-service-layout.md) | Hexagonal Go layout on `net/http` | Accepted |
| [0008](adr/0008-identifier-strategy.md) | Opaque string ids in public APIs, UUID keys internally | Accepted, amended by 0009 |
| [0009](adr/0009-shard-ready-data-model.md) | Shard-ready schema from day one, deployed on a single node | Accepted |

New ADRs start from [the template](adr/0000-template.md) and are numbered
sequentially. An ADR is never edited after acceptance — it is superseded by a
new one that links back.

## Open questions awaiting a decision

Tracked in [PRD §9](product/prd.md#9-risks-and-open-questions): seed-data policy
(R-3), identity validation (R-4), and booking eligibility (R-5).
