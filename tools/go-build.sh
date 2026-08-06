#!/usr/bin/env sh
set -eu

modules=$(find . -name go.mod -type f -not -path './.git/*' -not -path './apps/web/*' -exec dirname {} \; | sort)
if [ -z "$modules" ]; then
  echo "no Go modules found" >&2
  exit 1
fi

for module in $modules; do
  echo "==> go build: $module"
  (cd "$module" && CGO_ENABLED=0 go build -trimpath ./...)
done
