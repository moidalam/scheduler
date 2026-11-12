# Build-verification tests (BVT)

These tests run against a **live stack** over real HTTP, with no mocks. Each
request is checked three ways at once:

| What | Where it is checked |
|---|---|
| The API result (status, stable `code`, body) | the HTTP response |
| The log lines the request wrote | Loki, found by `trace_id` |
| The metrics each layer recorded | Prometheus, `operation_total` and `operation_duration_milliseconds` |
| The spans in the request's trace | Tempo, found by `trace_id` |

Run them with:

```bash
make bvt
```

This starts the stack with `docker compose up -d --build`, waits for `/healthz`,
and runs the tests. To run against another stack, set `BVT_BASE_URL`,
`BVT_LOKI_URL`, `BVT_PROMETHEUS_URL` and `BVT_TEMPO_URL`.

## What is covered

- Availability: free slots are listed, bad windows are refused, unknown resources are 404.
- Booking: create, read back, list, conflict, idempotent replay, rule violations (422 with the rule named), unknown requester and booking, overlong idempotency key.
- Tenant: a malformed or unknown `X-Tenant-ID` is refused.
- Telemetry: a successful booking appears in every layer, a conflict is logged at Info and never counted as an error, a 404 is never counted as an error, and every response names its request and trace.

## Rules for these tests

- Each run books a fresh slot found from availability, so runs don't depend on each other.
- Metrics are checked for presence and for not having recorded errors, not for exact deltas. Counters reset when the app restarts, so a before/after comparison on a shared stack is unreliable. Logs and traces are matched by a unique trace id, so they are exact.
- The telemetry collector batches exports, so checks wait up to 60 seconds. A failure there means the data never arrived.
