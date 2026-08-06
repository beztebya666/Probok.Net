#!/usr/bin/env sh
set -eu

kind="${1:-unit}"
case "$kind" in
  unit) tags="" ;;
  contract) tags="contract" ;;
  integration) tags="integration" ;;
  *) echo "usage: $0 {unit|contract|integration}" >&2; exit 2 ;;
esac

modules=$(find . -name go.mod -type f -not -path './.git/*' -not -path './apps/web/*' -exec dirname {} \; | sort)
if [ -z "$modules" ]; then
  echo "no Go modules found" >&2
  exit 1
fi

for module in $modules; do
  echo "==> go test ($kind): $module"
  if [ -n "$tags" ]; then
    (cd "$module" && go test -count=1 -race -tags="$tags" ./...)
  else
    (cd "$module" && go test -count=1 -race ./...)
  fi
done
