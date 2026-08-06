# Privacy and data handling

Route endpoints, waypoints, departure times and detailed geometry can reveal home, work, habits and current location. Пробок.Нет treats them as sensitive user data even when a request is anonymous. Product analytics, observability and administrative tooling are designed not to require exact routes.

## Data inventory and purpose

| Data | Purpose | Default storage | Access |
|---|---|---:|---|
| Origin, destination, waypoints, departure time | Build the requested route | Memory for the bounded search only | Routing services |
| Raw provider request/response | Provider call and normalization | Never persisted; never logged | Provider adapter memory |
| Normalized candidates and geometry | Score and return alternatives | Memory only; optional short cache only when license and flags permit | Orchestrator |
| Saved route/search history | User-requested history | Disabled for anonymous users; product policy must define an explicit TTL before enabling | User and narrowly scoped support role |
| Account identifier | Authentication, authorization and abuse prevention | Account lifetime plus deletion workflow | Edge/auth administrators |
| OIDC session and upstream token material | Maintain an authenticated browser session | Encrypted Redis session, at most the configured one-hour cookie lifetime; removed on logout/expiry | Authentication gateway only |
| IP-derived abuse key | Rate limiting and fraud prevention | Pseudonymized keyed hash with short rotation window | Edge/security automation |
| Audit event | Security and administrative accountability | 90 days (`AUDIT_RETENTION=2160h`); purge runs at least daily | Security auditors |
| Metrics | Reliability, cost and aggregate product quality | Aggregate only; 30 days online unless platform policy is shorter | SRE/product aggregate access |
| Traces | Debug distributed requests | 72 hours; IDs and coarse outcome only | SRE |
| Application logs | Operations and security investigation | 7 days online; no route payloads | SRE/security |
| Encrypted database backup | Disaster recovery | 35 days, then cryptographic/physical deletion | Backup operators |

These periods are engineering defaults, not a claim of legal sufficiency. The privacy owner must approve jurisdiction, data residency, contractual terms and any longer retention before production launch.

The edge rejects audit retention outside 24 hours to 365 days and purge intervals outside one minute to 24 hours. The default `AUDIT_PURGE_INTERVAL=24h` makes deletion lag bounded; reducing either period is allowed by environment policy, while extending retention requires the approvals above.

## Provider data

`PROVIDER_DATA_STORAGE_ALLOWED=false`, `PROVIDER_DATA_MODIFICATION_ALLOWED=false` and `ENABLE_PROVIDER_CACHE=false` are fail-closed defaults. Raw Yandex payloads, tiles and undocumented traffic data are never collected. A cache or persisted normalized provider-derived data may be enabled only when current provider terms explicitly permit the exact data, purpose, geography, transformation and retention period, and ADR-005 has been revised and approved.

Provider references are returned or stored only when contractually allowed. Test fixtures are synthetic and hand-authored; production responses are not copied into test data.

## Telemetry minimization

Never place latitude, longitude, addresses, polylines, request or response bodies, access/refresh/ID tokens, API keys, cookies, email addresses, raw IPs or full provider URLs in logs, trace attributes, metric labels or error messages. OAuth request and authentication logs are disabled; standard gateway logs must remain path/payload-free. Approved fields include `requestId`, `searchId`, `traceId`, coarse status, reason code, routing mode, confidence bucket, provider operation and bounded latency/cost counters.

The OpenTelemetry Collector performs a second deletion pass for known sensitive attributes. This is defense in depth; producers remain responsible for not emitting them. Metric labels must have bounded cardinality.

## User controls and rights operations

Account export, correction and deletion requests are authenticated before execution and recorded as coordinate-free audit events. Logout deletes the active gateway session; identity-provider global logout/revocation remains an IdP responsibility and short token/session expiry bounds the residual window. Account deletion covers primary records, auth sessions, caches and derived indexes promptly; encrypted backups expire on their normal 35-day schedule and are not restored except for disaster recovery. If a backup is restored, the deletion ledger is replayed before serving users.

Anonymous usage is opt-in by environment and enabled only for local development by default. Production must either authenticate requests or have an approved anonymous-use policy describing rate-limit identifiers and notice/consent.

## Access and disclosure

Use least-privilege roles and audited, time-limited production access. The admin view exposes provider state, aggregate cost, confidence distribution and degraded ratios, not exact user routes. Support tooling must not add route visibility without a documented lawful basis, purpose limitation and privacy/security review.

Data is encrypted in transit and at rest by the platform. Production backups are encrypted with separately controlled keys. Any new processor, analytics sink, provider, region or cross-border transfer requires a data-flow review and an update to this document and the threat model.
