# ADR-0009 — Tenant context is read from the `X-Tenant-ID` header

- **Status:** Accepted
- **Date:** 2026-11-12
- **Relates to:** ADR-0006, ADR-0008

## Context

Every query must be scoped by tenant (ADR-0008), so every request needs a tenant.
The brief specifies no authentication, so there is no token to take it from.

## Decision

- One middleware reads `X-Tenant-ID`, parses a positive integer, confirms the
  tenant exists, and stores it in the request context.
- Every query takes `tenant_id` from that context. It is never read from the
  path or the body, so a caller cannot send a body that disagrees with the
  header.
- A missing or malformed header returns `400` with code `missing_tenant`; an
  unknown tenant returns `404` with code `unknown_tenant`.
- An optional `DEFAULT_TENANT_ID` setting supplies a tenant when the header is
  absent. It is **unset by default** and set to `1` only in the Compose file,
  so the sample can be exercised with plain `curl` while a real deployment fails
  closed.

## Alternatives considered

| Option | Why not |
|---|---|
| Tenant in the URL path | Repeats it in every route and invites a mismatch with the body. |
| Subdomain per tenant | Needs DNS and TLS machinery out of proportion to a sample. |
| Always default to tenant 1 | Hides routing mistakes in a real deployment. |
| Require a signed token now | The brief has no auth; deferred (API-1). |

## Consequences

**Positive** — one place reads the tenant, so swapping the header for a token
claim later changes the middleware only.

**Negative / accepted costs** — **the header is caller-asserted, not
authenticated.** Anyone can send `X-Tenant-ID: 2` and read tenant 2's data. This
is not tenant isolation. It is acceptable for this sample and must be replaced
by a token-derived tenant before any real deployment (API-1, API-8).

**Revisit when** — authentication is added.
