#!/usr/bin/env python3
"""Fail when rendered Services/configuration and NetworkPolicies disagree."""

from __future__ import annotations

import re
import sys
import urllib.parse
from pathlib import Path


def fail(message: str) -> None:
    raise SystemExit(f"rendered call-graph assertion failed: {message}")


def resource_documents(rendered: str) -> list[tuple[str, str, str]]:
    resources: list[tuple[str, str, str]] = []
    for document in re.split(r"(?m)^---\s*$", rendered):
        kind = re.search(r"(?m)^kind:\s*([^\s#]+)\s*$", document)
        name = re.search(r"(?m)^  name:\s*([^\s#]+)\s*$", document)
        if kind and name:
            resources.append((kind.group(1), name.group(1), document))
    return resources


def find_resource(resources: list[tuple[str, str, str]], kind: str, suffix: str) -> str:
    matches = [document for candidate_kind, name, document in resources if candidate_kind == kind and name.endswith(suffix)]
    if len(matches) != 1:
        fail(f"expected exactly one {kind} ending in {suffix!r}, found {len(matches)}")
    return matches[0]


def section(document: str, name: str) -> str:
    match = re.search(
        rf"(?ms)^  {re.escape(name)}:\s*\n(.*?)(?=^  [A-Za-z][A-Za-z0-9-]*:\s*(?:\n|$)|\Z)",
        document,
    )
    if not match:
        fail(f"missing {name} section")
    return match.group(1)


def rules(document: str, direction: str) -> list[str]:
    body = section(document, direction)
    return [block for block in re.split(r"(?m)(?=^    - (?:to|from):\s*$)", body) if re.match(r"^    - (?:to|from):", block)]


def assert_owner(document: str, owner: str) -> None:
    header = document.split("  policyTypes:", 1)[0]
    if f"app.kubernetes.io/component: {owner}" not in header:
        fail(f"policy owner is not {owner}")


def assert_peer(document: str, direction: str, peer: str, port: int) -> None:
    port_pattern = re.compile(rf"(?m)^\s*-\s*\{{protocol:\s*TCP,\s*port:\s*{port}\}}\s*$")
    for rule in rules(document, direction):
        if f"app.kubernetes.io/component: {peer}" in rule and port_pattern.search(rule):
            return
    fail(f"{direction} has no peer={peer} TCP/{port} rule")


def assert_service(document: str, port: int) -> None:
    if not re.search(rf"\bport:\s*{port}\b", document):
        fail(f"Service does not expose TCP/{port}")


def require(document: str, pattern: str, message: str) -> None:
    if not re.search(pattern, document, re.MULTILINE | re.DOTALL):
        fail(message)


def reserved_hostname(hostname: str) -> bool:
    hostname = hostname.lower().rstrip(".")
    labels = hostname.split(".")
    return (
        not hostname
        or labels[-1] in {"example", "invalid", "localhost", "test"}
        or hostname in {"example.com", "example.net", "example.org"}
        or hostname.endswith((".example.com", ".example.net", ".example.org"))
    )


def environment_names(document: str) -> set[str]:
    return set(re.findall(r"(?m)^\s+- name:\s*([A-Z][A-Z0-9_]*)\s*$", document))


def assert_environment_scope(document: str, workload: str, required: set[str], forbidden: set[str]) -> None:
    names = environment_names(document)
    missing = sorted(required - names)
    unexpected = sorted(forbidden & names)
    if missing:
        fail(f"{workload} is missing environment references {', '.join(missing)}")
    if unexpected:
        fail(f"{workload} unexpectedly receives {', '.join(unexpected)}")


def assert_ingress_route(document: str, path: str, component: str) -> None:
    require(
        document,
        rf"^\s*- path:\s*{re.escape(path)}\s*$.*?^\s*name:\s*[^\s#]*-{re.escape(component)}\s*$",
        f"Ingress {path} does not route to {component}",
    )


def main() -> None:
    if len(sys.argv) not in (2, 3) or (len(sys.argv) == 3 and sys.argv[2] != "--require-image-digests"):
        fail("usage: assert-rendered-call-graph.py RENDERED.yaml [--require-image-digests]")
    require_image_digests = len(sys.argv) == 3
    rendered = Path(sys.argv[1]).read_text(encoding="utf-8")
    resources = resource_documents(rendered)

    if require_image_digests:
        for kind, name, document in resources:
            if kind not in ("Deployment", "Job"):
                continue
            images = re.findall(r"(?m)^\s+image:\s*[\"']?([^\s\"']+)[\"']?\s*$", document)
            if not images:
                fail(f"{kind}/{name} contains no container image")
            for container_image in images:
                if not re.search(r"@sha256:[a-f0-9]{64}$", container_image):
                    fail(f"{kind}/{name} uses mutable image reference {container_image}")

    config = find_resource(resources, "ConfigMap", "-config")
    for key, service, port in (
        ("ORCHESTRATOR_URL", "routing-orchestrator", 8081),
        ("PROVIDER_URL", "provider-yandex", 8082),
    ):
        if not re.search(rf"(?m)^  {key}:\s*[\"']?http://[^\s\"']*-{service}:{port}[\"']?\s*$", config):
            fail(f"ConfigMap {key} does not target {service}:{port}")

    required_runtime_configuration = (
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
        "AUDIT_RETENTION",
        "AUDIT_PURGE_INTERVAL",
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
        "SEARCH_STATE_TTL",
        "MAX_ACTIVE_CANDIDATES",
        "MAX_CONCURRENT_SEARCHES",
        "MAX_ENHANCED_ITERATIONS",
        "MINIMUM_SCORE_IMPROVEMENT",
        "SSE_HEARTBEAT_INTERVAL",
    )
    for key in required_runtime_configuration:
        require(config, rf"^  {key}:\s*[^\s]+\s*$", f"ConfigMap is missing hardened runtime setting {key}")
    for key in ("PROVIDER_MAX_REQUEST_BODY_BYTES", "PROVIDER_MAX_RESPONSE_BYTES"):
        require(config, rf'^  {key}:\s*["\']?[0-9]+["\']?\s*$', f"ConfigMap {key} must render as a base-10 integer")
    for key in (
        "ENABLE_LIVE_REROUTING",
        "ENABLE_PROVIDER_CACHE",
        "PROVIDER_DATA_STORAGE_ALLOWED",
        "PROVIDER_DATA_MODIFICATION_ALLOWED",
        "ENABLE_ANONYMOUS_USAGE",
        "ENABLE_ADMIN_DEBUG_DETAILS",
        "ALLOW_EXPERIMENTAL_PROVIDER_SOURCES",
    ):
        require(config, rf'^  {key}:\s*["\']?false["\']?\s*$', f"production ConfigMap must fail closed for {key}")
    require(config, r'^  APP_ENV:\s*["\']?production["\']?\s*$', "production ConfigMap has the wrong APP_ENV")
    require(config, r'^  PROVIDER_MODE:\s*["\']?yandex["\']?\s*$', "production ConfigMap must use the official provider mode")
    require(config, r'^  OTEL_EXPORTER_OTLP_INSECURE:\s*["\']?false["\']?\s*$', "production OTLP transport cannot be insecure")

    for component, port in (("edge-api", 8080), ("routing-orchestrator", 8081), ("provider-yandex", 8082), ("web", 3000)):
        assert_service(find_resource(resources, "Service", f"-{component}"), port)

    edge_deployment = find_resource(resources, "Deployment", "-edge-api")
    orchestrator_deployment = find_resource(resources, "Deployment", "-routing-orchestrator")
    provider_deployment = find_resource(resources, "Deployment", "-provider-yandex")
    web_deployment = find_resource(resources, "Deployment", "-web")
    require(edge_deployment, r"^\s*- name:\s*DATABASE_URL\s*$", "edge-api is missing its database credential")
    require(edge_deployment, r"^\s*- name:\s*REDIS_URL\s*$", "edge-api is missing its Redis credential")
    require(edge_deployment, r"^\s*- name:\s*SHUTDOWN_GRACE_PERIOD\s*$", "edge-api is missing its shutdown deadline")
    require(orchestrator_deployment, r"^\s*- name:\s*REDIS_URL\s*$", "routing-orchestrator is missing its Redis credential")
    require(orchestrator_deployment, r"^\s*- name:\s*SHUTDOWN_GRACE_PERIOD\s*$", "routing-orchestrator is missing its shutdown deadline")
    if re.search(r"^\s*- name:\s*DATABASE_URL\s*$", orchestrator_deployment, re.MULTILINE):
        fail("routing-orchestrator must not receive a database credential")
    server_secret_names = {
        "DATABASE_URL",
        "REDIS_URL",
        "INTERNAL_API_TOKEN",
        "AUDIT_HASH_KEY",
        "ABUSE_HASH_KEY",
        "ABUSE_PREVIOUS_HASH_KEY",
        "YANDEX_ROUTER_API_KEY",
        "YANDEX_GEOCODER_API_KEY",
        "OAUTH2_PROXY_CLIENT_SECRET",
        "OAUTH2_PROXY_COOKIE_SECRET",
        "OAUTH2_PROXY_REDIS_CONNECTION_URL",
    }
    edge_secrets = {"DATABASE_URL", "REDIS_URL", "INTERNAL_API_TOKEN", "AUDIT_HASH_KEY", "ABUSE_HASH_KEY", "ABUSE_PREVIOUS_HASH_KEY"}
    assert_environment_scope(
        edge_deployment,
        "edge-api",
        edge_secrets,
        server_secret_names - edge_secrets,
    )
    assert_environment_scope(
        orchestrator_deployment,
        "routing-orchestrator",
        {"REDIS_URL", "INTERNAL_API_TOKEN"},
        server_secret_names - {"REDIS_URL", "INTERNAL_API_TOKEN"},
    )
    assert_environment_scope(
        provider_deployment,
        "provider-yandex",
        {"INTERNAL_API_TOKEN", "YANDEX_ROUTER_API_KEY", "YANDEX_GEOCODER_API_KEY"},
        server_secret_names - {"INTERNAL_API_TOKEN", "YANDEX_ROUTER_API_KEY", "YANDEX_GEOCODER_API_KEY"},
    )
    assert_environment_scope(web_deployment, "web", set(), server_secret_names)
    migration_job = find_resource(resources, "Job", "-migrate")
    assert_environment_scope(migration_job, "migration", {"DATABASE_URL"}, server_secret_names - {"DATABASE_URL"})

    expectations = (
        ("-edge-egress", "edge-api", "egress", "routing-orchestrator", 8081),
        ("-edge-egress", "edge-api", "egress", "provider-yandex", 8082),
        ("-edge-internal-ingress", "edge-api", "ingress", "web", 8080),
        ("-orchestrator", "routing-orchestrator", "ingress", "edge-api", 8081),
        ("-orchestrator", "routing-orchestrator", "egress", "provider-yandex", 8082),
        ("-provider", "provider-yandex", "ingress", "routing-orchestrator", 8082),
        ("-provider", "provider-yandex", "ingress", "edge-api", 8082),
        ("-web-egress", "web", "egress", "edge-api", 8080),
    )
    for suffix, owner, direction, peer, port in expectations:
        policy = find_resource(resources, "NetworkPolicy", suffix)
        assert_owner(policy, owner)
        assert_peer(policy, direction, peer, port)

    edge_egress = section(find_resource(resources, "NetworkPolicy", "-edge-egress"), "egress")
    orchestrator_egress = section(find_resource(resources, "NetworkPolicy", "-orchestrator"), "egress")
    require(edge_egress, r"port:\s*5432\b", "edge-api cannot reach PostgreSQL")
    require(edge_egress, r"port:\s*6379\b", "edge-api cannot reach Redis")
    require(orchestrator_egress, r"port:\s*6379\b", "routing-orchestrator cannot reach Redis")
    if re.search(r"port:\s*5432\b", orchestrator_egress):
        fail("routing-orchestrator must not have PostgreSQL egress")

    oauth_service = find_resource(resources, "Service", "-oauth2-proxy")
    assert_service(oauth_service, 4180)
    assert_service(oauth_service, 44180)

    oauth_deployment = find_resource(resources, "Deployment", "-oauth2-proxy")
    assert_environment_scope(
        oauth_deployment,
        "oauth2-proxy",
        {"OAUTH2_PROXY_REDIS_CONNECTION_URL", "OAUTH2_PROXY_CLIENT_SECRET", "OAUTH2_PROXY_COOKIE_SECRET"},
        server_secret_names - {"OAUTH2_PROXY_REDIS_CONNECTION_URL", "OAUTH2_PROXY_CLIENT_SECRET", "OAUTH2_PROXY_COOKIE_SECRET"},
    )
    for argument in (
        "--code-challenge-method=S256",
        "--set-authorization-header=true",
        "--session-store-type=redis",
        "--cookie-secure=true",
        "--cookie-httponly=true",
        "--cookie-samesite=lax",
        "--insecure-oidc-skip-nonce=false",
        "--request-logging=false",
        "--auth-logging=false",
    ):
        if argument not in oauth_deployment:
            fail(f"OAuth2 Proxy is missing security argument {argument}")
    if require_image_digests:
        email_domains = re.findall(r"--email-domain=([^\s\"']+)", oauth_deployment)
        if not email_domains or any(domain == "*" or reserved_hostname(domain) for domain in email_domains):
            fail("OAuth2 Proxy needs an explicit non-placeholder email allowlist")
        issuer_match = re.search(r"--oidc-issuer-url=([^\s\"']+)", oauth_deployment)
        issuer = urllib.parse.urlsplit(issuer_match.group(1) if issuer_match else "")
        if issuer.scheme != "https" or reserved_hostname(issuer.hostname or ""):
            fail("OAuth2 Proxy needs a non-placeholder HTTPS issuer")
    require(
        oauth_deployment,
        r"--trusted-proxy-ip=(?!0\.0\.0\.0/0|::/0)[^\s\"']+",
        "OAuth2 Proxy must have a narrowed trusted-proxy-ip",
    )
    oauth_image_pattern = (
        r"image:\s*[\"']?[^\s\"']+@sha256:[a-f0-9]{64}[\"']?"
        if require_image_digests
        else r"image:\s*[\"']?quay\.io/oauth2-proxy/oauth2-proxy@sha256:[a-f0-9]{64}[\"']?"
    )
    require(oauth_deployment, oauth_image_pattern, "OAuth2 Proxy image must be pinned by manifest digest")

    oauth_ingress = find_resource(resources, "Ingress", "-oauth2")
    api_ingress = find_resource(resources, "Ingress", "-api")
    web_ingress = find_resource(resources, "Ingress", "-web")
    if require_image_digests:
        ingress_hosts = {
            match
            for ingress in (oauth_ingress, api_ingress, web_ingress)
            for match in re.findall(r"(?m)^\s*- host:\s*[\"']?([^\s\"']+)", ingress)
        }
        if len(ingress_hosts) != 1 or reserved_hostname(next(iter(ingress_hosts), "")):
            fail("release Ingress needs one explicit non-placeholder hostname")
    assert_ingress_route(oauth_ingress, "/oauth2", "oauth2-proxy")
    assert_ingress_route(api_ingress, "/api", "edge-api")
    assert_ingress_route(web_ingress, "/", "web")
    for ingress, name in ((api_ingress, "API"), (web_ingress, "web")):
        require(
            ingress,
            r"nginx\.ingress\.kubernetes\.io/auth-url:\s*[\"']?http://[^\s\"']*-oauth2-proxy\.[^\s\"']+\.svc\.cluster\.local:4180/oauth2/auth[\"']?",
            f"{name} Ingress auth-url does not target the internal OAuth2 Proxy",
        )
    require(
        api_ingress,
        r"nginx\.ingress\.kubernetes\.io/auth-response-headers:\s*[\"']?Authorization[\"']?",
        "API Ingress does not inject the validated bearer token",
    )
    if "nginx.ingress.kubernetes.io/auth-response-headers: Authorization" in web_ingress:
        fail("web Ingress must not receive the API bearer token")
    if "nginx.ingress.kubernetes.io/auth-signin:" in api_ingress:
        fail("API/SSE Ingress must return 401 instead of redirecting machine requests")
    require(
        api_ingress,
        r"nginx\.ingress\.kubernetes\.io/proxy-buffering:\s*[\"']?off[\"']?",
        "API/SSE Ingress must disable response buffering",
    )
    require(
        web_ingress,
        r"nginx\.ingress\.kubernetes\.io/auth-signin:\s*[\"']?https://\$host/oauth2/start\?rd=\$escaped_request_uri[\"']?",
        "web Ingress does not start the OIDC browser flow",
    )
    if "nginx.ingress.kubernetes.io/auth-url:" in oauth_ingress:
        fail("OAuth2 callback Ingress must not authenticate recursively")

    oauth_policy = find_resource(resources, "NetworkPolicy", "-oauth2-proxy")
    assert_owner(oauth_policy, "oauth2-proxy")
    require(section(oauth_policy, "ingress"), r"port:\s*4180\b", "Ingress controller cannot reach OAuth2 Proxy")
    require(section(oauth_policy, "egress"), r"port:\s*6379\b", "OAuth2 Proxy cannot reach Redis session storage")
    require(section(oauth_policy, "egress"), r"port:\s*443\b", "OAuth2 Proxy cannot reach the OIDC issuer")

    print("rendered application call graph is internally consistent")


if __name__ == "__main__":
    main()
