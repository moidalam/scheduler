# Domain Glossary

This service is built domain-agnostically to serve trainers, clinicians, advisors,
support agents, equipment, or rooms. The mapping from the original brief's
trainer/client terms is defined once below and reused throughout.

| Generic term | Trainer-domain example | Definition |
|---|---|---|
| **Resource** | Trainer | The bookable entity whose time is consumed. Exactly one resource per booking in v1. |
| **Requester** | Client / user | The party asking for the booking. A row in `requesters`, identified by `requester_id`. |
| **Service** | "30-minute video check-in" | A named, bookable offering. Carries duration, granularity, and (later) buffers and price. |
| **Availability rule** | "Mon–Fri 8am–5pm Pacific" | A recurring window in which a resource's slots may be generated. |
| **Exception** | PTO, holiday, one-off extra hours | A dated override that adds or removes availability. (v2) |
| **Slot** | "2026-11-10 09:00–09:30 PT" | A candidate time interval derived from availability rules minus existing bookings. Slots are computed, never stored. |
| **Booking** | Appointment | A confirmed, persisted reservation of a slot by a requester against a resource. |
| **Schedule window** | The `starts_at`/`ends_at` of a query | The half-open interval `[starts_at, ends_at)` a caller asks about. |
| **Tenant** | The gym / clinic / firm | The customer that owns a set of data, and the shard key (ADR-0008). Every request is scoped to one, read from `X-Tenant-ID` (ADR-0009). |

## Conventions used throughout

- **All intervals are half-open**: `[starts_at, ends_at)`. Two bookings that
  touch at an endpoint do **not** overlap.
- **All timestamps on the wire are RFC 3339 with an explicit offset**
  (`2026-11-10T09:00:00-08:00`). All timestamps at rest are UTC `timestamptz`.
- **Business rules are evaluated in the resource's local timezone**, not the
  caller's and not the server's. Pacific is a configuration value, not a
  constant in code.
- **Identifiers are integers**, unique within a tenant and meaningless across
  tenants. The brief's `trainer_id` and `user_id` are `resource_id` and
  `requester_id` here.
- **"User" is avoided** as a domain word. It means an authenticated principal
  of the API, which may be a requester, a resource owner, or an admin.
