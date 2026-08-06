# Security policy

## 2GIS server credential

`DGIS_API_KEY` is a server-only credential used only by the provider process.
The documented 2GIS endpoints require the key in the query string, so outbound
destinations are compiled fixed, ambient proxies are disabled, redirects are
rejected, and upstream URLs/bodies are never logged. The key must not use a
`NEXT_PUBLIC_` prefix or be injected into the web workload. Restrict the key in
2GIS Platform Manager by the production egress network and enabled services,
and rotate it through the same external-secret rollout used for other provider
credentials.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Send a private report through the repository host's security-advisory feature. Include the affected revision, reproducible steps, impact, and any safe proof of concept. Do not include real API keys, access tokens, provider responses, or user coordinates.

The maintainers acknowledge reports within two business days, provide an initial severity assessment within five business days, and coordinate remediation and disclosure. These are response targets rather than a bug-bounty commitment. A reporter must not access third-party data, degrade the mapping provider, or test against production without written authorization.

## Supported versions

Security fixes are issued for the current production release and the immediately preceding minor release. Container images are immutable release artifacts identified by OCI digest. Older deployments must upgrade before receiving support.

## Security architecture

- Public traffic terminates at a managed TLS ingress. `/oauth2` reaches the dedicated authentication gateway; protected `/` and `/api` routes reach only `web` and `edge-api` after an internal authentication subrequest. The orchestrator and provider adapter have no public Service.
- The authentication gateway uses OIDC authorization code with PKCE, issuer and nonce verification, an explicit email-domain allowlist and Redis-backed server-side sessions. Its `__Host-` cookie is `Secure`, `HttpOnly`, `SameSite=Lax`, path `/`, short-lived and CSRF-bound.
- For `/api`, ingress replaces any client-supplied authorization value with the gateway's validated short-lived bearer token; `edge-api` independently validates issuer, audience, signature, expiry/not-before and permitted clock skew. The web workload does not receive this bearer token. Unauthenticated API/SSE calls return `401` rather than an HTML login redirect. Administrative routes require an explicit administrator role.
- Bearer tokens, cookies and SSE credentials are never accepted in URLs. State-changing cookie-authenticated endpoints require CSRF protection.
- The edge enforces request schemas, body limits, CORS allowlists, per-user and per-IP rate limits, idempotency, correlation IDs and normalized error responses.
- Yandex Router and Geocoder credentials exist only in server-side secret storage and use distinct entitlements where the contract requires them. The optional Maps JavaScript key is a distinct browser credential restricted by origin, API and quota.
- Provider URLs are allowlisted HTTPS origins. Redirects to another origin, private/link-local addresses, user-supplied upstream URLs and experimental sources are rejected.
- Pods run as UID/GID 65532 with a read-only root filesystem, no privilege escalation, all Linux capabilities dropped, RuntimeDefault seccomp and no mounted service-account token.
- Kubernetes starts with default-deny ingress and egress. Provider egress permits public TCP/443 while excluding private, loopback, carrier-grade NAT, link-local and multicast ranges.
- Datastore credentials and network paths are workload-specific: edge has PostgreSQL and application Redis, the orchestrator has application Redis only, the authentication gateway has a distinct Redis credential only, and the migration Job has PostgreSQL only.
- Coordinates, route geometry, request/response bodies, credentials and provider raw payloads are removed by telemetry processors and prohibited as metric labels.

## Secrets

Secrets must enter production through an approved secret manager and a workload identity or secrets operator. They must not be committed, placed in Helm values, printed by CI, passed through command-line arguments, embedded in images, or exposed as `NEXT_PUBLIC_*` unless they are deliberately origin-restricted browser credentials. Yandex Maps and 2GIS MapGL browser keys are distinct from their server routing keys. OIDC client and cookie-encryption secrets are separate from provider, datastore, audit-chain and abuse-pseudonymization keys. None may be reused.

Rotate provider and pseudonymization keys using [the key-rotation runbook](docs/runbooks/key-rotation.md). Rotate immediately after suspected exposure, an operator departure with access, provider notification, or a failed secret-scanning gate. Database, Redis, OIDC and signing credentials follow the same versioned rollout/revocation pattern.

## Secure development and release

Pull requests run formatting, linting, unit/contract/integration/frontend/E2E tests, OpenAPI checks, Helm lint, Kubernetes schema validation, secret scanning, dependency review, SAST and filesystem/container vulnerability scans. Release jobs generate CycloneDX SBOMs, publish provenance, scan the exact pushed digest and use keyless Cosign signing. Production deployment requires a protected-environment approval, obtains a refreshable short-lived GitHub OIDC cluster identity bound to the exact repository/environment subject, performs a server-side admission dry run and deploys only digest-pinned images.

High or critical exploitable vulnerabilities block release. An exception requires a time-bounded, documented risk acceptance naming the owner, compensating controls and expiry. Dependency updates are automated but still pass the complete test and scan gates.

## Runtime response

Security events are correlated with request and trace IDs, never exact route coordinates. On suspected compromise:

1. Preserve access and audit logs under incident retention controls; do not copy route payloads into chat or tickets.
2. Disable affected credentials or features and isolate the workload with NetworkPolicy or ingress controls.
3. Rotate secrets, rebuild from a trusted revision, verify signatures/SBOMs and redeploy by digest.
4. Determine affected users and data with privacy and legal owners before notification.
5. Record timeline, root cause, indicators, containment, recovery and preventive actions.

See [THREAT_MODEL.md](THREAT_MODEL.md), [PRIVACY.md](PRIVACY.md) and the operational runbooks for detailed controls.
