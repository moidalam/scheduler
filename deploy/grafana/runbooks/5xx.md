# Our failures (5xx)

**Alert:** `scheduler-5xx` · **Severity:** warning · **Labels:** `op`

## What fired and why it matters

The named operation returned a 5xx response in the last 5 minutes. A 5xx is our
failure, never the caller's. Any single one is worth a look, because it means a
request we should have served was not.

## Confirm it is real

Check the **Our failures** panel for that operation. A one-off in a 5-minute
window can be a transient. If it recurs over a few minutes it is real.

## Find the cause

1. Open the failing request in Loki:
   ```
   {service_name="scheduler"} | op = "<operation>" | severity_text = "ERROR"
   ```
2. Read the line. The message says where it broke: "database operation failed"
   means the store, "operation failed" means the controller, and "unhandled error"
   means the edge.
3. Open the `trace_id` in Tempo to see which span failed.

## Mitigate

Depends on the cause. A database cause is in [db-errors.md](db-errors.md). An
unhandled error in our code is a bug: if it is recent, roll back the deploy.

## Resolve and verify

The alert clears when no 5xx has appeared for the operation in 5 minutes. Add a
regression test for the cause before you close the ticket.
