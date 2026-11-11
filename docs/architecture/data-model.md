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
| `id` | `uuid` PK | Internal key. Never required of a caller. |
| `external_id` | `text` | The caller's own identifier, verbatim — `"1"` for trainer 1. Unique per tenant. Public ids resolve here first (ADR-0008). |
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
| `id` | `uuid` PK | Internal key. |
| `external_id` | `text` | Caller-facing identifier, e.g. `"default-30m"`. Unique per tenant. |
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
| `id` | `uuid` PK | Internal key. |
| `external_id` | `text` | Caller-facing booking id. Seeded from the file's integer `id` (`"9"`). Unique per tenant. |
| `tenant_id` | `uuid` | |
| `resource_id` | `uuid` FK | |
| `requester_id` | `text` | The caller's identifier **verbatim** — `"3"` for user 3. Not a surrogate: this service does not own requesters (PRD R-4, ADR-0008). |
| `service_id` | `uuid` FK | |
| `starts_at` | `timestamptz` | UTC instant. |
| `ends_at` | `timestamptz` | `CHECK (ends_at > starts_at)` |
| `status` | `text` | `confirmed` \| `cancelled`. Cancellation is a transition, not a delete. |
| `meeting_url` | `text` null | Caller-supplied. |
| `idempotency_key` | `text` null | |
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

CREATE INDEX bookings_resource_window
  ON bookings (resource_id, starts_at)
  WHERE status = 'confirmed';

-- Public-id resolution (ADR-0008). One per table that is addressed externally.
CREATE UNIQUE INDEX resources_external_id ON resources (tenant_id, external_id);
CREATE UNIQUE INDEX services_external_id  ON services  (tenant_id, external_id);
CREATE UNIQUE INDEX bookings_external_id  ON bookings  (tenant_id, external_id);

CREATE INDEX bookings_requester ON bookings (tenant_id, requester_id);
```

The exclusion constraint is the authority for the no-overlap invariant
(ADR-0004); the application pre-check exists only to produce a clean 409 on the
common path.

## Seeding from `appointments.json`

The supplied file uses integer `id`, `trainer_id`, `user_id`. The loader maps:

| Seed field | Target |
|---|---|
| `id` (e.g. `9`) | `bookings.external_id = "9"`. A fresh `uuid` becomes `bookings.id`. |
| `trainer_id` (e.g. `1`) | One `resources` row per distinct value, `type = 'trainer'`, `external_id = "1"`, `timezone = 'America/Los_Angeles'`. Its `uuid` key is generated, not derived. |
| `user_id` (e.g. `3`) | `bookings.requester_id = "3"` — stored verbatim as text, no surrogate (ADR-0008). |
| `started_at` / `ended_at` | `starts_at` / `ends_at`, parsed with their offsets, stored as UTC |

Every integer is stored as its decimal string, so `GET /v1/resources/1/availability`
— the brief's own call — works against the seeded data without translation.

The loader is idempotent via `ON CONFLICT (tenant_id, external_id) DO NOTHING`
on each table, so a rerun creates neither duplicate bookings nor duplicate
trainers.

**Known dirty record:** seed `id: 9` is 13:00–14:00, a 60-minute booking, which
the 30-minute service forbids. It is loaded verbatim and reported by the
loader's summary rather than rejected (PRD §6/US-5, R-3). It does occupy its
full hour for overlap purposes, so the 13:00 and 13:30 slots are correctly
unavailable.

The seed file's `ended_at` is listed before `started_at` in each record; field
order carries no meaning and the loader reads by name.
