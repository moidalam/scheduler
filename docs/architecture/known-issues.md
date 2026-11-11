# API Risk & Known-Issue Register

Status: Open · Owner: Moid Alam · Last updated: 2026-11-10

Every issue below is a property of the **current specification**, not a bug
found in code — there is no code yet. The point of writing them down now is that
each one is cheap to fix on paper and expensive to fix after launch.

Severity is about impact if the service shipped as currently specified:
**S1** must be fixed before any real traffic · **S2** fix before general
availability · **S3** fix when convenient.

| # | Issue | Severity | Status |
|---|---|---|---|
| [API-1](#api-1--no-authentication-or-authorization-anywhere) | No authentication or authorization anywhere | S1 | Open |
| [API-2](#api-2--identifiers-are-guessable) | Identifiers are guessable | S2 | Open — partially mitigated |
| [API-3](#api-3--requester_id-is-caller-asserted) | `requester_id` is caller-asserted and spoofable | S1 | Open |
| [API-4](#api-4--no-eligibility-model) | Any requester may book any resource | S2 | Open |
| [API-5](#api-5--availability-discloses-a-full-schedule) | Availability discloses a resource's working pattern | S2 | Open |
| [API-6](#api-6--no-rate-limiting-or-abuse-controls) | No rate limiting or abuse controls | S2 | Open |
| [API-7](#api-7--no-audit-trail) | No audit trail on writes | S3 | Open |
| [API-8](#api-8--tenancy-is-modelled-but-not-enforced) | Tenancy is modelled but not enforced | S2 | Open |
| [API-9](#api-9--metadata-is-an-unbounded-pii-sink) | `metadata` is an unbounded PII sink | S3 | Open |
| [API-10](#api-10--availability-queries-are-unbounded-work) | Availability queries are costly to serve | S3 | Mitigated by the window cap |
| [API-11](#api-11--seed-data-violates-current-policy) | Seed data violates current policy | S3 | Accepted, documented |

---

## API-1 — No authentication or authorization anywhere

**Severity S1.** The OpenAPI contract defines no `securitySchemes`. Every
endpoint is effectively public: anyone who can reach the service can read any
resource's schedule and create bookings against it.

This is the root issue, and it is the one that makes API-2, API-3, and API-4
exploitable rather than theoretical. Guessable ids do not matter much when
every request is authenticated and authorized; they matter enormously when
nothing is.

**What to do:** add a `securitySchemes` entry (OAuth 2.0 bearer tokens or signed
service-to-service JWTs), mark every operation as requiring it, and define the
principal model — is the caller a requester acting for themselves, a resource
owner, or a back-office service acting on behalf of either? Authorization rules
then hang off that principal. This needs a decision record of its own before
implementation.

## API-2 — Identifiers are guessable

**Severity S2.** The supplied data identifies trainers and users by small
sequential integers (`trainer_id: 1`, `user_id: 3`), and ADR-0008 makes those
integers the public identifiers so the existing data keeps working.

Consequences, assuming no authorization (API-1):

- **Enumeration.** `GET /v1/resources/1/availability`,
  `/2`, `/3`… walks the entire roster of trainers and their working patterns.
- **Inference of volume.** Sequential ids disclose roughly how many trainers
  and clients exist, and how fast they are being added — commercially sensitive.
- **Correlation.** A booking returns its `requester_id`. Small integers make it
  trivial to correlate activity across bookings and build a profile of an
  individual's appointment history.

**Partially mitigated, deliberately.** ADR-0008 made public ids *opaque
strings* rather than integers specifically so this can be fixed without a
breaking API change: clients are contractually forbidden from parsing ids, and
the `external_id` column means an entity's public id can be rotated to a UUID
while its internal key and all its history stay put.

**What to do, in order:**

1. Fix API-1. Authorization is the actual control; unguessable ids are defence
   in depth, not a substitute. An attacker who can enumerate is an attacker who
   should not have been answered at all.
2. Issue UUIDv4 (or ULID) `external_id` values to all **new** entities, so the
   integers become a legacy tail that shrinks rather than a growing surface.
3. Decide on the existing integers. Rotating them to UUIDs is a one-row-per-
   entity update here, but it breaks any caller still holding `1`. Options are
   a dual-read window where both the old integer and the new UUID resolve, or a
   hard cutover coordinated with the single known consumer. Favour the dual-read
   window; the `external_id` design already supports it if the column becomes a
   one-to-many `external_ids` table.

**Note on scope:** this cannot be fixed by changing the `requester_id` type
alone. That value is minted by whatever system owns users; making it a UUID here
requires that system to supply one.

## API-3 — `requester_id` is caller-asserted

**Severity S1.** `POST /v1/bookings` takes `requester_id` in the request body
and the service records it as given (PRD R-4: "trust, and record"). Nothing ties
it to the authenticated caller, because there is no authenticated caller.

So any client can book on anyone's behalf, cancel the appearance of someone
else's intent, or fill a trainer's calendar under fabricated identities.

**What to do:** once API-1 lands, derive `requester_id` from the authenticated
principal for self-service callers and reject it in the body. Keep it as an
explicit field only for trusted back-office principals holding a
`book-on-behalf-of` scope, and record both the subject and the actor.

## API-4 — No eligibility model

**Severity S2.** There is no relationship between a requester and a resource,
so any requester may book any trainer. Already tracked as PRD R-5; repeated
here because its security consequence is distinct from its product consequence.

**What to do:** decide whether booking requires an existing relationship. If so,
model it (a `resource_requesters` table or an external entitlement check) and
make the check part of booking validation, returning `403`.

## API-5 — Availability discloses a full schedule

**Severity S2.** The availability endpoint returns every free slot, from which
every **busy** slot is directly inferable by subtracting from the published
business hours. That is an individual's working pattern — when they are at
work, when they took an afternoon off — exposed to any caller.

**What to do:** treat availability as authorized data, not public data (API-1).
Consider also returning only a bounded number of slots and suppressing the
distinction between "outside hours" and "booked" where the caller has no
relationship with the resource.

## API-6 — No rate limiting or abuse controls

**Severity S2.** Nothing limits booking creation. A single caller can exhaust a
trainer's entire calendar in one loop — a denial-of-service against the
business, not the servers. Nothing limits availability queries either, which are
the most expensive reads (ADR-0005).

**What to do:** per-principal rate limits on writes; a cap on concurrent future
bookings per requester per resource; `429` with `Retry-After` in the contract.

## API-7 — No audit trail

**Severity S3.** Bookings carry `created_at`/`updated_at` but no record of
*who* acted. With cancellation arriving in v1.1 as a status transition, "who
cancelled this and when" will be unanswerable.

**What to do:** an append-only `booking_events` table (`booking_id`, `actor`,
`action`, `at`, `payload`) written in the same transaction as the change.

## API-8 — Tenancy is modelled but not enforced

**Severity S2.** `tenant_id` is on every table, but no query in the spec is
scoped by it and no request carries a tenant. A single forgotten `WHERE` leaks
one customer's schedule to another.

**What to do:** derive the tenant from the authenticated principal, never from
the request body; enforce with Postgres row-level security so the isolation does
not depend on every query being written correctly.

## API-9 — `metadata` is an unbounded PII sink

**Severity S3.** `metadata jsonb` on resources and bookings accepts anything.
Callers will put health notes, phone numbers, and session details in it, and the
service has no retention policy, no size limit, and no idea it is holding
sensitive data.

**What to do:** cap the document size, document that `metadata` must not carry
personal or special-category data, and — if it must — classify the field and
bring it under a retention policy before launch.

## API-10 — Availability queries are unbounded work

**Severity S3.** Slot generation is proportional to window size (ADR-0005).
**Mitigated:** the window is capped at 60 days and over-cap requests are
rejected with `400`. Listed so the mitigation is not quietly removed later.

## API-11 — Seed data violates current policy

**Severity S3.** `appointments.json` record `id: 9` is a 60-minute booking,
which the 30-minute service forbids. **Accepted:** it loads verbatim and is
reported by the loader rather than rejected (PRD R-3). It still occupies its
full hour for overlap purposes, so availability stays correct. Open only in the
sense that the product owner has not confirmed the choice.

---

## How this list is maintained

An entry is added whenever a review finds a property of the spec or the
implementation that we are choosing not to fix now. Entries are closed by a
commit that links back to the issue number, not by deletion — the history of
what we knowingly accepted is the useful part.

Issues that turn into design decisions graduate into an ADR; API-2's resolution
is already partly recorded in [ADR-0008](../adr/0008-identifier-strategy.md),
and API-1 needs an ADR of its own before implementation starts.
