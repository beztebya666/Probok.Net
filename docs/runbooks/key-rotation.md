# Runbook: credential and pseudonymization-key rotation

## Preconditions

Use an approved secret manager and provider console. Two operators should perform emergency rotation: one changes provider/secret state and one verifies audit evidence. Never place either key in shell history, Helm values, Git, CI output, tickets or chat. Prefer a secret-manager UI or stdin-safe automation that suppresses output.

## Planned rotation

1. Record change owner, reason, affected environment, expected key identifier/fingerprint (never value), start time and rollback window.
2. Create a new server Router credential with only required official APIs, environment/IP restrictions and the smallest adequate quota. Do not reuse the browser Maps key.
3. Add a new version under the existing secret-manager name/key. Confirm the external-secret controller synchronized a new Kubernetes Secret resource version without reading the value back.
4. Trigger a rolling restart of `provider-yandex` through the signed Helm/GitOps release. The adapter must fail readiness if the active mode lacks its server credential (`YANDEX_ROUTER_API_KEY` or `DGIS_API_KEY`).
5. Verify rollout, secret version metadata, readiness, provider success/error/429/cost metrics and one synthetic route. Search logs/telemetry/frontend assets for the old/new fingerprints only if fingerprints are non-secret and pre-approved.
6. Observe for at least 15 minutes, then disable the old key at the provider. Do not delete it until disablement and rollback-window policy are confirmed.
7. Verify traffic continues on the new key, then revoke/delete the old credential and close the change with provider and cluster audit records.

## Emergency suspected exposure

Immediately stop or isolate affected provider traffic, revoke the exposed key, create a replacement, update secret storage and roll the adapter. Accept a temporary degraded route experience rather than continuing with a compromised credential. Search repository history, CI artifacts, images, logs and incident systems for scope; rotate related credentials if shared access could have exposed them. Notify security/privacy/provider owners and follow the incident process.

## Abuse pseudonymization key rotation

`ABUSE_HASH_KEY` pseudonymizes IP-derived abuse identifiers and is distinct from `AUDIT_HASH_KEY`. For a continuity-preserving rotation, atomically move the current abuse key to `ABUSE_PREVIOUS_HASH_KEY`, install a newly generated random value as `ABUSE_HASH_KEY`, and roll every `edge-api` replica. Verify both key fingerprints through secret-manager metadata, readiness and rate-limit behavior without printing either value. Keep the previous key only for the documented overlap needed by the longest abuse-control TTL, then remove it and roll the edge again. An exposed key is not retained for overlap; rotate immediately and accept loss of cross-window correlation.

Audit-chain key rotation has no dual-key compatibility field. Coordinate an `AUDIT_HASH_KEY` change with Security so the final old-key event, first new-key event, time and approved key fingerprints are preserved outside the application audit chain. Never reuse the abuse key as the audit key.

## OIDC gateway credential rotation

For `OAUTH2_PROXY_CLIENT_SECRET`, create a new IdP client-secret version while the old one remains valid, update the secret-manager version, verify synchronization by metadata and roll every OAuth2 Proxy replica. Complete a login, token refresh, API request and SSE connection through the production hostname. Revoke the old IdP secret only after the rollout and observation window. If the IdP cannot overlap client secrets, use an approved maintenance window and expect login/refresh interruption.

`OAUTH2_PROXY_COOKIE_SECRET` protects session tickets and has no dual-key field in this deployment. A planned change therefore invalidates every active browser session: announce the forced reauthentication, update the secret atomically, roll all gateway replicas and verify that old cookies fail while a new PKCE login succeeds. On suspected exposure, rotate immediately and accept forced logout. Never reuse the OIDC client secret, Redis credential, audit key or provider key as the cookie secret.

Rotate application `REDIS_URL` and gateway `OAUTH2_PROXY_REDIS_URL` credentials independently with a server-supported overlap/versioned-user procedure where available. For application Redis, update `edge-api` and `routing-orchestrator`; for gateway Redis, update only OAuth2 Proxy and verify multi-replica session continuity. Revoke each old credential after its own rollout and observation window. If overlap is unavailable, coordinate the corresponding bounded loss of ephemeral coordination or fail-closed authentication window; do not weaken Redis network or authentication controls to preserve availability.

## Rollback

Before revocation, rollback means reselecting the prior disabled-but-valid secret version and rolling `provider-yandex`. After compromise or revocation, the old key is never a rollback option; mitigate by degraded mode while fixing the new credential restriction. Confirm the rollback does not reintroduce the original rotation cause.
