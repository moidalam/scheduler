# PRD — Scheduler

Status: Draft · Owner: Moid Alam · Last updated: 2026-11-10

## 1. Summary

A standalone scheduling service that lets a requester discover when a resource
is free and reserve a slot, without double-booking that resource. The first
consumer is a trainer/client video check-in product; the service is built
domain-agnostic so later consumers (clinics, advisors, rooms, equipment) adopt
it without a rewrite.

## 2. Problem

Booking today is done by hand — messages back and forth, a shared calendar,
a spreadsheet. That produces three failures:

1. **Double bookings.** Two requesters are promised the same time.
2. **Wasted discovery.** A requester cannot see what is free without asking.
3. **No system of record.** Nobody can answer "what is on my schedule" or
   "how utilised are we" from data.

## 3. Goals

- G1. A requester can see every bookable slot for a resource over a date range.
- G2. A requester can book a slot and get a definitive yes/no, never a maybe.
- G3. A resource's bookings never overlap. This is enforced by the datastore,
  not only by application logic.
- G4. A resource owner can list their schedule.
- G5. Availability policy (hours, duration, granularity, timezone) is
  configuration, not code.

## 4. Non-goals (v1)

Explicitly out of scope, listed so they are not accidentally designed in:

- Cancellation and rescheduling (v1.1 — shape reserved in the API, see §8).
- Recurring bookings, waitlists, group/multi-attendee bookings.
- Notifications (email/SMS/push), calendar sync (Google/Outlook/CalDAV).
- Payments, pricing, cancellation fees.
- Video call provisioning. The system stores a `meeting_url` the caller
  supplies; it does not create meetings.
- Multi-resource bookings (a session needing both a trainer *and* a room).
- End-user UI. This is an API.

## 5. Users and jobs

| Persona | Job to be done |
|---|---|
| **Requester** (client) | "Show me when my trainer is free next week, and let me take 10am Tuesday before anyone else does." |
| **Resource owner** (trainer) | "Show me what I have booked, and never let two people be promised the same half hour." |
| **Operator** (gym admin) | "Set the hours each trainer works and the length of a session, without an engineer." |
| **Integrating service** | "Give me a stable, idempotent API I can call from a mobile app or a back-office tool." |

## 6. User stories and acceptance criteria

### US-1 — Discover availability
> As a requester, I want the list of bookable slots for a resource between two
> dates, so I can choose one.

Accepts:
- Given a window `[starts_at, ends_at)`, the response lists every slot that is
  (a) inside an availability rule for that resource, (b) aligned to the
  service's granularity, (c) not overlapping an existing booking, and
  (d) not in the past.
- Slots are returned in ascending chronological order.
- An empty window, a weekend-only window, or a fully booked window returns an
  empty list with HTTP 200 — not an error.
- `ends_at` must be strictly after `starts_at`; otherwise 400.
- The window is capped (default 60 days). Over the cap → 400 with a clear
  message rather than a slow, enormous response.

### US-2 — Book a slot
> As a requester, I want to reserve a slot and receive a confirmation or a
> definitive rejection.

Accepts:
- A valid request returns 201 with the created booking, including a
  server-assigned id.
- The slot must satisfy every rule in US-1 at the moment of booking, not at the
  moment the list was fetched.
- Booking a slot already taken for that resource returns **409 Conflict** with
  a machine-readable code, even under simultaneous requests. Exactly one of two
  concurrent identical bookings succeeds.
- A booking outside business hours, misaligned to the grid, of the wrong
  duration, or in the past returns **422** with a code naming the violated rule.
- Supplying the same `Idempotency-Key` twice returns the original booking with
  201/200 rather than creating a second one or returning 409.

### US-3 — View a schedule
> As a resource owner, I want my booked appointments for a window.

Accepts:
- Returns bookings for the resource in ascending order, filtered by an optional
  window, paginated.
- Includes the requester id and the booking's status.

### US-4 — Configure availability
> As an operator, I want to define when a resource can be booked.

Accepts:
- Weekly recurring rules per resource: weekday, local start time, local end
  time, and the resource's IANA timezone.
- A service defines duration and start-granularity (30 minutes / on :00 and :30
  for the first product).
- Changing a rule does not retroactively cancel bookings already made; existing
  bookings survive and are reported as out-of-policy if queried.

### US-5 — Migrate existing data
> As an operator, I want the historical appointments loaded so the system is
> correct from day one.

Accepts:
- The supplied `appointments.json` loads into bookings without mutation of its
  times.
- Its integer `id`, `trainer_id`, and `user_id` become the real identifiers of
  the booking, resource, and requester in a default tenant `1`, so a caller
  holding `trainer_id: 1` can call the endpoints with `1` (ADR-0008).
- The loader is idempotent — running it twice does not duplicate rows.
- Records in the seed file that violate current policy are loaded as-is and
  reported by the loader, not rejected. Five of the ten do: see §9/R-3.

## 7. Business rules (v1 defaults, all configurable)

| Rule | v1 default | Configured by |
|---|---|---|
| Bookable hours | Mon–Fri, 08:00–17:00 | Availability rule per resource |
| Timezone | `America/Los_Angeles` | Resource |
| Duration | 30 minutes | Service |
| Start granularity | :00 and :30 | Service |
| Overlap | Not permitted per resource | Enforced in the datastore |
| Lead time (min notice) | 0 (v1) | Service (v1.1) |
| Booking horizon (max ahead) | 60 days | Service |

## 8. Reserved shape for v1.1

To avoid a breaking change later, v1 already carries:
- `status` on a booking (`confirmed` | `cancelled`), so cancellation is a state
  transition rather than a delete.
- `service_id` on a booking, so multiple durations are additive.
- `Idempotency-Key` on writes.

## 9. Risks and open questions

- **R-1 — Timezone and DST.** Slot generation in local time across a DST
  boundary is the likeliest source of bugs. Mitigation: generate slots by
  walking local civil time via the IANA database, then convert; explicit test
  cases on both US transition days.
- **R-2 — Concurrency.** Application-level "check then insert" is not safe.
  Mitigation: a database exclusion constraint. See ADR-0004.
- **R-3 — Dirty seed data.** Five of the ten supplied appointments break the
  current rules. Records 5, 7, 8, 9 and 10 are all on Saturday 2019-01-26, outside
  the Monday–Friday window, and record 9 is also sixty minutes where the rule is
  thirty. Assumption taken: history is immutable and loaded verbatim; policy
  applies to new bookings only (R-3). **Open:** confirm with the product owner that
  this is preferred over normalising or dropping them, or whether Saturday is
  meant to be bookable.
- **R-4 — Identity.** This service owns a `requesters` table, so a booking's
  requester must exist in the tenant; the foreign key enforces it. Who is
  *allowed* to act as a given requester is not enforced (see known-issues
  API-3). `requesters.external_ref` holds the id in any upstream identity system.
- **R-5 — Who may book whom.** v1 does not model a client↔trainer relationship,
  so any requester may book any resource. **Open:** is an eligibility check
  required before launch?

- **R-6 — Tenancy.** Every request is scoped to a tenant read from the
  `X-Tenant-ID` header (ADR-0009). The header is unauthenticated, so this is
  routing, not isolation (known-issues API-8). A very large tenant cannot be
  split across nodes (API-14). **Open:** confirm tenants are many and bounded.

## 10. Success measures

- Zero overlapping confirmed bookings per resource (invariant; alert on any).
- p99 latency: availability query < 200 ms for a 7-day window; booking < 150 ms.
- Conflict rate (409 ÷ booking attempts) < 2% — higher suggests stale slot lists
  in clients.
- Share of bookings made without human mediation: > 95%.
