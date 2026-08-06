# Runbook: provider rate limiting and quota pressure

## Trigger

Use for `GreenRouteProviderRateLimited`, provider 429 responses, cost/quota warnings, search-budget exhaustion or a provider-request rate inconsistent with route-search demand.

## Diagnose

1. Verify 429 rate, `Retry-After`, provider operation/credential/region, request concurrency and estimated cost.
2. Compare `rate(provider_requests_total[5m])` with `rate(route_search_total[5m]) * maxProviderRequests`. A sustained excess indicates retry amplification or unbounded candidate generation.
3. Segment by documented operation and result code, never by coordinate or route ID. Check recent feature/config/release changes and abusive principals/IP rate-limit buckets.
4. Confirm the account quota and key restriction in the official provider console through an authorized operator; never expose the key in screenshots or tickets.

## Mitigate

- Honor `Retry-After`, apply jitter and count every retry against the original search request/deadline budget.
- Reduce provider concurrency and public route-search rate. Return explicit bounded 429/503 or a truthful degraded initial result instead of queueing.
- Disable enhanced search, avoid zones and corridor anchors in that order if they drive extra calls; record and deploy the Helm/GitOps change.
- Keep provider cache disabled unless ADR-005 and current contract explicitly permit it. Do not bypass limits with undocumented APIs, extra keys or scraped data.
- Block abusive principals/IPs through the normal abuse-control path with an expiry and audit record.

## Verify and restore

Require 429s to remain at baseline for 15 minutes, circuit/concurrency to stabilize, provider-call/search ratio to stay within budget and estimated cost to match expected traffic. Restore admission/features gradually. Escalate to provider account support only through approved contacts and share request IDs/timestamps, not route payloads or credentials.

The post-incident record includes quota limit, observed peak, provider-call amplification factor, affected searches, cost impact, mitigations and the capacity/alert adjustment. A quota increase does not replace retry-budget and abuse controls.
