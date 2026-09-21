#!/usr/bin/env bash
# End-to-end test of GET /v1/trips/{trip_id}/driver, through the gateway, with real user
# tokens: the rider's view of who is driving their trip.
# Run from the repo root:
#   bash scripts/e2e/test-trip-driver.sh
#
#   1. before any driver accepts, there is nobody to show (400)
#   2. once a driver accepted, the rider sees the driver's name, car, plate and rating, as
#      driver-service keeps them, and nothing else (no identity, no phone)
#   3. only the rider of the trip may ask: another rider 403, the trip's own driver 403,
#      no token 401, a trip that does not exist 403
#
# It sets every driver offline for the first part, and needs the local default
# DISPATCH_OFFER_TTL=0 (direct assignment) like the other trip scripts. It cancels its trips.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
DRV="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
RIDER_IDENTITY="a0000000-0000-4000-8000-0000000000a1"
OTHER_RIDER_IDENTITY="a0000000-0000-4000-8000-0000000000a2"
MISSING_ID="00000000-0000-4000-8000-00000000dead"
PROTOSET="/tmp/ride.binpb"

RIDER_ADDR="localhost:50052"
DRIVER_ADDR="localhost:50053"
LOCATION_ADDR="localhost:50054"
TRIP_ADDR="localhost:50055"
WALLET_ADDR="localhost:50058"

FAILURES=0
BODY_FILE="$(mktemp)"
TRIPS_TO_CANCEL=()

call() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" \
    -H "authorization: Bearer $TOKEN" -d "$3" "$1" "$2"
}

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

tsql() { sql ride-trip-postgres "$1"; }

json_field() { # <python expression over d> (reads stdin)
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

body_field() { # <python expression over d>: from the last HTTP response
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE" 2> /dev/null || true
}

mint() { go run scripts/tools/devtoken/main.go -sub "$1"; }

http() { # <method> <path> <token>
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$1" "$BASE$2")

  [ -n "$3" ] && args+=(-H "Authorization: Bearer $3")

  curl "${args[@]}"
}

expect() { # <label> <expected status> <method> <path> <token>
  local actual
  actual="$(http "$3" "$4" "$5")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s: %s\n' "$1" "$2" "$actual" "$(head -c 200 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

same() { # <label> <expected> <actual>
  if [ "$2" = "$3" ]; then
    printf '  ok    %s -> %s\n' "$1" "${3:-(none)}"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "${2:-(none)}" "${3:-(none)}"
    FAILURES=$((FAILURES + 1))
  fi
}

fail() { echo; echo "FAIL: $*" >&2; exit 1; }

cleanup() {
  rm -f "$BODY_FILE"

  for id in "${TRIPS_TO_CANCEL[@]:-}"; do
    [ -n "$id" ] && call "$TRIP_ADDR" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null 2>&1 || true
  done
}
trap cleanup EXIT

request_trip() { # prints the new trip's id
  call "$TRIP_ADDR" ride.trip.v1.TripService/RequestTrip \
    "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\",\"payment_method\":\"cash\"}" > /dev/null

  tsql "select id from trips where rider_id='$RIDER' order by created_at desc limit 1;"
}

echo "==> [1/4] preparing: protoset, the rider, tokens, no driver free"
buf build -o "$PROTOSET"

rider_id_for_identity() { # <identity id>
  call "$RIDER_ADDR" ride.rider.v1.RiderService/GetRiderByIdentity "{\"identity_id\":\"$1\"}" 2> /dev/null \
    | json_field 'd["rider"]["id"]' 2> /dev/null || true
}

RIDER="$(rider_id_for_identity "$RIDER_IDENTITY")"
if [ -z "$RIDER" ]; then
  call "$RIDER_ADDR" ride.rider.v1.RiderService/CreateRider \
    "{\"identity_id\":\"$RIDER_IDENTITY\",\"display_name\":\"Settlement Test Rider\"}" > /dev/null
  RIDER="$(rider_id_for_identity "$RIDER_IDENTITY")"
fi
[ -n "$RIDER" ] || fail "could not find or create the test rider"

DRIVER_IDENTITY="$(call "$DRIVER_ADDR" ride.driver.v1.DriverService/GetDriver "{\"driver_id\":\"$DRV\"}" | json_field 'd["driver"]["identityId"]')"
[ -n "$DRIVER_IDENTITY" ] || fail "the test driver has no identity"

RIDER_TOKEN="$(mint "$RIDER_IDENTITY")"
OTHER_RIDER_TOKEN="$(mint "$OTHER_RIDER_IDENTITY")"
DRIVER_TOKEN="$(mint "$DRIVER_IDENTITY")"

for id in $(tsql "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted','in_progress');"); do
  call "$TRIP_ADDR" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
done

sql ride-driver-postgres "update drivers set availability_status='offline';" > /dev/null

echo "==> [2/4] a trip no driver has accepted"
WAITING_TRIP="$(request_trip)"
TRIPS_TO_CANCEL+=("$WAITING_TRIP")
same "the trip is waiting" "requested|" "$(tsql "select status || '|' || coalesce(driver_id::text,'') from trips where id='$WAITING_TRIP';")"
expect "there is nobody to show yet" 400 GET "/v1/trips/$WAITING_TRIP/driver" "$RIDER_TOKEN"

call "$TRIP_ADDR" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$WAITING_TRIP\",\"reason\":\"test cleanup\"}" > /dev/null

echo "==> [3/4] a trip a driver accepted"
if [ "$(sql ride-wallet-postgres "select coalesce(sum(balance),0) < 50000 from wallets where owner_id='$DRV' and owner_type='driver';")" = "t" ]; then
  call "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp \
    "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRV\",\"amount\":\"100000\",\"idempotency_key\":\"e2e-trip-driver-$(date +%s)\",\"description\":\"e2e test deposit\"}" > /dev/null
fi

sql ride-driver-postgres "update drivers set availability_status='available' where id='$DRV';" > /dev/null

call "$LOCATION_ADDR" ride.location.v1.LocationService/UpdateLocation \
  "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRV\",\"coordinates\":{\"latitude\":36.1905,\"longitude\":44.0105}}" > /dev/null

TRIP="$(request_trip)"
TRIPS_TO_CANCEL+=("$TRIP")
echo "    trip=$TRIP"

state=""
for _ in $(seq 1 30); do
  state="$(tsql "select status || '|' || coalesce(driver_id::text,'') from trips where id='$TRIP';")"
  [ "$state" = "accepted|$DRV" ] && break
  sleep 1
done
[ "$state" = "accepted|$DRV" ] || fail "trip was not dispatched (got: $state)"

DRIVER_URL="/v1/trips/$TRIP/driver"

expect "the rider sees the driver" 200 GET "$DRIVER_URL" "$RIDER_TOKEN"
same "the name is the one driver-service keeps" "$(sql ride-driver-postgres "select display_name from drivers where id='$DRV';")" "$(body_field 'd["driver"]["displayName"]')"
same "the plate is the one driver-service keeps" "$(sql ride-driver-postgres "select vehicle_plate_number from drivers where id='$DRV';")" "$(body_field 'd["driver"]["vehicle"]["plateNumber"]')"
same "the car's make" "$(sql ride-driver-postgres "select vehicle_make from drivers where id='$DRV';")" "$(body_field 'd["driver"]["vehicle"]["make"]')"
same "the rating count" "$(sql ride-driver-postgres "select rating_count from drivers where id='$DRV';")" "$(body_field 'd["driver"]["ratingCount"]')"
same "nothing else about the driver is shown" "displayName,ratingAverage,ratingCount,vehicle" "$(body_field '",".join(sorted(d["driver"].keys()))')"

if grep -q "$DRIVER_IDENTITY" "$BODY_FILE"; then
  echo "  FAIL  the answer contains the driver's identity id"
  FAILURES=$((FAILURES + 1))
else
  echo "  ok    the driver's identity id is not in the answer"
fi

echo "==> [4/4] nobody else can ask"
expect "another rider" 403 GET "$DRIVER_URL" "$OTHER_RIDER_TOKEN"
expect "the trip's own driver" 403 GET "$DRIVER_URL" "$DRIVER_TOKEN"
expect "no token" 401 GET "$DRIVER_URL" ""
expect "a trip that does not exist" 403 GET "/v1/trips/$MISSING_ID/driver" "$RIDER_TOKEN"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: the rider sees who is driving their trip, and only that"
