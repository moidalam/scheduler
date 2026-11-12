# p95 latency high

**Alert:** `scheduler-latency-p95` · **Severity:** warning · **Labels:** `op`

## What fired and why it matters

The 95th percentile of the edge duration for the named operation has been above
500 ms for 10 minutes. Normal traffic here is single-digit milliseconds, so this
is a real slowdown that callers feel.

## Confirm it is real

On the dashboard's **Latency** row, the p95 for that operation should stay above
500 ms, not spike once. Check that the traffic is not just a small sample: a few
slow requests in a quiet minute can move p95.

## Find the cause

Use the **Time breakdown** and **Layers** rows, which split the time:

- **Database time is most of it:** the **Database p95 latency by operation** panel
  names the slow query. See [db-errors.md](db-errors.md) for `statement_timeout`.
- **Controller logic is most of it:** the controller does its own work after the
  queries. Check the slot calculation for the operation's time window; a large
  window is the usual cause.
- **HTTP overhead is most of it:** the tenant lookup is a query outside the
  controller. If it is slow, the database is slow.

In Tempo, open one slow request's trace. The waterfall shows the span that took
the time.

## Mitigate

- Shrink the work: a smaller availability window, or a query that uses the index.
- If it followed a deploy, roll back and mark the rollback with `make deploy-marker`.

## Resolve and verify

p95 is back under 500 ms for 10 minutes. Add the slow case to the BVTs if it
was not covered.
