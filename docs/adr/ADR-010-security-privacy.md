# ADR-010: Security and privacy model

- Status: Accepted
- Date: 2026-08-05
- Decision owners: Architecture, Security, Privacy, SRE

## Context

Пробок.Нет processes precise location and calls a paid third-party provider. Its public async/SSE API is attractive for account/route disclosure, denial of service and quota abuse. A server provider credential must never reach a browser. Microservice boundaries only improve containment if identity, egress, secrets and telemetry are designed consistently.

## Decision

### Identity and public boundary

Production uses an ingress-integrated OIDC session gateway with authorization code, PKCE S256, nonce verification, an explicit account-domain allowlist and short-lived tokens. The gateway stores encrypted session material in Redis and gives browsers a Secure, HttpOnly, SameSite `__Host-` cookie. Web navigation redirects to login; unauthenticated API/SSE calls return `401`. For authenticated API calls, ingress overwrites any client-supplied `Authorization` header with the gateway's validated bearer token. The web workload does not receive that bearer token.

Edge independently validates signature, issuer, audience, expiry/not-before and allowed clock skew and authorizes ownership on every route-search GET, DELETE and SSE connection/reconnect. Tokens and cookies are never accepted in URLs. Administrative APIs require an explicit admin role and expose aggregate operational data only.

The edge enforces JSON schemas and size/range limits, per-user and per-IP rate limits, canonical-body idempotency scoped to the principal, CORS allowlists, correlation IDs and normalized errors. The gateway uses a bounded per-request CSRF cookie and a same-host callback/redirect allowlist. CSP and output encoding apply to the web application.

Anonymous usage is an explicit environment feature and defaults off outside development. It does not weaken server-side budgets or abuse controls.

### Secrets and provider isolation

The Yandex Router key is a server-only secret injected from the platform secret manager into `provider-yandex`. The optional Maps JavaScript key is distinct, deliberately public and restricted by origin/API/quota. Secrets are not created by Helm, logged, placed in values, images or CI artifacts.

Provider base URLs are fixed allowlisted HTTPS origins. The client rejects user-controlled URLs, credentials in URLs, cross-origin redirects, DNS/IP results in private/link-local/loopback ranges and responses exceeding size/deadline limits. Only officially documented APIs are implemented.

### Workload and network containment

Pods run non-root with read-only root filesystems, RuntimeDefault seccomp, no privilege escalation/capabilities and no service-account tokens. NetworkPolicy starts default deny, explicitly permits the service call graph, datastore/telemetry destinations and provider public TCP/443 while excluding internal CIDRs. Ingress is the only caller of public web, edge and authentication ports; the callback path cannot authenticate recursively. Datastore access is split by need: edge gets PostgreSQL and Redis, orchestrator and auth gateway get Redis only, and the migration Job gets PostgreSQL only. Production transport is encrypted at the ingress and to managed data/identity/provider endpoints; the platform should add mesh mTLS for internal traffic when its operational ownership is established.

### Privacy and observability

Precise coordinates, addresses, geometry, request/response bodies, credentials, raw IPs and raw provider payloads are prohibited in telemetry. Services emit structured allowlisted attributes; the OTel Collector deletes sensitive keys again. IP-derived abuse identifiers use a dedicated keyed hash with a bounded dual-key rotation overlap; the audit-chain key is separate. IDs used for correlation are access-controlled and time-bounded. Metrics have bounded-cardinality labels. Retention follows ADR-005 and `PRIVACY.md`.

### Supply chain and release

Dependencies and actions are locked/pinned and reviewed. CI performs secret/dependency scanning, SAST, tests, SBOM and vulnerability scanning, including the pinned external authentication image. Release artifacts have provenance, keyless signatures and digest references. Production deployment uses a protected environment approval and a refreshable GitHub OIDC identity bound to the exact repository/environment subject, not a static kubeconfig. It verifies the cluster identity, pre-provisioned restricted namespace and runtime-secret key names, renders digest-only manifests, then performs a server-side admission dry run before the atomic Helm deployment. Kubernetes admission should enforce signature/digest and Pod Security restrictions.

## Alternatives considered

- Passing the server provider key to the browser was rejected because origin restrictions cannot make a server credential safe and quota abuse would be difficult to contain.
- Trusting the internal cluster network was rejected because a compromised workload could move laterally.
- Logging route payloads for debugging was rejected because aggregate signals, reason codes and trace IDs are sufficient and have far smaller privacy impact.
- Long-lived CI/cloud keys were rejected in favor of workload identity/OIDC and environment approvals.
- Always-on provider caching was rejected because contractual permission is not implicit and fail-open behavior would violate ADR-005.

## Consequences

Production depends on an identity provider, secret manager, TLS, NetworkPolicy-enforcing CNI, monitoring CRDs and signed-artifact policy. Local Compose deliberately uses a stub provider and loopback-only public ports. Some debugging is less convenient without payload logging, and internal mTLS is a platform integration rather than application code. These costs are accepted to reduce credential, location, quota and lateral-movement risk.

## Verification

Security integration tests cover token/ownership/idempotency/rate-limit/SSRF/error redaction. CI scans the frontend bundle and logs for server secrets, validates Helm security fields and NetworkPolicies, scans exact image digests and verifies signatures before deployment. Threat-model and privacy reviews are required when a trust boundary, provider, stored dataset, auth flow, admin feature or analytics sink changes.
