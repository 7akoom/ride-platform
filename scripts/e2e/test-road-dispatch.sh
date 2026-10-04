#!/usr/bin/env bash
# End-to-end test of ranking drivers by time to the pickup by road (P11a), on
# the real services and the real OSRM. Run from the ride-platform repo root:
#   bash scripts/e2e/test-road-dispatch.sh
#
# Needs grpcurl, buf, the local identity signing key, OSRM running (port 5000)
# and dispatch-service with DISPATCH_ROAD_RANKING on (the default).
#
# What it proves:
#   1. location-service GetTravelTimes answers OSRM's times for several drivers
#      at once, in their order (cross-checked with OSRM itself); more than 50
#      origins is refused; a user cannot call it, only services
#   2. dispatch gives the trip to the driver fastest to the pickup by road (not
#      necessarily the nearest in a straight line) and says how far away by road
#      they are (pickup_eta_seconds)
#
# Three test drivers are put a few hundred metres to a couple of kilometres from
# a pickup in Erbil; the trip is written straight into trip-service's database
# (no trip.requested, so auto-dispatch stays out of it) and DispatchTrip is
# called directly. Everything it creates is removed again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
LOCATION_ADDR="${LOCATION_GRPC_ADDRESS:-localhost:50054}"
DISPATCH_ADDR="${DISPATCH_GRPC_ADDRESS:-localhost:50056}"
WALLET_ADDR="${WALLET_GRPC_ADDRESS:-localhost:50058}"
OSRM_URL="${OSRM_URL:-http://127.0.0.1:5000}"
GRPCURL="${GRPCURL:-grpcurl}"
PROTOSET="${PROTOSET:-/tmp/ride.binpb}"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
TRIP="$(uuid)"
UNKNOWN="$(uuid)"
PLATE="RD-$RUN"

# The pickup (near Erbil's citadel) and three drivers around it.
PICKUP_LAT=36.1912
PICKUP_LNG=44.0092
NAMES=(north east south)
LATS=(36.1950 36.1925 36.1790)
LNGS=(44.0100 44.0250 44.0080)

INTERNAL_TOKEN=""
for env_file in services/dispatch-service/.env services/trip-service/.env; do
  [ -z "$INTERNAL_TOKEN" ] && [ -f "$env_file" ] && INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' "$env_file" | head -1 | tr -d '"')"
done
INTERNAL_TOKEN="${INTERNAL_TOKEN:-dev-internal-service-token-change-me}"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
DRIVER_IDS=()

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

id_list() {
  local out="'$UNKNOWN'" id
  for id in "${DRIVER_IDS[@]}"; do out="$out, '$id'"; done
  echo "$out"
}

cleanup() {
  rm -rf "$WORK"
  local drivers
  drivers="$(id_list)"
  sql ride-trip-postgres "delete from trip_offers where trip_id = '$TRIP'; delete from trips where id = '$TRIP' or driver_id in ($drivers);" > /dev/null 2>&1 || true
  sql ride-wallet-postgres "
    delete from wallet_transactions where wallet_id in (select id from wallets where owner_id in ($drivers));
    delete from wallets where owner_id in ($drivers);" > /dev/null 2>&1 || true
  sql ride-notification-postgres "delete from notifications where recipient_id in ($drivers);" > /dev/null 2>&1 || true
  sql ride-driver-postgres "delete from drivers where id in ($drivers);" > /dev/null 2>&1 || true
}
trap cleanup EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1"; }

body_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE"
}

http() { # <method> <path> <token> [json body]
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$1" "$BASE$2" -H "Authorization: Bearer $3")
  [ -n "${4:-}" ] && args+=(-H 'Content-Type: application/json' -d "$4")
  curl "${args[@]}"
}

grpc() { # <token> <address> <method> <json> -> body in BODY_FILE, exit code of grpcurl
  "$GRPCURL" -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$4" "$2" "$3" > "$BODY_FILE" 2>&1
}

check() { # <label> <expected> <actual>
  if [ "$2" = "$3" ]; then
    printf '  ok    %s -> %s\n' "$1" "$3"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "$3"
    FAILURES=$((FAILURES + 1))
  fi
}

echo "==> [0/2] preparing: three drivers near a pickup in Erbil"
[ -n "${E2E_SKIP_BUF:-}" ] || buf build -o "$PROTOSET"

OSRM_TABLE="$OSRM_URL/table/v1/driving/${LNGS[0]},${LATS[0]};${LNGS[1]},${LATS[1]};${LNGS[2]},${LATS[2]};$PICKUP_LNG,$PICKUP_LAT?sources=0;1;2&destinations=3"
curl -fsS --max-time 5 "$OSRM_TABLE" > "$WORK/osrm.json" \
  || { echo "ABORT: OSRM does not answer on $OSRM_URL (docker compose up -d osrm)" >&2; exit 2; }

for i in 0 1 2; do
  identity="$(uuid)"
  token="$(mint "$identity")"
  http POST /v1/drivers "$token" "{\"identityId\":\"$identity\",\"displayName\":\"E2E Road ${NAMES[$i]}\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"White\",\"plateNumber\":\"$PLATE-$i\",\"vehicleClass\":\"economy\",\"year\":2021}}" > /dev/null
  DRIVER_IDS+=("$(body_field 'd["driver"]["id"]')")
  [ "$i" = 0 ] && DRIVER_TOKEN="$token"
done

drivers="$(id_list)"
sql ride-driver-postgres "update drivers set status = 'active', availability_status = 'available' where id in ($drivers);" > /dev/null

for i in 0 1 2; do
  grpc "$INTERNAL_TOKEN" "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp \
    "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"${DRIVER_IDS[$i]}\",\"amount\":\"20000\",\"idempotency_key\":\"e2e-road-$RUN-$i\",\"description\":\"road dispatch e2e\"}" \
    || { echo "ABORT: top-up failed: $(cat "$BODY_FILE")" >&2; exit 1; }
done

sql ride-trip-postgres "
  insert into trips (id, rider_id, status, pickup_latitude, pickup_longitude, dropoff_latitude, dropoff_longitude, vehicle_class)
  values ('$TRIP', gen_random_uuid(), 'requested', $PICKUP_LAT, $PICKUP_LNG, 36.2050, 44.0200, 'economy');" > /dev/null

echo "==> [1/2] travel times from location-service"
ORIGINS="[{\"latitude\":${LATS[0]},\"longitude\":${LNGS[0]}},{\"latitude\":${LATS[1]},\"longitude\":${LNGS[1]}},{\"latitude\":${LATS[2]},\"longitude\":${LNGS[2]}}]"
DESTINATION="{\"latitude\":$PICKUP_LAT,\"longitude\":$PICKUP_LNG}"

grpc "$INTERNAL_TOKEN" "$LOCATION_ADDR" ride.location.v1.LocationService/GetTravelTimes \
  "{\"origins\":$ORIGINS,\"destination\":$DESTINATION}" || true
cp "$BODY_FILE" "$WORK/times.json"

check "three times, all reachable, as OSRM says" True "$(python3 - "$WORK/times.json" "$WORK/osrm.json" <<'PY'
import json, sys
try:
    times = json.load(open(sys.argv[1])).get("times", [])
except ValueError:
    print("not JSON: " + open(sys.argv[1]).read()[:200]); sys.exit()
osrm = [row[0] for row in json.load(open(sys.argv[2]))["durations"]]
ok = len(times) == 3 and all(t.get("reachable") for t in times) and \
     all(abs(t.get("durationSeconds", 0) - o) < 1 for t, o in zip(times, osrm))
print(ok if ok else f"{times} vs OSRM {osrm}")
PY
)"

for i in 0 1 2; do
  printf '        %-5s %5.0f m in a straight line, %s s by road\n' "${NAMES[$i]}" \
    "$(python3 -c "import math; a,b,c,d=map(math.radians,[$PICKUP_LAT,$PICKUP_LNG,${LATS[$i]},${LNGS[$i]}]); print(6371000*2*math.asin(math.sqrt(math.sin((c-a)/2)**2+math.cos(a)*math.cos(c)*math.sin((d-b)/2)**2)))")" \
    "$(python3 -c "import json; print(round(json.load(open('$WORK/times.json'))['times'][$i].get('durationSeconds',0)))")"
done

MANY="$(python3 -c "import json; print(json.dumps([{'latitude':36.19,'longitude':44.01}]*51))")"
grpc "$INTERNAL_TOKEN" "$LOCATION_ADDR" ride.location.v1.LocationService/GetTravelTimes \
  "{\"origins\":$MANY,\"destination\":$DESTINATION}" || true
check "51 origins" InvalidArgument "$(grep -o 'Code: [A-Za-z]*' "$BODY_FILE" | cut -d' ' -f2)"

grpc "$DRIVER_TOKEN" "$LOCATION_ADDR" ride.location.v1.LocationService/GetTravelTimes \
  "{\"origins\":$ORIGINS,\"destination\":$DESTINATION}" || true
check "a driver asking" PermissionDenied "$(grep -o 'Code: [A-Za-z]*' "$BODY_FILE" | cut -d' ' -f2)"

echo "==> [2/2] dispatch picks the fastest by road"
for i in 0 1 2; do
  grpc "$INTERNAL_TOKEN" "$LOCATION_ADDR" ride.location.v1.LocationService/UpdateLocation \
    "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"${DRIVER_IDS[$i]}\",\"coordinates\":{\"latitude\":${LATS[$i]},\"longitude\":${LNGS[$i]}}}" \
    || { echo "ABORT: could not place a driver: $(cat "$BODY_FILE")" >&2; exit 1; }
done

grpc "$INTERNAL_TOKEN" "$LOCATION_ADDR" ride.location.v1.LocationService/FindNearby \
  "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"coordinates\":$DESTINATION,\"radius_meters\":3000,\"limit\":30}" || true
OTHERS="$(python3 -c "
import json,sys
ours = set(sys.argv[1].split(','))
print(len([e for e in json.load(open('$BODY_FILE')).get('entities', []) if e['entityId'] not in ours]))" "$(IFS=,; echo "${DRIVER_IDS[*]}")")"
[ "$OTHERS" = 0 ] || echo "    note: $OTHERS other live driver(s) near the pickup; they may be chosen instead"

grpc "$INTERNAL_TOKEN" "$DISPATCH_ADDR" ride.dispatch.v1.DispatchService/DispatchTrip \
  "{\"trip_id\":\"$TRIP\",\"search_radius_meters\":3000}" || true
cp "$BODY_FILE" "$WORK/dispatch.json"

RESULT="$(python3 - "$WORK/dispatch.json" "$WORK/times.json" "$(IFS=,; echo "${DRIVER_IDS[*]}")" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except ValueError:
    print("dispatch failed: " + open(sys.argv[1]).read()[:200]); sys.exit()
times = [t["durationSeconds"] for t in json.load(open(sys.argv[2]))["times"]]
ids = sys.argv[3].split(",")
fastest = ids[times.index(min(times))]
eta = d.get("pickupEtaSeconds", 0)
print(f"{d.get('driverId') == fastest}|{abs(eta - min(times)) < 2}")
PY
)"
check "the fastest driver by road, with their time" "True|True" "$RESULT"

CHOSEN="$(python3 -c "import json; print(json.load(open('$WORK/dispatch.json')).get('driverId',''))" 2>/dev/null || true)"
for i in 0 1 2; do
  [ "${DRIVER_IDS[$i]}" = "$CHOSEN" ] && echo "        chosen: ${NAMES[$i]}"
done
[ "${DRIVER_IDS[0]}" = "$CHOSEN" ] || echo "        (north is the nearest in a straight line: the road changed the choice)"

# Assigned outright, or offered to them when DISPATCH_OFFER_TTL is set.
check "the trip is theirs (assigned or offered)" yes "$(sql ride-trip-postgres "
  select case when exists (select 1 from trips where id = '$TRIP' and status = 'accepted' and driver_id::text = '$CHOSEN')
              or exists (select 1 from trip_offers where trip_id = '$TRIP' and driver_id::text = '$CHOSEN' and status = 'pending')
         then 'yes' else 'no' end;")"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: dispatch ranks drivers by time to the pickup by road"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
