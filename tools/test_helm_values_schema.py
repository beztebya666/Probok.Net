from __future__ import annotations

import json
import unittest
from pathlib import Path
from typing import Any


SCHEMA = json.loads(
    (Path(__file__).parents[1] / "deploy" / "helm" / "greenroute" / "values.schema.json").read_text(encoding="utf-8")
)


def dereference(node: dict[str, Any]) -> dict[str, Any]:
    while "$ref" in node:
        reference = node["$ref"]
        if not reference.startswith("#/definitions/"):
            raise AssertionError(f"unsupported schema reference {reference}")
        node = SCHEMA["definitions"][reference.rsplit("/", 1)[1]]
    return node


def property_path(path: str) -> dict[str, Any]:
    node = SCHEMA
    for part in path.split(".") if path else ():
        node = dereference(node)["properties"][part]
    return dereference(node)


class HelmValuesSchemaTests(unittest.TestCase):
    def test_fixed_objects_reject_unknown_fields(self) -> None:
        closed_paths = (
            "",
            "global",
            "images",
            "serviceAccount",
            "authProxy",
            "services",
            "config",
            "config.features",
            "secrets",
            "secrets.keys",
            "migrations",
            "ingress",
            "ingress.tls",
            "autoscaling",
            "autoscaling.behavior",
            "podDisruptionBudget",
            "monitoring",
            "monitoring.serviceMonitor",
            "monitoring.prometheusRule",
            "monitoring.slo",
            "networkPolicy",
            "networkPolicy.ingressController",
            "networkPolicy.monitoring",
            "networkPolicy.observability",
            "podSecurityContext",
            "podSecurityContext.seccompProfile",
            "containerSecurityContext",
            "containerSecurityContext.capabilities",
        )
        for path in closed_paths:
            with self.subTest(path=path or "<root>"):
                self.assertIs(property_path(path).get("additionalProperties"), False)
        for definition in ("image", "service", "resources", "resourcePair", "localObjectReference", "scalingRules"):
            with self.subTest(definition=definition):
                self.assertIs(dereference(SCHEMA["definitions"][definition]).get("additionalProperties"), False)

    def test_feature_and_secret_key_sets_are_explicit_and_required(self) -> None:
        expected_features = {
            "enableEnhancedSearch",
            "enableAvoidZoneGeneration",
            "enableCorridorAnchors",
            "enableRouteReranking",
            "enableLiveRerouting",
            "enableProviderCache",
            "providerDataStorageAllowed",
            "providerDataModificationAllowed",
            "enableAnonymousUsage",
            "enableAdminDebugDetails",
            "allowExperimentalProviderSources",
        }
        expected_secrets = {
            "databaseUrl",
            "redisUrl",
            "oauth2RedisUrl",
            "yandexRouterApiKey",
            "yandexGeocoderApiKey",
            "dgisApiKey",
            "yandexMapsBrowserApiKey",
            "twoGisMapGLBrowserApiKey",
            "internalApiToken",
            "auditHashKey",
            "abuseHashKey",
            "abusePreviousHashKey",
            "oauth2ClientSecret",
            "oauth2CookieSecret",
        }
        for path, expected in (("config.features", expected_features), ("secrets.keys", expected_secrets)):
            node = property_path(path)
            self.assertEqual(set(node["properties"]), expected)
            self.assertEqual(set(node["required"]), expected)

    def test_hardening_settings_are_typed_and_required(self) -> None:
        required = set(property_path("config")["required"])
        expected = {
            "maxConcurrentEdgeRequests",
            "maxConcurrentSSE",
            "maxSSEPerPrincipal",
            "sseMaxLifetime",
            "sseIdleTimeout",
            "auditRetention",
            "auditPurgeInterval",
            "maxConcurrentSearches",
            "providerResponseHeaderTimeout",
            "providerBulkheadWaitTimeout",
            "providerRetryBaseDelay",
            "providerRetryMaxDelay",
            "providerMaxRequestBodyBytes",
            "providerMaxResponseBytes",
        }
        self.assertTrue(expected <= required)
        properties = property_path("config")["properties"]
        self.assertEqual(properties["maxConcurrentSearches"]["maximum"], 1000)
        self.assertEqual(properties["maxConcurrentSSE"]["maximum"], 1000)
        self.assertEqual(properties["maxSSEPerPrincipal"]["maximum"], 20)


if __name__ == "__main__":
    unittest.main()
