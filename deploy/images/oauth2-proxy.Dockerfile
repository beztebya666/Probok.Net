# syntax=docker/dockerfile:1.7

# Upstream OAuth2 Proxy release, pinned to the multi-platform manifest digest.
# The release workflow republishes this no-code wrapper with GreenRoute provenance,
# vulnerability scan, SBOM, keyless signature and an immutable deployment digest.
FROM quay.io/oauth2-proxy/oauth2-proxy:v7.15.3@sha256:10a1165743a192e1940b4708fb9647027185ce11a681a1c5519b442ff7f1f561

LABEL org.opencontainers.image.title="GreenRoute OAuth2 Proxy gateway" \
      org.opencontainers.image.description="Pinned OIDC session gateway release dependency"
