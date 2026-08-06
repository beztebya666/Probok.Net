from __future__ import annotations

import base64
import contextlib
import datetime as dt
import importlib.util
import io
import json
import os
import unittest
from pathlib import Path
from unittest import mock


MODULE_PATH = Path(__file__).with_name("github-oidc-exec-credential.py")
SPEC = importlib.util.spec_from_file_location("github_oidc_exec_credential", MODULE_PATH)
assert SPEC and SPEC.loader
CREDENTIAL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CREDENTIAL)

AUDIENCE = "greenroute-kubernetes"
SUBJECT = "repo:owner/repository:environment:prod"


def encode_segment(value: object) -> str:
    return base64.urlsafe_b64encode(json.dumps(value).encode()).rstrip(b"=").decode()


def make_token(**overrides: object) -> str:
    claims: dict[str, object] = {
        "iss": "https://token.actions.githubusercontent.com",
        "sub": SUBJECT,
        "aud": AUDIENCE,
        "exp": int(dt.datetime.now(tz=dt.timezone.utc).timestamp()) + 300,
    }
    claims.update(overrides)
    return f"{encode_segment({'alg': 'RS256'})}.{encode_segment(claims)}.fixture-signature"


class FakeResponse(io.BytesIO):
    def __enter__(self) -> FakeResponse:
        return self

    def __exit__(self, *_args: object) -> None:
        self.close()


class ExecCredentialTests(unittest.TestCase):
    def environment(self) -> dict[str, str]:
        return {
            "ACTIONS_ID_TOKEN_REQUEST_URL": "https://pipelines.actions.githubusercontent.com/oidc/token",
            "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "non-secret-test-fixture",
            "GREENROUTE_KUBE_OIDC_AUDIENCE": AUDIENCE,
            "GREENROUTE_EXPECTED_OIDC_SUBJECT": SUBJECT,
        }

    def execute(self, token: str, environment: dict[str, str] | None = None) -> tuple[dict[str, object], mock.Mock]:
        response = FakeResponse(json.dumps({"value": token}).encode())
        with (
            mock.patch.dict(os.environ, environment or self.environment(), clear=True),
            mock.patch.object(CREDENTIAL.urllib.request, "urlopen", return_value=response) as request,
            contextlib.redirect_stdout(io.StringIO()) as stdout,
        ):
            CREDENTIAL.main()
        return json.loads(stdout.getvalue()), request

    def assert_rejected(self, token: str, environment: dict[str, str] | None = None) -> None:
        response = FakeResponse(json.dumps({"value": token}).encode())
        with (
            mock.patch.dict(os.environ, environment or self.environment(), clear=True),
            mock.patch.object(CREDENTIAL.urllib.request, "urlopen", return_value=response),
            contextlib.redirect_stdout(io.StringIO()),
            contextlib.redirect_stderr(io.StringIO()),
            self.assertRaises(SystemExit),
        ):
            CREDENTIAL.main()

    def test_emits_v1_exec_credential_and_requests_configured_audience(self) -> None:
        token = make_token()
        result, request = self.execute(token)
        self.assertEqual(result["apiVersion"], "client.authentication.k8s.io/v1")
        self.assertEqual(result["kind"], "ExecCredential")
        self.assertEqual(result["status"]["token"], token)  # type: ignore[index]
        requested_url = request.call_args.args[0].full_url
        self.assertIn("audience=greenroute-kubernetes", requested_url)

    def test_rejects_wrong_issuer_subject_audience_and_expiration(self) -> None:
        now = int(dt.datetime.now(tz=dt.timezone.utc).timestamp())
        cases = (
            {"iss": "https://issuer.example"},
            {"sub": "repo:owner/repository:environment:stage"},
            {"aud": "another-cluster"},
            {"exp": now - 1},
            {"exp": now + 3600},
        )
        for claims in cases:
            with self.subTest(claims=claims):
                self.assert_rejected(make_token(**claims))

    def test_rejects_untrusted_request_endpoints_before_network_access(self) -> None:
        endpoints = (
            "https://example.invalid/oidc/token",
            "http://pipelines.actions.githubusercontent.com/oidc/token",
            "https://user@pipelines.actions.githubusercontent.com/oidc/token",
            "https://pipelines.actions.githubusercontent.com:444/oidc/token",
            "https://pipelines.actions.githubusercontent.com/oidc/token#fragment",
        )
        for endpoint in endpoints:
            with self.subTest(endpoint=endpoint):
                environment = self.environment()
                environment["ACTIONS_ID_TOKEN_REQUEST_URL"] = endpoint
                with (
                    mock.patch.dict(os.environ, environment, clear=True),
                    mock.patch.object(CREDENTIAL.urllib.request, "urlopen") as request,
                    contextlib.redirect_stderr(io.StringIO()),
                    self.assertRaises(SystemExit),
                ):
                    CREDENTIAL.main()
                request.assert_not_called()


if __name__ == "__main__":
    unittest.main()
