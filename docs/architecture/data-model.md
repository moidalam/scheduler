# Data Model

PostgreSQL. Runs on a **single node today**, with a schema that already obeys
the rules a distributed Postgres imposes, so growth is a deployment change
rather than a schema rewrite ([ADR-0009](../adr/0009-shard-ready-data-model.md)).

Related: ADR-0001 (naming), ADR-0002 (datastore), ADR-0003 (time),
ADR-0004 (overlap), ADR-0008 (identifiers). Migrations live in `migrations/`
once implementation begins.

## The three rules every table obeys

1. **The distribution key is `resource_id`** (or, on `resources` itself, `id`).
2. **Every primary key and unique constraint contains the distribution key.**
3. **Foreign keys point only at a co-located table or a reference table.**
   No cross-shard transactions.

On one node these cost nothing. They are what makes the growth path possible.

## Table classes

| Class | Tables | Distributed by |
|---|---|---|
| Distributed | `resources` | `id` |
| Distributed, co-located | `bookings`, `availability_rules`, `availability_exceptions`, `booking_events` | `resource_id` |
| Distributed, secondary index | `resource_aliases` | `alias` |
| Distributed, secondary index | `bookings_by_requester` | `requester_id` |
| Reference (replicated everywhere) | `services`, `tenants` | — |

## Entities

```
tenant 1──* resource 1──* availability_rule
             │             availability_exception
             │
             ├──* booking *──1 service          (co-located by resource_id)
             │       │
             │       ├──* booking_event
             │       └── requester_id            (opaque external reference)
             │
             └──* resource_alias                 (legacy/alternate public ids)
```

---

## Distributed tables

### `resources` — distributed by `id`
| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | UUIDv7, application-generated. Also the public id for resources created here. |
| `tenant_id` | `uuid` | Isolation boundary. |
| `type` | `text` | `trainer`, `room`, `device`… discriminator (ADR-0001). |
| `display_name` | `text` | |
| `timezone` | `text` | IANA name, e.g. `America/Los_Angeles`. Never an offset (ADR-0003). |
| `default_service_id` | `uuid` → `services` | Reference table, so this FK is valid from any node. |
| `active` | `boolean` | Inactive resources yield no availability. |
| `metadata` | `jsonb` | Domain-specific attributes. See API-9. |
| `created_at` / `updated_at` | `timestamptz` | |

`PRIMARY KEY (id)` — the distribution key is the whole key, so this is already
compliant.

### `bookings` — distributed by `resource_id`
| Column | Type | Notes |
|---|---|---|
| `resource_id` | `uuid` | **Distribution key. Leads the primary key.** |
| `id` | `uuid` | UUIDv7, application-generated. |
| `tenant_id` | `uuid` | |
| `requester_id` | `text` | The caller's identifier **verbatim** — `"3"` for user 3. Not a surrogate (PRD R-4, ADR-0008). |
| `service_id` | `uuid` → `services` | Reference table. |
| `starts_at` | `timestamptz` | UTC instant. |
| `ends_at` | `timestamptz` | `CHECK (ends_at > starts_at)` |
| `status` | `text` | `confirmed` \| `cancelled`. A transition, never a delete. |
| `meeting_url` | `text` null | Caller-supplied; this service provisions nothing. |
| `idempotency_key` | `text` null | |
| `metadata` | `jsonb` | |
| `created_at` / `updated_at` | `timestamptz` | |

The public booking id is `id` itself, rendered with a `bkg_` prefix. There is
no separate `external_id`: bookings are created here, never supplied from
outside. Seeded rows are the exception — see [Seeding](#seeding-from-appointmentsjson).

### `availability_rules` — distributed by `resource_id`
Recurring weekly windows in the resource's **local** time.

| Column | Type | Notes |
|---|---|---|
| `resource_id` | `uuid` | Distribution key, leads the PK. |
| `id` | `uuid` | |
| `weekday` | `smallint` | 0=Sunday … 6=Saturday. `CHECK (weekday BETWEEN 0 AND 6)` |
| `starts_at_local` | `time` | e.g. `08:00`. Local civil time, no offset. |
| `ends_at_local` | `time` | e.g. `17:00`. `CHECK (ends_at_local > starts_at_local)` |
| `effective_from` / `effective_to` | `date` null | Lets hours change without rewriting history. |

v1 seed: for each trainer resource, five rows (weekday 1–5), 08:00–17:00.

### `availability_exceptions` *(v1.1)* — distributed by `resource_id`
`resource_id`, `id`, `starts_at`/`ends_at` (`timestamptz`), `kind`
(`block` | `open`), `reason`. Blocks subtract from generated slots; opens add.

### `booking_events` *(addresses API-7)* — distributed by `resource_id`
Append-only audit of every write, in the same transaction as the change — which
is possible precisely because it is co-located with `bookings`.

`resource_id`, `id`, `booking_id`, `actor`, `action`, `at`, `payload jsonb`.

---

## Secondary-index tables

These exist so that a lookup which cannot name a `resource_id` still touches
one node instead of all of them (ADR-0009 §4).

### `resource_aliases` — distributed by `alias`
Resolves a legacy or alternate public identifier to a resource.

| Column | Type | Notes |
|---|---|---|
| `alias` | `text` | **Distribution key.** `"1"` for trainer 1 from the seed file. |
| `tenant_id` | `uuid` | |
| `resource_id` | `uuid` | The resource it resolves to. |

`PRIMARY KEY (alias, tenant_id)` — contains the distribution key.

This is what keeps the brief's `GET /v1/resources/1/availability` working: one
hop to resolve `"1"`, one hop to the resource's own shard. New resources need
no alias row; their public id is their `id`.

### `bookings_by_requester` — distributed by `requester_id`
Answers "all of this requester's bookings across resources" without a fan-out.

| Column | Type | Notes |
|---|---|---|
| `requester_id` | `text` | **Distribution key.** |
| `starts_at` | `timestamptz` | Sort key for the common query. |
| `booking_id` | `uuid` | |
| `resource_id` | `uuid` | Where the authoritative row lives. |
| `tenant_id` | `uuid` | |

`PRIMARY KEY (requester_id, booking_id)`.

Written via a **transactional outbox**, not a cross-shard write: the booking
transaction appends to an outbox on its own shard, and a relay applies it here.
So this table is **eventually consistent** — correct within seconds, not
immediately. `bookings` remains the system of record; this is an index.

---

## Reference tables

Replicated in full to every node, so any shard can join against them. Both are
small and bounded, which is the requirement.

### `services`
| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | UUIDv7. |
| `external_id` | `text` | Caller-facing, e.g. `"default-30m"`. `UNIQUE (tenant_id, external_id)` — enforceable here because reference tables carry no distribution column. |
| `tenant_id` | `uuid` | |
| `name` | `text` | e.g. "30-minute video check-in". |
| `duration_minutes` | `int` | v1 default 30. `CHECK (duration_minutes > 0)` |
| `granularity_minutes` | `int` | Start grid. v1 default 30 → starts on :00/:30. |
| `min_notice_minutes` | `int` | Default 0. Reserved for v1.1. |
| `max_horizon_days` | `int` | Default 60; also caps the availability window. |

### `tenants`
`id`, `name`, `created_at`.

---

## Constraints and indexes

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- The invariant (ADR-0004). resource_id leads, so it is both the distribution
-- key and the equality term: every booking that could conflict with another is
-- on the same node, and this stays a LOCAL constraint after sharding.
ALTER TABLE bookings ADD CONSTRAINT bookings_no_overlap
  EXCLUDE USING gist (
    resource_id WITH =,
    tstzrange(starts_at, ends_at, '[)') WITH &&
  ) WHERE (status = 'confirmed');

ALTER TABLE bookings ADD CONSTRAINT bookings_pkey
  PRIMARY KEY (resource_id, id);

-- Idempotency (ADR-0006). Contains the distribution key.
CREATE UNIQUE INDEX bookings_idempotency
  ON bookings (resource_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

-- The availability and schedule queries.
CREATE INDEX bookings_resource_window
  ON bookings (resource_id, starts_at)
  WHERE status = 'confirmed';

-- Secondary-index tables.
CREATE UNIQUE INDEX resource_aliases_pkey
  ON resource_aliases (alias, tenant_id);
CREATE INDEX bookings_by_requester_window
  ON bookings_by_requester (requester_id, starts_at);

-- Reference table: global uniqueness is enforceable here.
CREATE UNIQUE INDEX services_external_id
  ON services (tenant_id, external_id);
```

The exclusion constraint is the authority for the no-overlap invariant
(ADR-0004); the application pre-check exists only to produce a clean 409 on the
common path.

**Open verification (API-12):** whether a distributed Postgres accepts an
exclusion constraint on a distributed table. The constraint above is correctly
shaped — `resource_id` leads and is the equality term — but shape is not the
same as support. Test against the target platform before relying on it.

## Identifier generation

Per ADR-0008 as amended by ADR-0009 §5:

- UUIDv7, generated in the application, so the id exists before the insert and
  can be written into the outbox and the audit event in one transaction.
- A column `DEFAULT` stays as a backstop so a direct SQL insert cannot produce
  a NULL.
- Public form carries a type prefix: `res_`, `bkg_`, `svc_`.
- No sequences anywhere. They collide the moment there is a second writer.

## The growth path

Nothing below is done now. It is recorded so the schema above is not quietly
broken by a change that makes it impossible.

```sql
SELECT create_reference_table('services');
SELECT create_reference_table('tenants');

SELECT create_distributed_table('resources',            'id');
SELECT create_distributed_table('bookings',             'resource_id',
                                colocate_with => 'resources');
SELECT create_distributed_table('availability_rules',   'resource_id',
                                colocate_with => 'bookings');
SELECT create_distributed_table('booking_events',       'resource_id',
                                colocate_with => 'bookings');

SELECT create_distributed_table('resource_aliases',     'alias');
SELECT create_distributed_table('bookings_by_requester','requester_id');
```

Shard count is set to **65536 logical shards** at the first distribution and
never changed; physical nodes are added by rebalancing shards between them
(ADR-0009 §6).

The syntax above is Citus's. Aurora Limitless and hand-rolled sharding express
the same thing differently — the schema is what makes any of them reachable.

## Seeding from `appointments.json`

The supplied file uses integer `id`, `trainer_id`, `user_id`.

| Seed field | Target |
|---|---|
| `trainer_id` (e.g. `1`) | One `resources` row per distinct value, `type = 'trainer'`, `timezone = 'America/Los_Angeles'`, with a fresh UUIDv7 `id`, **plus** a `resource_aliases` row `alias = "1"` pointing at it. |
| `id` (e.g. `9`) | A `resource_aliases`-style alias is *not* created; the booking keeps its integer as `metadata.legacy_id` and receives a fresh UUIDv7 `id`. Booking ids were never exposed by the brief's endpoints. |
| `user_id` (e.g. `3`) | `bookings.requester_id = "3"` — verbatim text, no surrogate. |
| `started_at` / `ended_at` | `starts_at` / `ends_at`, parsed with their offsets, stored as UTC. |

So `GET /v1/resources/1/availability` — the brief's own call — resolves through
the alias table and works against the seeded data with no translation by the
caller.

The loader is idempotent: `ON CONFLICT DO NOTHING` against
`resource_aliases (alias, tenant_id)` for trainers, and against a
`metadata.legacy_id` unique index for bookings, so a rerun creates neither
duplicate trainers nor duplicate bookings.

**Known dirty record:** seed `id: 9` is 13:00–14:00, a 60-minute booking, which
the 30-minute service forbids. It is loaded verbatim and reported by the
loader's summary rather than rejected (PRD §6/US-5, R-3). It does occupy its
full hour for overlap purposes, so the 13:00 and 13:30 slots are correctly
unavailable.

The seed file lists `ended_at` before `started_at` in each record; field order
carries no meaning and the loader reads by name.
