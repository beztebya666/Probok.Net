# Runbook: authentication gateway outage

## Trigger and impact

Use this runbook for `GreenRouteAuthenticationGatewayUnavailable`, a login/callback failure spike, repeated CSRF/nonce failures, or reports that authenticated API/SSE calls return `401`. The gateway is fail closed: when it or Redis/IdP is unavailable, protected web and API access is unavailable, while existing routing work may continue internally.

Never copy cookies, authorization headers, OIDC codes, tokens, client secrets, email addresses or Redis session values into logs, commands, tickets or chat. Use only aggregate counters, timestamps, deployment revisions and redacted request/trace IDs.

## Triage

1. Declare the incident, record the first alert time and freeze unrelated auth/ingress/Redis releases.
2. Check OAuth2 Proxy desired/available replicas, readiness, restarts, OOM/evictions and the exact deployed image digest. Check the authentication Service endpoints and Prometheus target state.
3. From a controlled synthetic account, distinguish: web redirect failure, IdP discovery/connectivity failure, callback/CSRF rejection, Redis session failure, API-only token rejection, and SSE-only behavior. Do not bypass authentication to test production.
4. Check ingress controller health and events for the `/oauth2`, `/api` and `/` Ingress objects. Confirm `/oauth2` is not protected recursively, API auth failures remain `401`, and the auth subrequest targets the internal gateway Service.
5. Check IdP status, DNS/TLS reachability, issuer discovery/JWKS availability, client registration, exact callback URI and certificate time validity. Check Redis availability, TLS/auth status, memory pressure, eviction and latency.
6. Correlate the first failure with Helm history, IdP/Redis/ingress changes, secret-version metadata and certificate rotation. Never read secret values back merely to compare them.

## Mitigation and recovery

- Roll back a recent application/chart change atomically when the prior signed digest and configuration are compatible. Do not roll back a compromised secret.
- Restore OAuth2 Proxy replicas or Redis capacity within existing limits. Do not widen NetworkPolicy, trusted-proxy CIDRs, email domains, redirect allowlists or token validation to recover traffic.
- For an IdP outage, communicate that login and token refresh are unavailable and wait/fail closed. Extending token or cookie lifetime during the incident requires Security approval and is not the default mitigation.
- If a secret version is wrong but uncompromised, select the prior approved version, roll the gateway and verify metadata. If exposure is suspected, follow the key-rotation runbook and accept forced reauthentication.
- After the dependency is healthy, verify a fresh PKCE login, web navigation, API call, SSE connection/reconnect, logout and expired-session `401`. Confirm all gateway replicas share Redis-backed sessions and no sensitive material entered telemetry.

Resolve only after the alert clears, synthetic checks pass from every served region, error rates return to baseline and the incident owner records cause, duration, affected environments, recovery revision and follow-up actions.

## Escalation

Escalate immediately to Security for suspected token/cookie/client-secret exposure, unexpected issuer/audience/signature behavior, callback manipulation or authorization-header spoofing. Escalate to the IdP or Redis platform owner for dependency failure. Involve Privacy if session/token data may have been disclosed and the provider owner only if routing provider traffic was also affected.
