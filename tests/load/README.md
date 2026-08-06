# Load and failure testing

The suite uses only synthetic Moscow coordinates and never sends production user routes. Run the normal/peak profile against the local stub:

```sh
docker compose up -d --build --wait
docker compose --profile load run --rm k6 run /scripts/route-search.js
docker compose --profile load run --rm k6 run /scripts/sse-reconnect.js
docker compose --profile load run --rm k6 run /scripts/mass-cancellation.js
```

Failure tests require the provider stub to start in the selected deterministic mode. Recreate the affected services so the environment change takes effect:

```sh
docker compose --profile observability up -d prometheus
PROVIDER_STUB_SCENARIO=slow PROVIDER_STUB_DELAY=4s docker compose up -d --build --force-recreate provider-yandex routing-orchestrator edge-api
docker compose --profile observability --profile load run --rm -e PROMETHEUS_URL=http://prometheus:9090 -e EXPECTED_FAILURE_MODE=slow k6 run /scripts/provider-failures.js

PROVIDER_STUB_SCENARIO=rate_limit docker compose up -d --force-recreate provider-yandex
docker compose --profile observability --profile load run --rm -e PROMETHEUS_URL=http://prometheus:9090 -e EXPECTED_FAILURE_MODE=rate_limit k6 run /scripts/provider-failures.js

PROVIDER_STUB_SCENARIO=outage docker compose up -d --force-recreate provider-yandex
docker compose --profile observability --profile load run --rm -e PROMETHEUS_URL=http://prometheus:9090 -e EXPECTED_FAILURE_MODE=outage k6 run /scripts/provider-failures.js
```

Acceptance gates are encoded as k6 thresholds: initial p95 below 3 seconds in the normal profile, fewer than 1% failed route starts, bounded completion under provider faults, successful SSE reconnects and cancellations, and no provider call rate above the configured per-search budget. Size RPS and VUs from measured capacity rather than copying local defaults into production tests. Load tests must use an isolated provider quota and environment; never aim failure scenarios at production.
