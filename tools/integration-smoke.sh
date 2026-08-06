#!/usr/bin/env sh
set -eu

base_url="${BASE_URL:-http://localhost:8080}"
payload='{"origin":{"latitude":55.751244,"longitude":37.618423},"destination":{"latitude":55.796127,"longitude":37.537922},"waypoints":[],"routingMode":"GREENEST","maxExtraDistanceMeters":30000,"maxExtraDistancePercent":50,"maxExtraTimeSeconds":1200,"avoidTolls":false,"avoidUnpaved":false,"strictness":0.8,"maxProviderRequests":8,"searchDeadlineMs":10000}'
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

curl --fail --silent --show-error "$base_url/health/live" >/dev/null
curl --fail --silent --show-error "$base_url/health/ready" >/dev/null

code=$(curl --silent --show-error \
  --output "$tmp_dir/first.json" \
  --write-out '%{http_code}' \
  --header 'Content-Type: application/json' \
  --header 'Accept: application/json' \
  --header 'Idempotency-Key: integration-smoke-0001' \
  --data "$payload" \
  "$base_url/api/v1/route-searches")
case "$code" in 200|202) ;; *) echo "unexpected create status: $code" >&2; cat "$tmp_dir/first.json" >&2; exit 1;; esac

search_id=$(jq -er '.searchId | select(type == "string" and length > 0)' "$tmp_dir/first.json")
jq -e '.status | type == "string" and length > 0' "$tmp_dir/first.json" >/dev/null

repeat_code=$(curl --silent --show-error \
  --output "$tmp_dir/repeat.json" \
  --write-out '%{http_code}' \
  --header 'Content-Type: application/json' \
  --header 'Accept: application/json' \
  --header 'Idempotency-Key: integration-smoke-0001' \
  --data "$payload" \
  "$base_url/api/v1/route-searches")
case "$repeat_code" in 200|202) ;; *) echo "unexpected idempotent replay status: $repeat_code" >&2; exit 1;; esac
repeat_id=$(jq -er '.searchId' "$tmp_dir/repeat.json")
test "$search_id" = "$repeat_id" || { echo "idempotency replay created a different search" >&2; exit 1; }

curl --fail --silent --show-error "$base_url/api/v1/route-searches/$search_id" | jq -e --arg id "$search_id" '.searchId == $id' >/dev/null

cancel_code=$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' --request DELETE "$base_url/api/v1/route-searches/$search_id")
test "$cancel_code" = "204" || { echo "unexpected cancel status: $cancel_code" >&2; exit 1; }
