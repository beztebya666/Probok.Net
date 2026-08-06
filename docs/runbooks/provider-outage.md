# Runbook: route-provider outage

## Trigger and impact

Use this runbook for `GreenRouteProviderCircuitOpen`, sustained provider 5xx/timeouts, initial/enhanced latency SLO alerts, or a sharp rise in degraded/failed searches. Пробок.Нет should return initial or degraded results when safe; it must not represent UNKNOWN traffic as GREEN or create an unbounded retry queue.

## First 10 minutes

1. Acknowledge the alert, assign incident commander and operations lead, and record start time/affected region/release.
2. Confirm user impact from edge availability, route-search outcomes and p95 latency. Do not rely only on provider health.
3. Check provider request rate, error codes, timeouts, circuit state, concurrency, budget exhaustion and estimated cost. Correlate a sanitized `traceId`/`searchId`; do not paste coordinates or provider payloads into the incident record.
4. Compare against provider status/contract notifications, DNS/TLS failures, credential expiry/quota and the most recent Пробок.Нет deploy/config change.
5. If the incident started with a release, follow [rollback.md](rollback.md). If external, keep the circuit breaker and bounded deadline enabled; never raise retries globally during an outage.

Useful PromQL:

```promql
sum by (code) (rate(provider_errors_total{service="provider-yandex"}[5m]))
histogram_quantile(0.95, sum by (le) (rate(provider_request_duration_seconds_bucket[5m])))
max(provider_circuit_breaker_state)
sum(rate(search_budget_exhausted_total[5m])) / clamp_min(sum(rate(route_search_total[5m])), 0.000001)
sum by (status) (rate(route_search_total[5m]))
```

## Mitigation

- Preserve `FASTEST`/initial results when the implementation has valid candidates and mark green optimization unavailable/degraded with confidence and reason codes.
- Disable enhanced search, avoid-zone generation or corridor anchors through the reviewed Helm/GitOps feature values if they amplify provider calls. Do not edit production Pods by hand; record the configuration revision.
- Reduce admission rate at the edge if demand threatens the bounded provider concurrency/quota. Prefer explicit 429 with `Retry-After` over queue growth.
- Keep provider concurrency and request budgets bounded. Honor upstream `Retry-After`; do not retry non-temporary 4xx responses.
- If credentials/quota are the cause, use the key-rotation or rate-limit runbook. Never switch to undocumented endpoints or scrape provider web traffic.

## Recovery

1. Require provider success/latency and circuit state to remain healthy for at least 15 minutes under a gradual traffic ramp.
2. Confirm no retry surge, queue backlog, abnormal cost, stale lock or rising 5xx as the circuit closes.
3. Re-enable features one at a time through GitOps and observe one full alert-evaluation window between changes.
4. Validate a synthetic FASTEST and GREENEST search, hard constraints, confidence/reason codes, cancellation and SSE completion.
5. Resolve the incident only after edge availability/latency and degraded-result ratios return to baseline.

Communicate what users experienced and that route quality may have degraded; do not claim roads were clear. Complete a post-incident review for any SLO burn, data/security impact, provider-cost anomaly or manual mitigation.
