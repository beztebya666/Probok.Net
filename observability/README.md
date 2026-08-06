# Observability and SLOs

The local `observability` Compose profile sends OTLP/HTTP traces to Tempo and structured OTLP/HTTP logs to Loki through the collector, while Prometheus scrapes application metrics directly. Grafana provisions fixed Prometheus/Tempo/Loki data-source UIDs and five read-only dashboards:

1. GreenRoute — Product Overview
2. GreenRoute — Provider Health and Cost
3. GreenRoute — Routing Quality
4. GreenRoute — API Latency and Errors
5. GreenRoute — Low Confidence and Degraded Searches

Start it with `make compose-observability`. Local endpoints bind only to loopback: Grafana `http://localhost:3001`, Prometheus `http://localhost:9090`, web `http://localhost:3000`, edge `http://localhost:8080`. Local Grafana credentials are deliberately non-production defaults and must not be copied into a shared environment.

The initial configurable objectives are 99.9% route-search API availability, initial p95 under 3 seconds and enhanced-search p95 under 10 seconds. Multi-window 14.4x/6x error-budget alerts page on fast/slow burn, with separate latency, provider circuit/429, request-budget, low-confidence and telemetry-absence alerts. The authenticated Helm profile additionally pages when no OAuth2 Proxy target is healthy and links to the authentication-outage runbook. Rules use application metrics from the product contract; a release must verify actual exported label names before enabling paging.

Coordinates, addresses, waypoint arrays, geometries, HTTP bodies, database statements and provider keys are prohibited at the producer. `otel/collector.yaml` performs a second deletion pass. This defense does not make arbitrary payload logging safe. Metric labels must be bounded dimensions such as operation/status/reason code, never `searchId`, `requestId`, `traceId`, exact coordinates or user identifiers.

The Compose backends use short local retention and single-process storage for development. Production should use managed/high-availability backends, authenticated TLS ingestion/query paths, object storage with approved retention, tenant isolation and backup appropriate to the privacy policy.
