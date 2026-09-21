#!/usr/bin/env bash
# Helpers for trying the Driver app. Run from the ride-platform repo root.
#   bash scripts/dev/driver-dev.sh list                    the drivers and their state
#   bash scripts/dev/driver-dev.sh approve "<name>"        approve a pending driver (what an operator does)
#   bash scripts/dev/driver-dev.sh where "<name>"          where the platform thinks the driver is
# <name> is the name the driver typed in the app (the latest driver with that name is used).
set -uo pipefail
export LC_ALL=C.UTF-8

TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"
DRIVER_ADDR="localhost:50053"
LOCATION_ADDR="localhost:50054"

driver_sql() {
  echo "$1" | docker exec -i ride-driver-postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

grpc() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $TOKEN" -d "$3" "$1" "$2"
}

driver_id_for() { # <name>
  local name_sql="${1//\'/\'\'}"
  driver_sql "select id from drivers where display_name = '$name_sql' order by created_at desc limit 1;"
}

need_name() {
  if [ -z "${1:-}" ]; then
    echo "usage: bash scripts/dev/driver-dev.sh $CMD \"<driver name>\"" >&2
    exit 1
  fi
}

CMD="${1:-}"
shift || true

case "$CMD" in
  list)
    echo "name | status | availability | plate"
    driver_sql "select display_name || ' | ' || status || ' | ' || availability_status || ' | ' || vehicle_plate_number from drivers order by created_at desc limit 20;"
    ;;

  approve)
    need_name "${1:-}"
    ID="$(driver_id_for "$1")"
    [ -n "$ID" ] || { echo "no driver is called \"$1\". Try: bash scripts/dev/driver-dev.sh list" >&2; exit 1; }
    buf build -o "$PROTOSET" || { echo "buf build failed: run this from the repo root" >&2; exit 1; }
    grpc "$DRIVER_ADDR" ride.driver.v1.DriverService/ApproveDriver "{\"driver_id\":\"$ID\"}" > /dev/null || { echo "the approval failed" >&2; exit 1; }
    echo "approved \"$1\" ($ID). Now:"
    driver_sql "select display_name || ' | ' || status || ' | ' || availability_status from drivers where id = '$ID';"
    ;;

  where)
    need_name "${1:-}"
    ID="$(driver_id_for "$1")"
    [ -n "$ID" ] || { echo "no driver is called \"$1\". Try: bash scripts/dev/driver-dev.sh list" >&2; exit 1; }
    buf build -o "$PROTOSET" || { echo "buf build failed: run this from the repo root" >&2; exit 1; }
    echo "availability: $(driver_sql "select availability_status from drivers where id = '$ID';")"
    if OUT="$(grpc "$LOCATION_ADDR" ride.location.v1.LocationService/GetLocation "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$ID\"}" 2>&1)"; then
      echo "$OUT" | grep -E 'latitude|longitude|updatedAt'
    else
      echo "no live position (the app is offline, or has not reported for 30 seconds)"
    fi
    ;;

  *)
    echo "usage: bash scripts/dev/driver-dev.sh list | approve \"<name>\" | where \"<name>\"" >&2
    exit 1
    ;;
esac
