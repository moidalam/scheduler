# ADR-0010: Telemetry with OpenTelemetry, shipped to a local observability stack

**Status:** Accepted · **Date:** 2026-11-12

## Context

The service needs logs, metrics and traces that can be explored in a UI and that
support dashboards and monitors. The stack runs in Docker for development, and
it must be free. `payments-api` uses OpenTelemetry in production, with a private
logging library, but none of its backends are available here.

## Decision

1. **OpenTelemetry is the only telemetry API in business code.** The SDK and the
   OTLP/HTTP exporters are configured once, in `internal/observability`. Nothing
   else imports an exporter.
2. **Local backend: `grafana/otel-lgtm`**, one container bundling an OTel
   collector, Prometheus, Loki, Tempo and Grafana. Its UI is on port 3000,
   bound to `127.0.0.1`. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to send telemetry;
   leave it empty to keep logs on stdout only.
3. **One request, one trace.** `otelhttp` opens the span at the edge, and every
   layer logs and measures inside it. Each log line carries `request_id`,
   `tenant_id`, `trace_id` and `span_id`. The response returns `X-Request-Id`
   and `X-Trace-Id`.
4. **Logs are written where the failure starts.** The observed Store logs
   unexpected database errors. The controller logs rejections (Info for 404,
   409 and 422, Warn for 400, 401, 403 and 429). The edge logs one summary line
   per request. Errors already logged at their origin are marked, so the edge
   does not repeat them (ADR-0002, adapted to a service with fewer layers).
5. **One metric shape for every layer:** `operation.total` (counter) and
   `operation.duration` (histogram, milliseconds), labelled `layer`,
   `component`, `op` and `result`. Buckets are 1 ms to 10 s. Tags are bounded:
   `error_code`, `sqlstate`, `status_family`, `outcome`. No tenant, resource or
   request ids are metric labels.

## Consequences

- Any layer's latency, errors and rejections can be charted with the same
  queries, and a trace shows where time went inside one request.
- Dashboards and monitors are not written yet. They come next, from the
  metrics above.
- Logs and traces are kept only as long as the container runs. Durable storage
  and retention are a deployment decision, not made here.
- `sloglint` and a redaction rule for `metadata` and `meeting_url` are not
  enforced by tooling yet. Both are rules to follow in review.
