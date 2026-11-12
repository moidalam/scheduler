# Booking conflicts spiking

**Alert:** `scheduler-conflict-spike` · **Severity:** warning

## What fired and why it matters

More than 10% of booking attempts over 15 minutes found the slot already taken,
across at least 20 attempts. The service is working correctly: each conflict is
a clean 409 and nothing was double-booked. The problem is the clients, which are
choosing slots that are no longer free.

## Confirm it is real

On the dashboard's **Booking funnel** row, check the *Refused* figure and the
*Booking attempts over time* chart. A spike at one moment is usually a single
busy client. A steady rise is the client's availability view going stale.

## Find the cause

1. Which clients? In Loki, the conflicts carry a `tenant_id` and a `request_id`:
   ```
   {service_name="scheduler"} |~ "^request rejected$" | error_code = "slot_unavailable"
   ```
   Use the **Tenant drill-down** to see if one tenant is responsible.
2. Is the availability view cached? If clients fetch slots once and book
   minutes later, they will conflict. That is the client's cache, not ours.
3. Did something open a lot of slots at once? Check the availability panel
   for a jump in the free-slot count.

## Mitigate

- Tell the client team to refetch availability immediately before booking.
- If one client is hammering one slot, that is a bot or a bug: rate limit it
  at the edge, as the known-issues list (API-6) describes.

## Resolve and verify

The conflict rate is under 10%, or the volume is under 20 attempts. No code
change is expected; this is a client-side fix.
