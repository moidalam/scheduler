# ADR-0006 — Resource-oriented JSON over HTTP, OpenAPI-first, versioned in the path

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

The brief asks for an HTTP JSON API. The contract will outlive the first
client, so error semantics, versioning and write safety are worth fixing now.

## Decision

- **Style:** resource-oriented REST with JSON bodies, `snake_case` fields.
- **OpenAPI first.** `openapi/spec.yaml` is the source of truth. Server stubs
  and request/response types are generated with `oapi-codegen` in strict-server
  mode, so the handlers implement a generated interface and the code cannot
  drift from the contract. The spec is **OpenAPI 3.0.3**, not 3.1, because that
  is what the generator supports reliably.
- **Versioning:** `/v1` path prefix. Additive changes are not breaking; a
  breaking change gets `/v2`.
- **Identifiers:** JSON integers (`int64`). They are unique within a tenant and
  never meaningful across tenants (ADR-0008).
- **Tenant:** carried in the `X-Tenant-ID` header, never in the path or body
  (ADR-0009).
- **Errors:** RFC 9457 `application/problem+json` with a stable
  machine-readable `code`. Clients branch on `code`, never on prose.
- **Status codes:** `400` malformed request or parameters; `404` unknown
  resource or tenant; `409` the slot is taken; `422` well-formed but violates a
  scheduling rule (outside hours, misaligned, wrong duration, in the past). 409
  and 422 are kept apart because they tell a client different things: "pick
  another time" versus "this request is wrong".
- **Idempotency:** `POST /v1/bookings` accepts an `Idempotency-Key` header; a
  repeat returns the original booking with `200`.
- **Pagination:** keyset (`limit` plus an opaque `next_cursor`) on list
  endpoints. Offsets skip and repeat rows under concurrent inserts.

## Alternatives considered

| Option | Why not |
|---|---|
| gRPC | The first clients are web and mobile and the brief asks for HTTP JSON. |
| GraphQL | Solves an over-fetching problem this API does not have. |
| Hand-written handlers and a separate spec | The two drift; generation makes drift a compile error. |
| OpenAPI 3.1 | Weaker generator support today. |
| Header or query-parameter versioning | Harder to see in logs and to route on. |
| Bare `{"error": "..."}` | Forces clients to parse prose. |

## Consequences

**Positive** — predictable to integrate; errors are actionable and stable;
retries are safe; the contract cannot silently diverge from the code.

**Negative / accepted costs** — a code generation step in the build; a 3.0
spec cannot use 3.1-only features such as type arrays.

**Revisit when** — a client needs server push, at which point webhooks or SSE
are added beside this API, not instead of it.
