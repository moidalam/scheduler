# Runbooks

One page per alert. Each alert's `runbook` annotation points here. Every
runbook has the same shape, so you can read any of them under pressure:

1. **What fired and why it matters**: what the alert measures and who it affects.
2. **Confirm it is real**: the first check, to rule out a false alarm.
3. **Find the cause**: where to look on the dashboard, and the log and trace queries.
4. **Mitigate**: what to do now, before the root cause is known.
5. **Resolve and verify**: how to know it is over.

Queries use the labels the service emits (see ADR-0010): `layer`, `op`,
`result`, `error_code`, `sqlstate`. Logs are in Loki under
`{service_name="scheduler"}`, and every line carries `request_id`, `trace_id` and
`tenant_id`. Open a trace id in Tempo to see the spans.

Dashboard: <http://localhost:3000/d/scheduler-overview>

| Alert | Severity | Runbook |
|---|---|---|
| SLO fast burn | critical | [slo-fast-burn.md](slo-fast-burn.md) |
| SLO slow burn | warning | [slo-slow-burn.md](slo-slow-burn.md) |
| Our failures (5xx) | warning | [5xx.md](5xx.md) |
| Database errors | critical | [db-errors.md](db-errors.md) |
| p95 latency high | warning | [latency-p95.md](latency-p95.md) |
| Booking conflicts spiking | warning | [conflict-spike.md](conflict-spike.md) |
| No metrics from the service | critical | [telemetry-gap.md](telemetry-gap.md) |
