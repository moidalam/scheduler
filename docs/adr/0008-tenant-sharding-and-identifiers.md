# ADR-0008 — Tenant-scoped, shard-ready data model with bigint identity ids

- **Status:** Accepted
- **Date:** 2026-11-12
- **Relates to:** ADR-0001, ADR-0004, ADR-0009

## Context

The service starts on one PostgreSQL instance but must not be a one-node
design: data may outgrow a single server, and sharding cannot be bolted on
cheaply. Two things are near-impossible to retrofit once production data
exists: the shape of primary keys, and the identifier scheme. Both cost nothing
to get right now, while the deployment itself stays on one node.

The supplied data identifies trainers, users and appointments by small
integers, and the brief's endpoints take those integers directly.

## Decision

**1. The tenant is the top-level entity and the shard key.** Every table except
`tenants` carries `tenant_id` and is distributed by it. All of a tenant's
resources, requesters, services, availability rules and bookings therefore live
together, so every hot path is single-shard: availability, booking creation,
schedule listing, "all of a requester's bookings", and the no-overlap
constraint (ADR-0004).

**2. `tenant_id` leads every primary key and unique constraint.**

```sql
PRIMARY KEY (tenant_id, id)
UNIQUE      (tenant_id, idempotency_key)
FOREIGN KEY (tenant_id, resource_id) REFERENCES resources (tenant_id, id)
```

This is valid, useful SQL on a single node and is the exact shape a distributed
Postgres requires. Foreign keys reference only rows of the same tenant, so none
ever crosses a shard.

**3. Identifiers are `bigint` identity columns.** They are unique within a
tenant, never exposed as meaningful across tenants, and the integers in the
supplied file load as the real ids (inserted explicitly, then the sequences
advanced past them). There are no UUIDs and no external-id or alias tables.

**4. `tenants` is a reference table**, replicated to every node when sharded.

**5. A transaction touches one tenant.** No cross-shard transactions, no
cross-tenant queries on any request path.

### The growth path (not performed now)

```sql
SELECT create_reference_table('scheduler.tenants');
SELECT create_distributed_table('scheduler.resources', 'tenant_id');
SELECT create_distributed_table('scheduler.bookings', 'tenant_id',
                                colocate_with => 'scheduler.resources');
-- ...and the remaining tenant-keyed tables, co-located the same way.
```

Citus syntax is shown for concreteness; Aurora Limitless and hand-rolled
sharding impose the same rules.

## Alternatives considered

| Option | Why not |
|---|---|
| Shard by `resource_id` | Finer grain and no large-tenant hotspot, but needs alias and secondary-index tables, an outbox for requester-centric reads, and forbids multi-resource bookings. Rejected for complexity. |
| UUID primary keys | Solve cross-node uniqueness but cost index locality and readability, and the tenant-scoped key already makes node-local ids sufficient. |
| Single-node schema, shard later | Key shape and id scheme cannot be retrofitted cheaply. |
| Shard by time | Scatters one resource's bookings and breaks the overlap constraint. |

## Consequences

**Positive** — one node today with no extension; the growth path is a set of
`create_distributed_table` calls rather than a schema rewrite; no secondary-index
tables, no outbox, no eventual consistency; a tenant is trivially exportable,
deletable and backed up as a unit.

**Negative / accepted costs**
- **A tenant cannot be split across nodes.** One very large tenant is a
  hotspot. Acceptable only if tenants are many and bounded; a platform-wide pool
  modelled as a single tenant would defeat this design. Large tenants can be
  moved to dedicated nodes.
- **Identity sequences assume a single allocator.** Node-local sequences can
  collide when a tenant moves between nodes. Safe while one coordinator
  allocates ids; must be verified on the target platform.
- **Integer ids are guessable.** Acceptable because they are tenant-scoped and
  authorization is a separate layer (known-issues API-1, API-2).
- Composite keys are wordier, and every query must carry `tenant_id` to stay
  single-shard. The discipline must be held before anything tells us we broke it.

**Unverified** — whether a distributed Postgres accepts the ADR-0004 exclusion
constraint on a distributed table (API-12).

**Revisit when** — a single tenant approaches a node's capacity, or a booking
must span tenants.
