# No metrics from the service

**Alert:** `scheduler-telemetry-gap` · **Severity:** critical

## What fired and why it matters

The service has sent no request metrics for 10 minutes. Two things can cause
this, and both mean every other alert is blind: the service is down, or it is
running but cannot reach the telemetry collector.

## Confirm it is real

1. Is the service answering? `curl -fsS http://localhost:8080/healthz`.
   - If it does not answer, the service is down. Go to step 3.
   - If it answers, it is running but not reporting. Go to step 2.
2. Is the collector up? `docker compose ps otel-lgtm` should say healthy. Check
   `OTEL_EXPORTER_OTLP_ENDPOINT` in the app's environment points at it.
3. Are there recent app logs? `docker compose logs app --tail 50`. A crash loop
   or a failed migration shows here.

## Find the cause

- **Service down:** the logs in step 3 say why. Common causes are a failed
  migration at startup or a database that is not reachable.
- **Collector down:** the app keeps running and logs to stdout, but nothing
  reaches Grafana. Restart the collector container.
- **Service healthy but silent:** nothing is sending requests. That is only a
  problem if traffic should be flowing; check the traffic panels.

## Mitigate

Restart the failing container: `docker compose restart app` or
`docker compose restart otel-lgtm`. If the app will not start, roll back the
last deploy.

## Resolve and verify

`make bvt` passes, and the **API requests** stat on the dashboard is counting
again. The alert clears within a minute of the first new metric.
