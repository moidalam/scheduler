# Database errors

**Alert:** `scheduler-db-errors` · **Severity:** critical · **Labels:** `op`, `sqlstate`

## What fired and why it matters

A repository query failed, not just returned no rows, in the last 5 minutes. The
`sqlstate` label says which kind of failure. Requests that touch that query are
failing or about to.

## Confirm it is real

Check the **Our failures by layer and Postgres class** table. The `sqlstate` value
decides how serious this is:

| sqlstate | Meaning | Usual action |
|---|---|---|
| `deadlock` | Two transactions blocked each other | Usually transient. If it recurs, check booking concurrency: the advisory lock should prevent it (ADR-0004). |
| `serialization_failure` | Concurrent write conflict | Usually transient; the caller can retry. |
| `statement_timeout` | A query ran too long | Check the query plan and recent table growth. |
| `exclusion_violation` | Overlap rejected by the constraint | Expected in a race, and mapped to a 409. It should not be a database error at all: file a bug. |
| `other` | Anything else, such as a dropped connection or a missing table | Check the database is up and the schema matches the migrations. |

## Find the cause

1. Postgres health: `make psql`, then `select 1;`. If it does not answer, the
   database is down or unreachable.
2. The log lines for the failing query:
   ```
   {service_name="scheduler"} | op = "<op>" | sqlstate != ""
   ```
   The `error` field has the full database message.
3. Long-running queries on the database: `select pid, now() - query_start, query from pg_stat_activity where state <> 'idle';`

## Mitigate

- **Database down or unreachable:** restart the database container, then the
  app. Migrations are forward-only, so do not restart mid-migration.
- **`statement_timeout` on one query:** find the query and add the index it
  needs, or stop the caller that runs it.
- **Deadlocks under load:** reduce concurrent writers to one resource, or retry at the caller.

## Resolve and verify

The alert clears when no new repository error appears in 5 minutes. Confirm with
the BVTs: `make bvt`.
