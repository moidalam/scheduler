# API Risk & Known-Issue Register

Status: Open · Owner: Moid Alam · Last updated: 2026-11-12

Every issue below is a property of the **current design**, recorded so each is
a decision made knowingly rather than discovered later. Much of it is hardening
beyond what the original brief asks for; the brief specifies no authentication,
so most of these are consequences of that, not defects in the exercise.

Severity is about impact if the service shipped as it stands: **S1** must be
fixed before any real traffic · **S2** before general availability · **S3** when
convenient.

| # | Issue | Severity | Status |
|---|---|---|---|
| API-1 | No authentication or authorization | S1 | Open |
| API-2 | Integer ids are guessable | S3 | Accepted — tenant-scoped |
| API-3 | `requester_id` is caller-asserted | S1 | Open |
| API-4 | Any requester may book any resource | S2 | Open |
| API-5 | Availability discloses a working pattern | S2 | Open |
| API-6 | No rate limiting or abuse controls | S2 | Open |
| API-7 | Audit trail is minimal | S3 | Partly addressed |
| API-8 | Tenant comes from an unauthenticated header | S1 | Open |
| API-9 | `metadata` is an unbounded PII sink | S3 | Open |
| API-10 | Availability queries are costly to serve | S3 | Mitigated by the window cap |
| API-11 | Seed data violates current policy | S3 | Accepted |
| API-12 | Overlap constraint unverified under sharding | S2 | Open — must verify |
| API-13 | Identity sequences assume a single allocator | S2 | Open — must verify |
| API-14 | A very large tenant cannot be split | S3 | Accepted by design |

---

## API-1 — No authentication or authorization

**S1.** The contract defines no security scheme. Anyone who can reach the service
can read any resource's schedule and create bookings. This is the root issue:
API-3, API-5 and API-8 are only exploitable because of it.

**To do:** add bearer-token authentication, define the principal model
(requester, resource owner, back-office service), and hang authorization rules
off it. Needs its own ADR before implementation.

## API-2 — Integer ids are guessable

**S3, accepted.** Ids are small sequential integers, matching the supplied data
and the brief's endpoints. Within a tenant, anyone who can reach the API can
enumerate trainers and requesters by counting.

This is proportionate for the exercise. Ids are unique only within a tenant
(`(7, 3)` is not `(1, 3)`), so a caller in one tenant gets a 404 for another's
ids once the tenant is authenticated. Who may see which resource *within* a
tenant is an authorization rule, and non-guessable ids would not replace it.
Sequential ids also reveal rough volume.

**To do:** nothing now. If the public surface grows, add an opaque public id
column rather than changing keys.

## API-3 — `requester_id` is caller-asserted

**S1.** `POST /v1/bookings` takes `requester_id` in the body and records it. Any
client can book on anyone's behalf.

**To do:** once API-1 lands, derive it from the authenticated principal for
self-service callers and reject it in the body; keep it explicit only for
trusted back-office principals.

## API-4 — No eligibility model

**S2.** Any requester may book any resource. There is no client↔trainer
relationship (PRD R-5).

**To do:** decide whether booking requires an existing relationship; if so,
model it and return `403` when absent.

## API-5 — Availability discloses a working pattern

**S2.** The endpoint returns every free slot, from which every busy slot is
inferable. That exposes when someone is working or off.

**To do:** treat availability as authorized data once API-1 lands.

## API-6 — No rate limiting or abuse controls

**S2.** A single caller can fill a trainer's calendar in a loop — a denial of
service against the business. Availability reads are the most expensive.

**To do:** per-principal limits on writes, a cap on concurrent future bookings
per requester, and `429` with `Retry-After`.

## API-7 — Audit trail is minimal

**S3, partly addressed.** `booking_events` records every write in the same
transaction, but the `actor` is whatever the caller says until authentication
exists.

## API-8 — Tenant comes from an unauthenticated header

**S1.** ADR-0009. Anyone can send `X-Tenant-ID: 2` and read tenant 2's data.
This is not tenant isolation.

**To do:** derive the tenant from the authenticated principal, never from a
request header, and enforce it with Postgres row-level security so isolation
does not depend on every query being correct.

## API-9 — `metadata` is an unbounded PII sink

**S3.** `metadata jsonb` accepts anything. Callers will put health notes and
phone numbers in it, with no size limit or retention policy.

**To do:** cap the size and document that it must not carry personal data.

## API-10 — Availability queries are costly to serve

**S3, mitigated.** Slot generation is proportional to window size. The window is
capped at 60 days and over-cap requests return `400`. Listed so the mitigation
is not quietly removed.

## API-11 — Seed data violates current policy

**S3, accepted.** Five of the ten seed bookings break current policy: 5, 7, 8, 9
and 10 are on a Saturday, and 9 is also sixty minutes against a thirty-minute
service. They load verbatim and are reported rather than rejected (PRD R-3).

## API-12 — Overlap constraint unverified under sharding

**S2.** ADR-0004 rests on an exclusion constraint. With `tenant_id` leading, it
is correctly shaped to stay local to one shard. Whether a distributed Postgres
**accepts an exclusion constraint on a distributed table** is not established.
Citus and Aurora Limitless each restrict which constraint types they propagate,
and declarative partitioning is understood not to support exclusion constraints
on a partitioned parent.

**To do:** before any sharded deployment, create the constraint on the target
platform and attempt two concurrent overlapping inserts. If it is refused, the
no-overlap guarantee needs an advisory lock keyed on `(tenant_id, resource_id)`
and ADR-0004 and ADR-0008 need revisiting.

## API-13 — Identity sequences assume a single allocator

**S2.** When a tenant moves between nodes that each run their own sequence, new
ids can collide with the moved rows. Safe while one coordinator allocates ids.

**To do:** verify the target platform's behavior before sharding; a
Snowflake-style or UUIDv7 id is the fallback.

## API-14 — A very large tenant cannot be split

**S3, accepted.** Sharding by tenant puts a tenant on exactly one node. A single
tenant approaching a node's capacity is a hotspot. Acceptable while tenants are
many and bounded; a platform-wide pool modelled as one tenant would defeat the
design (ADR-0008).

---

## How this list is maintained

An entry is added whenever a review finds something we are choosing not to fix
now. Entries are closed by a commit that links back to the issue number, not by
deletion; the history of what we knowingly accepted is the useful part.
