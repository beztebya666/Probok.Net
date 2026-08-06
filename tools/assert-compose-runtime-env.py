#!/usr/bin/env python3
"""Assert local Compose preserves production-relevant runtime limits and isolation."""

from __future__ import annotations

import json
import subprocess


def fail(message: str) -> None:
    raise SystemExit(f"Compose runtime wiring assertion failed: {message}")


def main() -> None:
    result = subprocess.run(
        ["docker", "compose", "--profile", "*", "config", "--format", "json"],
        check=True,
        capture_output=True,
        text=True,
    )
    services = json.loads(result.stdout)["services"]

    expected: dict[str, set[str]] = {
        "edge-api": {
            "RATE_LIMIT_IP_PER_MINUTE",
            "RATE_LIMIT_USER_PER_MINUTE",
            "RATE_LIMIT_SEARCH_PER_MINUTE",
            "MAX_CONCURRENT_EDGE_REQUESTS",
            "MAX_CONCURRENT_SSE",
            "MAX_SSE_PER_PRINCIPAL",
            "SSE_MAX_LIFETIME",
            "SSE_IDLE_TIMEOUT",
            "IDEMPOTENCY_TTL",
            "SEARCH_OWNERSHIP_TTL",
            "SHUTDOWN_GRACE_PERIOD",
            "AUDIT_RETENTION",
            "AUDIT_PURGE_INTERVAL",
            "ABUSE_HASH_KEY",
            "ABUSE_PREVIOUS_HASH_KEY",
        },
        "routing-orchestrator": {
            "SEARCH_STATE_TTL",
            "MAX_ACTIVE_CANDIDATES",
            "MAX_CONCURRENT_SEARCHES",
            "MAX_ENHANCED_ITERATIONS",
            "MINIMUM_SCORE_IMPROVEMENT",
            "SSE_HEARTBEAT_INTERVAL",
            "SHUTDOWN_GRACE_PERIOD",
        },
        "provider-yandex": {
            "YANDEX_MAX_RESULTS",
            "PROVIDER_CONNECT_TIMEOUT",
            "PROVIDER_REQUEST_TIMEOUT",
            "PROVIDER_RESPONSE_HEADER_TIMEOUT",
            "PROVIDER_BULKHEAD_WAIT_TIMEOUT",
            "PROVIDER_MAX_RETRIES",
            "PROVIDER_RETRY_BASE_DELAY",
            "PROVIDER_RETRY_MAX_DELAY",
            "PROVIDER_MAX_CONCURRENCY",
            "PROVIDER_CB_FAILURE_THRESHOLD",
            "PROVIDER_CB_OPEN_DURATION",
            "PROVIDER_SHUTDOWN_TIMEOUT",
            "PROVIDER_MAX_REQUEST_BODY_BYTES",
            "PROVIDER_MAX_RESPONSE_BYTES",
            "PROVIDER_COST_PER_BILLABLE_UNIT",
            "PROVIDER_DATA_STORAGE_ALLOWED",
            "PROVIDER_DATA_MODIFICATION_ALLOWED",
        },
    }
    for service, required in expected.items():
        environment = set(services[service].get("environment", {}))
        missing = sorted(required - environment)
        if missing:
            fail(f"{service} is missing {', '.join(missing)}")

    safe_defaults = {
        ("edge-api", "MAX_CONCURRENT_EDGE_REQUESTS"): "256",
        ("edge-api", "MAX_CONCURRENT_SSE"): "64",
        ("edge-api", "MAX_SSE_PER_PRINCIPAL"): "3",
        ("edge-api", "SSE_MAX_LIFETIME"): "30m",
        ("edge-api", "SSE_IDLE_TIMEOUT"): "45s",
        ("routing-orchestrator", "MAX_CONCURRENT_SEARCHES"): "64",
        ("routing-orchestrator", "PROVIDER_DATA_STORAGE_ALLOWED"): "false",
        ("provider-yandex", "PROVIDER_DATA_STORAGE_ALLOWED"): "false",
        ("provider-yandex", "PROVIDER_DATA_MODIFICATION_ALLOWED"): "false",
        ("provider-yandex", "ALLOW_EXPERIMENTAL_PROVIDER_SOURCES"): "false",
        ("provider-yandex", "YANDEX_ROUTER_BASE_URL"): "https://api.routing.yandex.net",
        ("provider-yandex", "YANDEX_GEOCODER_BASE_URL"): "https://geocode-maps.yandex.ru",
    }
    for (service, key), expected_value in safe_defaults.items():
        if services[service].get("environment", {}).get(key) != expected_value:
            fail(f"{service} {key} does not match the reviewed fail-closed local value")

    forbidden_environment = {
        "routing-orchestrator": {"DATABASE_URL"},
        "provider-yandex": {"DATABASE_URL", "REDIS_URL"},
        "web": {"DATABASE_URL", "REDIS_URL", "INTERNAL_API_TOKEN", "YANDEX_ROUTER_API_KEY"},
    }
    for service, forbidden in forbidden_environment.items():
        present = sorted(forbidden & set(services[service].get("environment", {})))
        if present:
            fail(f"{service} unexpectedly receives {', '.join(present)}")

    expected_networks = {
        "edge-api": {"database", "redis"},
        "routing-orchestrator": {"redis"},
    }
    for service, required in expected_networks.items():
        networks = set(services[service].get("networks", {}))
        missing = sorted(required - networks)
        if missing:
            fail(f"{service} is missing isolated network(s) {', '.join(missing)}")
    if "database" in services["routing-orchestrator"].get("networks", {}):
        fail("routing-orchestrator must not join the PostgreSQL network")

    print("Compose runtime limits and datastore isolation are internally consistent")


if __name__ == "__main__":
    main()
