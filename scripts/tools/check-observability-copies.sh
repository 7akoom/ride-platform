#!/usr/bin/env bash
# tracing_runtime.go (and its test) is copied byte-for-byte into every service
# and the gateway. Checks the copies are identical. Run from the repo root:
#   bash scripts/tools/check-observability-copies.sh
set -uo pipefail

status=0
for file in tracing_runtime.go tracing_runtime_test.go; do
  reference="services/trip-service/internal/observability/$file"
  for copy in services/*/internal/observability/"$file" infrastructure/gateway/internal/observability/"$file"; do
    cmp -s "$reference" "$copy" || { echo "differs from $reference: $copy" >&2; status=1; }
  done
  count="$(ls services/*/internal/observability/"$file" infrastructure/gateway/internal/observability/"$file" | wc -l)"
  echo "$file: $count copies"
done
[ "$status" = 0 ] && echo "all identical"
exit "$status"
