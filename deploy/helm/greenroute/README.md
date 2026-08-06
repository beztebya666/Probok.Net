# Пробок.Нет Helm chart

This chart deploys the four Пробок.Нет product workloads, their Services and an ingress-integrated OAuth2 Proxy authentication gateway. PostgreSQL, Redis, ingress controller, certificate management, OpenTelemetry Collector, identity provider and Prometheus Operator are platform dependencies and deliberately remain outside the application release lifecycle.

Production values are secure by default: Yandex mode, anonymous access off, provider-data retention off, authentication gateway on, hardened pod/container security contexts, default-deny network policy, PDBs, topology spreading and autoscaling. `greenroute-runtime` is a reference to a pre-provisioned Secret; the chart contains no secret material.

To deploy the official 2GIS adapter, set `config.providerMode: 2gis`, provision
the `DGIS_API_KEY` entry named by `secrets.keys.dgisApiKey`, and align the three
`dgis*Limit*` values with Platform Manager. The key is injected only into the
provider workload; it is never rendered into the ConfigMap or web workload.
If the optional browser MapGL traffic renderer is enabled, provision a separate
origin-restricted key named by `secrets.keys.twoGisMapGLBrowserApiKey`. That
browser credential is public by design and must never reuse `DGIS_API_KEY`.
Yandex traffic stays disabled by default. After the JavaScript API subscription
includes the official traffic layers, set `config.yandexTrafficAvailable: true`;
the web client then loads `@yandex/ymaps3-layers-extra` at runtime and falls back
to traffic-off mode if the package cannot initialize.

Render and validate:

```sh
helm lint deploy/helm/greenroute -f deploy/helm/greenroute/values-prod.yaml --strict
helm template greenroute deploy/helm/greenroute -n greenroute -f deploy/helm/greenroute/values-prod.yaml | kubeconform -strict -summary -ignore-missing-schemas
```

At release time pass the product image manifest digests using `--set-string images.edgeApi.digest=sha256:...` and the corresponding keys. Tags remain explicit for audit readability, while rendered Pods use digests when present. The chart's standalone default pins the upstream OAuth2 Proxy manifest. The protected release rebuilds that exact no-code base as a Пробок.Нет release artifact, generates provenance/SBOM, scans and keyless-signs it, then overrides `images.oauth2Proxy` with the signed registry digest. Provider egress excludes private, loopback, link-local and multicast ranges; if the provider publishes stable allowlist CIDRs, narrow `networkPolicy.providerEgressCIDR` further.

Before exposing the release, set `config.trustedProxyCIDRs` to the smallest comma-separated set of ingress-controller source CIDRs. The base chart uses `127.0.0.1/32` as a fail-closed sentinel and the protected release workflow rejects that value and `0.0.0.0/0`. Only allowlisted peers may supply `X-Forwarded-For` for rate limiting and audit attribution.

## OIDC browser and API flow

Register exactly `https://<ingress.host>/oauth2/callback` as the identity-provider redirect URI. Configure the issuer/client ID and replace `authProxy.allowedEmailDomains: [invalid.invalid]` with the intended stage or production domain. Wildcards and placeholder domains are rejected by the release gate. Provision distinct random `OAUTH2_PROXY_CLIENT_SECRET` and `OAUTH2_PROXY_COOKIE_SECRET` values plus a least-privilege `OAUTH2_PROXY_REDIS_URL` credential in the external runtime Secret; do not reuse application Redis credentials. The cookie secret must decode to a supported 16/24/32-byte AES key (32 bytes is preferred).

The `/oauth2` callback route reaches only OAuth2 Proxy. Protected web navigation starts the OIDC login flow. API and SSE paths never redirect: the ingress authentication subrequest returns `401` when no valid session exists and, on success, overwrites `Authorization` with a validated bearer token that `edge-api` validates again. OAuth request/auth logs are disabled, session material is encrypted in Redis, and the web workload does not receive the API bearer token.

The IdP must issue the configured audience and an `email`/verified-email identity accepted by the allowlist. Redis must be highly available and encrypted/authenticated according to platform policy; an auth-gateway or Redis outage intentionally fails access closed. Test login, logout, expired sessions, SSE `401`, callback/CSRF rejection and multi-replica session continuity before promotion.

The edge limits long-lived connections with `maxConcurrentSSE`, `maxSSEPerPrincipal`, `sseMaxLifetime` and `sseIdleTimeout`; keep the global SSE limit below `maxConcurrentEdgeRequests`. Security audit retention defaults to 90 days (`auditRetention: 2160h`) with a daily purge. Production requires distinct, random `AUDIT_HASH_KEY` and `ABUSE_HASH_KEY` values in the external runtime Secret. `ABUSE_PREVIOUS_HASH_KEY` is used only for the bounded rotation overlap described in the key-rotation runbook.

`maxConcurrentSearches` bounds orchestrator work independently of the edge connection cap. Provider connect/request/response-header deadlines, bulkhead wait, retry delays/count, concurrency, circuit breaker, request/response byte ceilings and shutdown deadline are all explicit typed values and are rendered into the runtime ConfigMap. Helm rejects unknown values, unsafe SSE/edge relationships, cache or multi-replica route state without storage permission, and invalid autoscaling bounds before creating a workload.

Datastore access is least-privilege by workload: `edge-api` receives PostgreSQL and application Redis credentials, `routing-orchestrator` receives application Redis only, OAuth2 Proxy receives its own Redis credential only, and the migration Job receives PostgreSQL only. The rendered NetworkPolicies enforce the same matrix.

The base chart's private CIDR ranges are development-safe placeholders. A protected release must inject the exact managed PostgreSQL ranges into `networkPolicy.databaseCIDRs` and Redis ranges into `networkPolicy.redisCIDRs`; wildcard CIDRs are rejected. Keeping the lists separate prevents Redis-only workloads from gaining a PostgreSQL network path.

With provider-derived storage disabled, asynchronous search state is process-local by design. The default/stage/production profiles therefore keep `routing-orchestrator` at one replica and exclude it from HPA. Scaling it horizontally requires explicit provider/licensing approval, `PROVIDER_DATA_STORAGE_ALLOWED=true`, a reviewed Redis retention/TTL configuration and an availability test proving cross-replica SSE/cancellation semantics.
