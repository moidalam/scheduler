# ADR-0005 — Compute availability on read rather than materialising slots

- **Status:** Accepted
- **Date:** 2026-11-10

## Context

Availability can be served either by generating candidate slots on each query
from rules minus bookings, or by writing a row per slot ahead of time and
marking rows taken.

## Decision

Compute on read. Availability = (slots generated from the resource's rules and
the service's grid, over the requested window) − (existing confirmed bookings) −
(past slots). No slot rows are persisted.

The query window is capped (default 60 days) so the generated set stays bounded.

## Alternatives considered

| Option | Why not |
|---|---|
| Materialise slot rows | Requires a generator job running into the future, back-fill when rules change, and garbage collection of the past. A rule edit becomes a data migration. |
| Materialise with a cache-invalidation layer | All the above, plus a cache to keep coherent with the booking write path. |

## Consequences

**Positive** — changing an availability rule takes effect immediately with no
back-fill; nothing can drift out of sync with the rules; far less state to own.
The booking table stays the single source of truth.

**Negative / accepted costs** — availability cost is proportional to window
size, hence the cap; a client's slot list can go stale between read and book,
which is why booking re-validates every rule server-side and may return 409
(US-2). Clients must handle 409 as a normal outcome, not an error condition.

**Revisit when** — p99 on the availability endpoint approaches the 200 ms
budget, or a "find the first free slot across 500 resources" query appears. The
first response would be a short-TTL read cache keyed by
`(resource_id, window, rules_version)`, not materialisation.
