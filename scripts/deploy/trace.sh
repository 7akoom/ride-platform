#!/usr/bin/env bash
# Finds one request in every service's log by its ID: the X-Request-Id the
# gateway returned (the same ID is the trace in Grafana). Run on the server:
#   bash scripts/deploy/trace.sh 4bf92f3577b34da6a3ce929d0e0e4736 [--since 24h]
set -uo pipefail

id="${1:-}"
[[ "$id" =~ ^[0-9a-f]{32}$ ]] || { echo "usage: bash scripts/deploy/trace.sh <32-character request id> [--since 24h]" >&2; exit 2; }
since=24h
[ "${2:-}" = --since ] && since="${3:-24h}"

found=0
for container in $(docker ps -a --filter name=ride- --format '{{.Names}}' | sort); do
  lines="$(docker logs --since "$since" "$container" 2>&1 | grep -F "$id")"
  [ -n "$lines" ] || continue
  found=1
  echo "== $container"
  echo "$lines" | python3 -c '
import json, sys
for raw in sys.stdin:
    raw = raw.strip()
    try:
        line = json.loads(raw)
    except ValueError:
        print("  " + raw)
        continue
    head = " ".join(str(line.pop(k, "")) for k in ("time", "level", "msg"))
    for k in ("service", "environment", "trace_id"):
        line.pop(k, None)
    print("  " + head + "  " + " ".join(f"{k}={v}" for k, v in line.items()))
'
done

if [ "$found" = 0 ]; then
  echo "no log line with $id in the last $since."
  echo "Quick successful requests are not logged; with monitoring on, find the trace in Grafana (Explore > Tempo > $id)."
fi
