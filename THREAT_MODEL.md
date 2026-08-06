# Пробок.Нет threat model

## Scope and security objectives

This model covers the browser/PWA, OIDC session gateway, public edge API, routing orchestrator, provider adapter, PostgreSQL, Redis, observability path, CI/CD and Kubernetes runtime. Managed identity, ingress, DNS, database, Redis, secret manager, artifact registry and monitoring systems are dependencies with separate control planes.

Primary objectives are: keep server credentials secret; prevent unauthorized route/account access; preserve route integrity and hard constraints; minimize disclosure of precise location; contain provider and dependency failures; prevent quota/cost abuse; and make builds and deployments verifiable.

## Trust boundaries and data flow

1. An untrusted browser crosses the public TLS boundary at ingress. `/oauth2` reaches the OIDC session gateway; `/` and `/api` first invoke that gateway as an internal authentication subrequest.
2. The gateway completes authorization code with PKCE and keeps upstream session material in Redis. It returns only a validated bearer token to the API ingress; ingress overwrites the inbound `Authorization` header before forwarding to `edge-api`. The web workload receives no API bearer token.
3. `edge-api` independently validates identity, authorization, schema, idempotency and rate limits before crossing the internal boundary to `routing-orchestrator`.
4. The orchestrator sends only provider-neutral bounded requests to `provider-yandex`.
5. The provider adapter crosses the third-party boundary to the allowlisted official Yandex HTTPS API.
6. Edge crosses the data boundary to PostgreSQL and Redis; the orchestrator and authentication gateway reach Redis only. Credentials come from secret storage. The migration Job is the only other PostgreSQL client.
7. All services cross the telemetry boundary to the collector after local redaction. Operators query aggregate observability systems.
8. CI crosses the supply-chain boundary to registries and the deployment environment using refreshable short-lived identities.

Exact coordinates and route geometry are sensitive assets. Yandex server keys, OIDC/database/Redis credentials, CI identities and signing identities are critical secrets. Search results, hard constraints, confidence/reason codes and idempotency records are integrity-sensitive assets.

## Threats and controls

| Threat | Scenario and impact | Preventive/detective controls | Residual risk |
|---|---|---|---|
| Spoofed user/token | Forged or replayed token reads/cancels another search | OIDC gateway verifies issuer, nonce and PKCE; ingress overwrites `Authorization`; edge revalidates issuer/audience/signature/time claims; short TTL; search ownership checks; no credentials in SSE query strings | Compromised user device remains able to act until expiry |
| Session fixation or callback abuse | Forged state, stolen cookie or open redirect acquires another session | `__Host-`, Secure, HttpOnly, SameSite cookie; per-request CSRF cookie; PKCE S256; nonce verification; exact HTTPS callback and same-host redirect allowlist; short expiry; Redis-backed session; auth callback path is not recursively protected | A compromised browser or identity-provider session can still authenticate |
| IDOR on search/SSE/admin | Guessing `searchId` exposes route or operational detail | High-entropy IDs plus authorization on every GET/DELETE/SSE reconnect; explicit admin role; aggregate admin data | Application authorization regression; covered by tests and audit |
| Request tampering | Invalid coordinates, huge waypoint set or budget bypass consumes resources | Strict schemas and numeric bounds; server-side maximum deadline/provider budget/body size; hard constraints rechecked after scoring | Valid but adversarial searches still consume bounded quota |
| Idempotency poisoning | Attacker reuses a key with a different body or across principals | Key scoped to principal and endpoint; canonical request hash; mismatch rejection; bounded TTL; atomic Redis write | Redis outage reduces deduplication; edge fails safely per policy |
| SSRF/redirect abuse | Provider URL override or redirect reaches metadata/internal services | Fixed HTTPS origin and host validation; private/link-local CIDR rejection; redirects disabled/cross-origin rejected; provider-only egress policy | DNS rebinding between resolution/connect must be blocked in client dialer |
| Provider key disclosure | Key leaks through frontend, logs, errors, image or CI | Separate browser/server keys; secret manager; redaction; secret scan; no raw upstream errors; read-only secret access; rotation runbook | Provider/operator compromise outside Пробок.Нет |
| Route privacy leak | Coordinates enter logs, traces, labels, admin or test fixtures | Producer allowlist; collector deletion processor; aggregate dashboards; synthetic fixtures; access TTLs; privacy tests/reviews | Free-form exception text could accidentally include address; output encoding/redaction required |
| Traffic-data misrepresentation | UNKNOWN segments shown as GREEN or Пробок.Нет score described as official traffic | Domain invariant UNKNOWN != GREEN; confidence/reason codes required; provider-neutral wording; deterministic golden tests | Provider data can be stale or incomplete and must remain visibly uncertain |
| Quota/cost exhaustion | Bot or retry storm consumes provider quota | Per-user/IP rate limits; max provider requests and concurrency; retry budget/jitter; circuit breaker; cost/429 alerts | Coordinated distributed abuse; upstream quota is final backstop |
| Cascading outage | Slow/429/5xx provider ties up edge and queue | Deadlines, cancellation propagation, bulkhead, bounded retry, circuit breaker, graceful degradation and no unbounded queue | Regional provider outage reduces route quality/availability |
| SSE resource exhaustion | Many slow connections consume file descriptors/memory | Auth before upgrade, per-principal connection cap, idle/max lifetime, heartbeat, cancellation and ingress timeout limits | Large legitimate reconnect wave; tested by k6 and capacity planning |
| Cache/database disclosure | Redis snapshot or DB backup exposes routes or OIDC session tokens | No raw provider payloads; short reviewed TTLs; encryption/access control; Redis restricted to ephemeral coordination and auth sessions; datastore-specific credentials/NetworkPolicies; encrypted backups | Infrastructure administrator access; audited and separated duties |
| Telemetry injection/cardinality | User strings forge log records or high-cardinality labels | Structured encoding; bounded label allowlist; payloads excluded; collector memory limiter; dashboards use safe fields | Novel attribute added by a library before review |
| Container escape/lateral movement | Compromised service reaches cluster API or neighbors | Non-root/read-only/no capabilities/seccomp; no service-account token; default deny; minimal images; scan and patch | Kernel/CNI vulnerability; platform patching and runtime detection required |
| Malicious dependency/build | Package/action/base image inserts code or steals CI token | Lockfiles; pinned actions/images; dependency review/SAST; minimal permissions; isolated jobs; SBOM, provenance, signature, digest deploy | Compromise before ecosystem detection; reproducibility and review reduce exposure |
| Unauthorized deployment | Stolen CI credential deploys unreviewed image | Refreshable GitHub OIDC federation bound to exact repository/environment subject; no static kubeconfig; protected environment approval; signed digest, server-side admission dry run and identity audit | Compromised approver, GitHub issuer or cluster trust policy |
| Destructive migration | Schema change prevents rollback or loses history | Expand/contract migrations, pre-deploy backup, migration Job deadline, restore rehearsal, rollback/DR runbooks | Logical corruption may replicate to backups before detection |

## Abuse cases that must be tested

- Cross-user GET, DELETE and SSE reconnect using another user's `searchId` returns a normalized denial.
- Forged OAuth state/nonce, disallowed email domain, callback redirect to another host and client-supplied `Authorization` fail closed; the web upstream receives no API bearer token.
- An unauthenticated API or SSE request returns `401` without a redirect body, while an unauthenticated web navigation starts the OIDC flow.
- Reusing an idempotency key with a changed body is rejected without another provider call.
- Coordinates outside valid ranges, oversized waypoints and excessive deadlines/budgets are rejected at the edge.
- Provider redirects, DNS results in denied ranges and non-HTTPS base URLs fail closed.
- Slow, 429 and outage modes complete or degrade within the search deadline without retry amplification.
- Cancellation closes provider work and SSE resources promptly.
- Generated frontend assets, logs, traces, metrics, SBOMs and error bodies contain no server provider key.
- UNKNOWN congestion is never ranked or presented as known GREEN.

## Review triggers

Review this model for any new provider, stored route/history feature, live rerouting, admin detail, analytics sink, authentication flow, public API, cluster/region, provider-cache permission, data-retention change or material dependency/runtime change. Security and privacy owners approve changes to trust boundaries or sensitive-data handling.
