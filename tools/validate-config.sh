#!/usr/bin/env sh
set -eu

docker compose config --quiet
sh ./tools/check-image-pins.sh
python3 ./tools/assert-compose-runtime-env.py

if command -v jq >/dev/null 2>&1; then
  find observability/grafana/dashboards -type f -name '*.json' -exec jq -e . {} \; >/dev/null
else
  echo "jq is required to validate Grafana dashboards" >&2
  exit 1
fi

docker run --rm \
  -v "$(pwd)/observability/prometheus:/etc/prometheus:ro" \
  prom/prometheus:v3.4.1@sha256:9abc6cf6aea7710d163dbb28d8eeb7dc5baef01e38fa4cd146a406dd9f07f70d \
  promtool check config /etc/prometheus/prometheus.yml
