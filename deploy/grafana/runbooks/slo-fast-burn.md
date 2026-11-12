# SLO fast burn

**Alert:** `scheduler-slo-fast-burn` · **Severity:** critical

## What fired and why it matters

Our failures (5xx) are using the 99.5% error budget about 14 times faster than
allowed, over both the last hour and the last 5 minutes. At this rate the
month's budget is gone in about two days. Clients are getting errors now.

## Confirm it is real

Open the dashboard's **SLO** row. The 1h and 5m burn rates should both be above
14.4. If the 5m figure is back under 1, this is ending, not ongoing: keep
watching, but this is no longer urgent.

## Find the cause

1. **Which operation?** In the **Errors** section, *Our failures (5xx) by operation*
   names it. Note that operation.
2. **Which layer?** In *Our failures by layer and Postgres class*: a repository row
   means the database; a controller row means our logic.
3. **Why?** In Loki, search the failures for that operation:
   ```
   {service_name="scheduler"} | severity_text = "ERROR"
   ```
   The line names the error. The `sqlstate` field, if present, names the database failure.
4. **One request, end to end.** Copy a `trace_id` from a failing line into Tempo.
   The span that is red is where it broke.

## Mitigate

- **Database errors** (`sqlstate` present): see [db-errors.md](db-errors.md).
- **Bad deploy:** the failures started at a deploy marker. Roll back to the last
  good commit with the usual deploy process, then run `make deploy-marker`
  with the rollback so the dashboard shows it.
- **Traffic spike** on one operation: reduce it at the caller. Do not raise
  the limits without a fix.

## Resolve and verify

The 5m burn rate is under 1 and the alert clears. Keep the incident open until
the 1h burn rate is also under 1, then write the postmortem.
