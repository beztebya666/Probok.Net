#!/usr/bin/env python3
"""Kubernetes exec credential backed by a fresh GitHub Actions OIDC token."""

from __future__ import annotations

import base64
import datetime as dt
import json
import os
import sys
import urllib.parse
import urllib.request


def abort(message: str) -> None:
    print(f"GitHub OIDC credential error: {message}", file=sys.stderr)
    raise SystemExit(1)


def decode_segment(segment: str) -> dict[str, object]:
    try:
        raw = base64.urlsafe_b64decode(segment + "=" * (-len(segment) % 4))
        value = json.loads(raw)
    except (ValueError, json.JSONDecodeError) as error:
        abort(f"malformed JWT payload: {error}")
    if not isinstance(value, dict):
        abort("JWT payload is not an object")
    return value


def main() -> None:
    request_url = os.environ.get("ACTIONS_ID_TOKEN_REQUEST_URL", "")
    request_token = os.environ.get("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")
    audience = os.environ.get("GREENROUTE_KUBE_OIDC_AUDIENCE", "")
    expected_subject = os.environ.get("GREENROUTE_EXPECTED_OIDC_SUBJECT", "")
    if not request_url or not request_token:
        abort("GitHub id-token:write environment is unavailable")
    if not audience:
        abort("GREENROUTE_KUBE_OIDC_AUDIENCE is required")
    if not expected_subject.startswith("repo:") or ":environment:" not in expected_subject:
        abort("GREENROUTE_EXPECTED_OIDC_SUBJECT must identify a protected repository environment")

    parsed_url = urllib.parse.urlparse(request_url)
    hostname = (parsed_url.hostname or "").lower()
    try:
        request_port = parsed_url.port
    except ValueError:
        abort("refusing a malformed GitHub OIDC request endpoint")
    if (
        parsed_url.scheme != "https"
        or not hostname.endswith(".actions.githubusercontent.com")
        or parsed_url.username
        or parsed_url.password
        or parsed_url.fragment
        or request_port not in (None, 443)
    ):
        abort("refusing an unexpected GitHub OIDC request endpoint")
    separator = "&" if parsed_url.query else "?"
    endpoint = request_url + separator + urllib.parse.urlencode({"audience": audience})
    request = urllib.request.Request(
        endpoint,
        headers={
            "Authorization": f"Bearer {request_token}",
            "Accept": "application/json",
            "User-Agent": "greenroute-kubernetes-exec-credential/1",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=15) as response:  # noqa: S310 - endpoint is allowlisted above
            payload = json.load(response)
    except Exception as error:  # The concrete network errors have no useful secret-safe distinction here.
        abort(f"token request failed: {type(error).__name__}")

    token = payload.get("value") if isinstance(payload, dict) else None
    if not isinstance(token, str) or token.count(".") != 2:
        abort("GitHub returned no valid JWT")
    claims = decode_segment(token.split(".")[1])
    if claims.get("iss") != "https://token.actions.githubusercontent.com":
        abort("JWT issuer is not GitHub Actions")
    token_audience = claims.get("aud")
    audiences = token_audience if isinstance(token_audience, list) else [token_audience]
    if audience not in audiences:
        abort("JWT audience does not match the configured cluster audience")
    subject = claims.get("sub")
    if subject != expected_subject:
        abort("JWT subject does not match the expected repository and protected environment")
    expires = claims.get("exp")
    if not isinstance(expires, int):
        abort("JWT has no integer expiration")
    now = int(dt.datetime.now(tz=dt.timezone.utc).timestamp())
    if expires <= now or expires > now + 900:
        abort("JWT expiration is outside the accepted short-lived window")
    expiration = dt.datetime.fromtimestamp(expires, tz=dt.timezone.utc).isoformat().replace("+00:00", "Z")

    json.dump(
        {
            "apiVersion": "client.authentication.k8s.io/v1",
            "kind": "ExecCredential",
            "status": {"expirationTimestamp": expiration, "token": token},
        },
        sys.stdout,
        separators=(",", ":"),
    )


if __name__ == "__main__":
    main()
