#!/usr/bin/env bash
# Helpers for trying the Driver app. Run from the ride-platform repo root.
#   bash scripts/dev/driver-dev.sh list                    the drivers and their state
#   bash scripts/dev/driver-dev.sh approve "<name>"        approve a pending driver (what an operator does)
#   bash scripts/dev/driver-dev.sh where "<name>"          where the platform thinks the driver is
#   bash scripts/dev/driver-dev.sh docs "<name>"           the driver's documents and their review state
#   bash scripts/dev/driver-dev.sh fake-docs "<name>"      DEVELOPMENT ONLY: mark every required document
#                                                          approved without files, so an app without the
#                                                          document screens can be approved and go online
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
    grpc "$DRIVER_ADDR" ride.driver.v1.DriverService/ApproveDriver "{\"driver_id\":\"$ID\"}" > /dev/null || {
      echo "the approval failed. Every required document must be approved first:" >&2
      echo "  bash scripts/dev/driver-dev.sh docs \"$1\"   (or, in development, fake-docs)" >&2
      exit 1
    }
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

  docs)
    need_name "${1:-}"
    ID="$(driver_id_for "$1")"
    [ -n "$ID" ] || { echo "no driver is called \"$1\". Try: bash scripts/dev/driver-dev.sh list" >&2; exit 1; }
    echo "type | required | status | number | expires on"
    driver_sql "select t.code || ' | ' || case when t.required then 'required' else 'optional' end || ' | ' ||
                       coalesce(d.status, 'missing') || ' | ' || coalesce(d.document_number, '') || ' | ' ||
                       coalesce(d.expires_on::text, '')
                from driver_document_types t
                left join lateral (
                  select status, document_number, expires_on from driver_documents
                  where driver_id = '$ID' and type_code = t.code and status <> 'superseded'
                    and (t.scope = 'driver'
                         or vehicle_id = (select id from vehicles where driver_id = '$ID' and active))
                  order by (status = 'approved') desc, created_at desc limit 1) d on true
                where t.active
                order by t.sort_order, t.code;"
    ;;

  fake-docs)
    need_name "${1:-}"
    if grep -qs '^ENVIRONMENT=production' services/driver-service/.env; then
      echo "refusing: services/driver-service/.env says ENVIRONMENT=production" >&2
      exit 1
    fi
    ID="$(driver_id_for "$1")"
    [ -n "$ID" ] || { echo "no driver is called \"$1\". Try: bash scripts/dev/driver-dev.sh list" >&2; exit 1; }
    driver_sql "insert into driver_documents
                  (id, driver_id, type_code, media_id, document_number, expires_on, status, reviewed_at, vehicle_id)
                select gen_random_uuid(), '$ID', t.code, gen_random_uuid(),
                       case when t.requires_number then 'DEV-' || left(md5('$ID' || t.code), 12) else '' end,
                       case when t.requires_expiry then current_date + 365 end,
                       'approved', now(), car.id
                from driver_document_types t
                left join vehicles car on t.scope = 'vehicle' and car.driver_id = '$ID' and car.active
                where t.active and t.required
                  and not exists (select 1 from driver_documents d
                                  where d.driver_id = '$ID' and d.type_code = t.code and d.status = 'approved'
                                    and d.vehicle_id is not distinct from car.id)
                on conflict do nothing;" > /dev/null || { echo "could not mark the documents" >&2; exit 1; }
    echo "every required document of \"$1\" is now approved (placeholders, no files). Next:"
    echo "  bash scripts/dev/driver-dev.sh approve \"$1\""
    ;;

  *)
    echo "usage: bash scripts/dev/driver-dev.sh list | approve \"<name>\" | where \"<name>\" | docs \"<name>\" | fake-docs \"<name>\"" >&2
    exit 1
    ;;
esac
