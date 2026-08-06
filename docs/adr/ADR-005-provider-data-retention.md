# ADR-005: Provider data retention

- Status: Accepted
- Date: 2026-08-05
- Decision owners: Architecture, Privacy, Security, Provider Compliance

## Context

Route-provider requests and responses can contain precise coordinates, route geometry, traffic-derived durations and provider-owned data. These are both privacy-sensitive and subject to provider contract/licensing restrictions. Пробок.Нет needs enough short-lived state for an asynchronous search and SSE reconnect, but does not require a historical raw-response corpus to score a request.

## Decision

Пробок.Нет does not persist or log raw provider requests or responses. They exist only in provider-adapter memory until normalization or cancellation and are released within the bounded search deadline. The adapter emits provider-neutral candidates and sanitized reason/error codes; internal URLs, headers, credentials and raw payloads never cross its boundary.

The secure defaults are:

- `PROVIDER_DATA_STORAGE_ALLOWED=false`;
- `PROVIDER_DATA_MODIFICATION_ALLOWED=false`;
- `ENABLE_PROVIDER_CACHE=false`.

The orchestrator may hold normalized candidates in memory for the active search. Application use of Redis is limited to rate limits, locks, idempotency and cancellation/SSE coordination. The authentication gateway also uses an isolated, short-lived Redis namespace for encrypted OIDC session material. A provider-derived cache is permitted only when all three conditions hold: the contract permits it, the corresponding flags explicitly enable it, and a reviewed retention configuration sets a bounded TTL. Absence, ambiguity or configuration error means no cache/write.

Synthetic, hand-authored fixtures replace captured production responses in tests. Provider route references and geometry are retained/returned only where current terms allow it. Exact coordinates, geometry, addresses and raw payloads are forbidden in logs, metric labels, traces, dashboards and administrative views.

User-requested saved-route history is a separate product dataset, not a provider-response archive. Before enabling it, product/privacy owners must document purpose, consent or other basis, user controls, region, encryption, maximum retention and deletion behavior.

## Retention envelope

| Artifact | Maximum/default |
|---|---|
| Raw provider request/response | Memory only, bounded by search deadline; never persisted |
| Active normalized candidates | Memory for active search |
| Optional licensed normalized cache | Disabled; TTL must be contractually approved before enablement |
| Idempotency/search coordination | Short TTL sufficient for retry/SSE semantics; no raw route body |
| OIDC gateway session | Redis only, encrypted, no longer than the configured one-hour cookie lifetime |
| Traces | 72 hours, route data removed |
| Logs | 7 days, route data removed |
| Metrics | Aggregate only; no coordinates or unbounded IDs |
| Security audit events | 90 days (`AUDIT_RETENTION=2160h`), purged at least daily, no coordinates/geometry |
| Database backups | 35 days encrypted, subject to deletion-ledger replay after restore |

Production-specific shorter periods may override these limits. Longer periods require a revision of this ADR plus privacy, security and provider-compliance approval.

## Enforcement

Configuration validation fails closed if provider storage/cache is requested without an explicit permission set. Telemetry has both producer allowlists and collector-side deletion. CI secret/data-leak tests inspect bundles and error/log fixtures. Operational audits compare feature flags, Redis keys/TTLs, database schema, telemetry attributes and current provider terms.

## Consequences

We reduce privacy, licensing and breach impact and avoid coupling scoring to undocumented payloads. We give up replaying production provider responses and some cache savings. Diagnosis relies on traces, aggregate metrics, reason codes and synthetic reproductions. If terms later allow caching, enabling it is an intentional reviewed change rather than a silent optimization.
