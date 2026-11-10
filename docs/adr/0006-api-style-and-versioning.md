# ADR-0006 — Resource-oriented JSON over HTTP, versioned in the path

- **Status:** Accepted
- **Date:** 2026-11-10

## Context

The brief asks for an HTTP JSON API with three endpoints. The contract will
outlive the first client, so error semantics, versioning, and write safety are
worth fixing now; they are expensive to retrofit.

## Decision

- **Style:** resource-oriented REST, JSON request and response bodies.
- **Versioning:** path prefix `/v1`. Additive changes are not breaking; breaking
  changes get `/v2`, with both served during migration.
- **Naming:** `snake_case` fields, matching the seed data's existing shape.
- **Errors:** RFC 9457 `application/problem+json`, always carrying a stable
  machine-readable `code` alongside human text. Clients branch on `code`, never
  on prose.
- **Status codes:** `400` malformed request/parameters; `404` unknown resource;
  `409` the slot is taken; `422` well-formed but violates a scheduling rule
  (outside hours, misaligned, wrong duration, in the past). Separating 409 from
  422 lets a client distinguish "pick another time" from "this request is wrong".
- **Idempotency:** `POST` accepts an `Idempotency-Key` header; a repeat returns
  the original booking.
- **Pagination:** cursor-based (`limit` + `next_cursor`) on list endpoints. Not
  offsets — they skip and repeat rows under concurrent inserts.
- **Contract:** `docs/api/openapi.yaml` is the source of truth, reviewed in PRs
  alongside the code.

## Alternatives considered

| Option | Why not |
|---|---|
| gRPC | Better typing, but the first clients are web/mobile and the brief asks for HTTP JSON. An optional gRPC surface remains possible later. |
| GraphQL | Solves an over-fetching problem this API does not have; complicates idempotency and caching. |
| Header or query-param versioning | Harder to see in logs and to route on; path versioning is the least surprising. |
| Bare `{"error": "..."}` | Forces clients to parse prose, which breaks the day the prose is improved. |

## Consequences

**Positive** — predictable to integrate; errors are actionable and stable;
retries are safe; the spec is testable and generates clients.

**Negative / accepted costs** — more up-front surface than three ad-hoc
handlers; the OpenAPI document must be kept honest, enforced by contract tests
in CI rather than by discipline.

**Revisit when** — a client needs server-push (availability changing live), at
which point webhooks or SSE are added beside, not instead of, this API.
