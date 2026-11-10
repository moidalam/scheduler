# Domain Glossary

The original brief is phrased in trainer/client terms. This system is modelled
domain-agnostically so the same core serves trainers, clinicians, advisors,
support agents, equipment, or rooms. The mapping is fixed here once and used
everywhere else.

| Generic term | Trainer-domain example | Definition |
|---|---|---|
| **Resource** | Trainer | The bookable entity whose time is consumed. Exactly one resource per booking in v1. |
| **Requester** | Client / user | The party asking for the booking. Identified by `requester_id`. |
| **Service** | "30-minute video check-in" | A named, bookable offering. Carries duration, granularity, and (later) buffers and price. |
| **Availability rule** | "Mon–Fri 8am–5pm Pacific" | A recurring window in which a resource's slots may be generated. |
| **Exception** | PTO, holiday, one-off extra hours | A dated override that adds or removes availability. (v2) |
| **Slot** | "2026-11-10 09:00–09:30 PT" | A candidate time interval derived from availability rules minus existing bookings. Slots are computed, never stored. |
| **Booking** | Appointment | A confirmed, persisted reservation of a slot by a requester against a resource. |
| **Schedule window** | The `starts_at`/`ends_at` of a query | The half-open interval `[starts_at, ends_at)` a caller asks about. |
| **Tenant** | The gym / clinic / firm | Isolation boundary for resources, services, and bookings. (v2, but keyed for now.) |

## Conventions used throughout

- **All intervals are half-open**: `[starts_at, ends_at)`. Two bookings that
  touch at an endpoint do **not** overlap.
- **All timestamps on the wire are RFC 3339 with an explicit offset**
  (`2026-11-10T09:00:00-08:00`). All timestamps at rest are UTC `timestamptz`.
- **Business rules are evaluated in the resource's local timezone**, not the
  caller's and not the server's. Pacific is a configuration value, not a
  constant in code.
- **"User" is avoided** as a domain word. It means an authenticated principal
  of the API, which may be a requester, a resource owner, or an admin.
