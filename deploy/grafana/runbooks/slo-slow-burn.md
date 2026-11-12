# SLO slow burn

**Alert:** `scheduler-slo-slow-burn` · **Severity:** warning

## What fired and why it matters

Our failures are using the error budget about 6 times faster than allowed, over
both the last 6 hours and the last 30 minutes. Nothing is on fire, but at this
rate the monthly budget runs out in about five days. A steady leak like this is
usually one bug, not many.

## Confirm it is real

On the dashboard's **SLO** row, the 6h and 30m burn rates should both be above 6.
If the 30m figure has dropped under 1, the leak has stopped. Check it again
tomorrow before you close it.

## Find the cause

Start with the same steps as [slo-fast-burn.md](slo-fast-burn.md), step 1 and 2:
which operation, and which layer. A slow leak usually sits on one operation.
Then look at the **Our failures** table for its `sqlstate` or `error_code`
pattern over the last day, not just the last hour.

## Mitigate

Usually none is urgent. Open a ticket with the operation, the layer and the
error class, and fix it in working hours. If it turns into a fast burn, follow
[slo-fast-burn.md](slo-fast-burn.md).

## Resolve and verify

Both the 6h and 30m burn rates are under 1. Record the leak and the fix in the
ticket.
