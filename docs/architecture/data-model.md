# Data Model

Postgres 16. See ADR-0001 (naming), ADR-0002 (datastore), ADR-0003 (time),
ADR-0004 (overlap). This is the intended target schema; migrations live in
`migrations/` once implementation begins.

## Entities

```
tenant 1──* resource 1──* availability_rule
                │
                ├──* availability_exception      (v1.1)
                └──* booking *──1 service
                       │
                       └── requester_id (opaque external reference)
```

## Tables

### `resources`
| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | |
| `tenant_id` | `uuid` | Isolation boundary; single default tenant in v1. |
| `type` | `text` | `trainer`, `room`, `device`… discriminator (ADR-0001). |
| `display_name` | `text` | |
| `timezone` | `text` | IANA name, e.g. `America/Los_Angeles`. Never an offset. |
| `default_service_id` | `uuid` FK → `services` | |
| `active` | `boolean` | Inactive resources yield no availability. |
| `metadata` | `jsonb` | Domain-specific attributes. |
| `created_at` / `updated_at` | `timestamptz` | |

### `services`
| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | |
| `tenant_id` | `uuid` | |
| `name` | `text` | e.g. "30-minute video check-in". |
| `duration_minutes` | `int` | v1 default 30. `CHECK (duration_minutes > 0)` |
| `granularity_minutes` | `int` | Start grid. v1 default 30 → starts on :00/:30. |
| `min_notice_minutes` | `int` | Default 0. Reserved for v1.1. |
| `max_horizon_days` | `int` | Default 60; also caps the availability window. |

### `availability_rules`
Recurring weekly windows in the resource's **local** time.

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | |
| `resource_id` | `uuid` FK | |
| `weekday` | `smallint` | 0=Sunday … 6=Saturday. `CHECK (weekday BETWEEN 0 AND 6)` |
| `starts_at_local` | `time` | e.g. `08:00`. Local civil time, no offset. |
| `ends_at_local` | `time` | e.g. `17:00`. `CHECK (ends_at_local > starts_at_local)` |
| `effective_from` / `effective_to` | `date` nullable | Lets hours change without rewriting history. |

v1 seed: for each trainer resource, five rows (weekday 1–5), 08:00–17:00.

### `availability_exceptions` *(v1.1, listed for shape)*
`resource_id`, `starts_at`/`ends_at` (`timestamptz`), `kind` (`block` | `open`),
`reason`. Blocks subtract from generated slots; opens add to them.

### `bookings`
| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | |
| `tenant_id` | `uuid` | |
| `resource_id` | `uuid` FK | |
| `requester_id` | `uuid` | Opaque external reference (PRD R-4). |
| `service_id` | `uuid` FK | |
| `starts_at` | `timestamptz` | UTC instant. |
| `ends_at` | `timestamptz` | `CHECK (ends_at > starts_at)` |
| `status` | `text` | `confirmed` \| `cancelled`. Cancellation is a transition, not a delete. |
| `meeting_url` | `text` null | Caller-supplied. |
| `idempotency_key` | `text` null | |
| `legacy_id` | `bigint` null | Preserves the seed file's integer ids. |
| `metadata` | `jsonb` | |
| `created_at` / `updated_at` | `timestamptz` | |

**Constraints and indexes**

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE bookings ADD CONSTRAINT bookings_no_overlap
  EXCLUDE USING gist (
    resource_id WITH =,
    tstzrange(starts_at, ends_at, '[)') WITH &&
  ) WHERE (status = 'confirmed');

CREATE UNIQUE INDEX bookings_idempotency
  ON bookings (resource_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

CREATE UNIQUE INDEX bookings_legacy_id
  ON bookings (legacy_id) WHERE legacy_id IS NOT NULL;

CREATE INDEX bookings_resource_window
  ON bookings (resource_id, starts_at)
  WHERE status = 'confirmed';
```

The exclusion constraint is the authority for the no-overlap invariant
(ADR-0004); the application pre-check exists only to produce a clean 409 on the
common path.

## Seeding from `appointments.json`

The supplied file uses integer `id`, `trainer_id`, `user_id`. The loader maps:

| Seed field | Target |
|---|---|
| `id` | `bookings.legacy_id` (a fresh `uuid` becomes `id`) |
| `trainer_id` | `resources` — a resource of type `trainer` is created per distinct value, deterministically from the integer so reruns are stable |
| `user_id` | `bookings.requester_id` — likewise deterministic |
| `started_at` / `ended_at` | `starts_at` / `ends_at`, parsed with their offsets, stored as UTC |

The loader is idempotent via `ON CONFLICT (legacy_id) DO NOTHING`.

**Known dirty record:** seed `id: 9` is 13:00–14:00, a 60-minute booking, which
the 30-minute service forbids. It is loaded verbatim and reported by the
loader's summary rather than rejected (PRD §6/US-5, R-3). It does occupy its
full hour for overlap purposes, so the 13:00 and 13:30 slots are correctly
unavailable.

The seed file's `ended_at` is listed before `started_at` in each record; field
order carries no meaning and the loader reads by name.
