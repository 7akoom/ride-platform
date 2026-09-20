#!/usr/bin/env bash
# End-to-end test of GetDriverLocation: the rider's live view of their driver.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-driver-location.sh
#
# Tokens are minted for local development only (scripts/tools/devtoken) and
# are never printed. It takes about a minute: it waits for automatic dispatch and
# for a driver position to expire.
#
# What it proves:
#   1. the rider of an accepted trip sees the driver's position, and follows it
#      as the driver moves; the answer carries only the position and its time
#   2. nobody else can: not the driver of the trip, not another rider or driver,
#      not for a missing trip; the internal token still can
#   3. a driver who stopped reporting shows up as "not available" (NOT_FOUND),
#      not as a stale position, and recovers as soon as they report again
#   4. it works while the trip is in progress and stops once it is completed
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"

DRIVER_A="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
DRIVER_B="4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
RIDER_IDENTITY_1="a0000000-0000-4000-8000-0000000000a1"
RIDER_IDENTITY_2="a0000000-0000-4000-8000-0000000000a2"
MISSING_ID="00000000-0000-4000-8000-00000000dead"

LAT_1="36.1905"; LNG_1="44.0105"
LAT_2="36.1912"; LNG_2="44.0111"

compose_port() { # <compose service name>
  awk -v svc="$1" '
    $0 ~ ("^  " svc ":$") {inside=1; next}
    inside && /^  [A-Za-z0-9_-]+:$/ {exit}
    inside && /GRPC_ADDRESS/ {gsub(/[^0-9]/, ""); print; exit}
  ' infrastructure/compose/compose.yaml
}

RIDER_PORT="$(compose_port rider-service)"
DRIVER_PORT="$(compose_port driver-service)"
LOCATION_PORT="$(compose_port location-service)"
TRIP_PORT="$(compose_port trip-service)"
[ -n "$RIDER_PORT" ] && [ -n "$DRIVER_PORT" ] && [ -n "$LOCATION_PORT" ] && [ -n "$TRIP_PORT" ] \
  || { echo "FAIL: could not read a GRPC_ADDRESS from compose.yaml" >&2; exit 1; }

RIDER_ADDR="localhost:$RIDER_PORT"
DRIVER_ADDR="localhost:$DRIVER_PORT"
LOCATION_ADDR="localhost:$LOCATION_PORT"
TRIP_ADDR="localhost:$TRIP_PORT"

RIDER_SVC="ride.rider.v1.RiderService"
DRIVER_SVC="ride.driver.v1.DriverService"
LOCATION_SVC="ride.location.v1.LocationService"
TRIP_SVC="ride.trip.v1.TripService"

FAILURES=0

mint() { go run scripts/tools/devtoken/main.go -sub "$1" "${@:2}"; }

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

internal_call() { # <address> <service/method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$3" "$1" "$2"
}

json_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

user_call() { # <address> <service/method> <token> <json>  -> the raw response of an authorized call
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $3" -d "$4" "$1" "$2"
}

code_of() { # <address> <service/method> <token> <json>  -> OK or the gRPC code name
  local output
  output="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $3" -d "$4" "$1" "$2" 2>&1 || true)"

  if grep -q '^ERROR:' <<<"$output"; then
    awk '/Code:/ {print $2; exit}' <<<"$output"
  else
    echo OK
  fi
}

expect() { # <label> <expected> <address> <service/method> <token> <json>
  local actual
  actual="$(code_of "$3" "$4" "$5" "$6")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "${actual:-no response}"
    FAILURES=$((FAILURES + 1))
  fi
}

pass() { printf '  ok    %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

rider_id_for_identity() { # <identity>
  internal_call "$RIDER_ADDR" "$RIDER_SVC/GetRiderByIdentity" "{\"identity_id\":\"$1\"}" 2>/dev/null \
    | json_field 'd["rider"]["id"]' 2>/dev/null || true
}

ensure_rider() { # <identity> <name>
  local id
  id="$(rider_id_for_identity "$1")"

  if [ -z "$id" ]; then
    internal_call "$RIDER_ADDR" "$RIDER_SVC/CreateRider" "{\"identity_id\":\"$1\",\"display_name\":\"$2\"}" > /dev/null
    id="$(rider_id_for_identity "$1")"
  fi

  [ -n "$id" ] || { echo "FAIL: could not find or create rider $2" >&2; exit 1; }
  echo "$id"
}

driver_identity() { # <driver id>
  internal_call "$DRIVER_ADDR" "$DRIVER_SVC/GetDriver" "{\"driver_id\":\"$1\"}" | json_field 'd["driver"]["identityId"]'
}

report() { # <token> <lat> <lng>: driver A reports their own position (also exercises location ownership)
  user_call "$LOCATION_ADDR" "$LOCATION_SVC/UpdateLocation" "$1" \
    "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRIVER_A\",\"coordinates\":{\"latitude\":$2,\"longitude\":$3}}" > /dev/null
}

track() { # <token> -> raw GetDriverLocation response
  user_call "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$1" "$GET"
}

near() { # <actual> <expected>
  python3 -c "import sys; sys.exit(0 if abs(float('$1') - float('$2')) < 1e-6 else 1)"
}

echo "==> [1/7] preparing: protoset, riders, drivers, tokens, a clean slate"
buf build -o "$PROTOSET"

RIDER_1="$(ensure_rider "$RIDER_IDENTITY_1" "Authz Test Rider")"
RIDER_2="$(ensure_rider "$RIDER_IDENTITY_2" "Authz Test Rider Two")"

IDENTITY_DRIVER_A="$(driver_identity "$DRIVER_A")"
IDENTITY_DRIVER_B="$(driver_identity "$DRIVER_B")"
[ -n "$IDENTITY_DRIVER_A" ] && [ -n "$IDENTITY_DRIVER_B" ] || { echo "FAIL: the test drivers have no identity" >&2; exit 1; }

TOKEN_RIDER_1="$(mint "$RIDER_IDENTITY_1")"
TOKEN_RIDER_2="$(mint "$RIDER_IDENTITY_2")"
TOKEN_DRIVER_A="$(mint "$IDENTITY_DRIVER_A")"
TOKEN_DRIVER_B="$(mint "$IDENTITY_DRIVER_B")"
TOKEN_EXPIRED="$(mint "$RIDER_IDENTITY_1" -ttl=-1h)"

for id in $(sql ride-trip-postgres "select id from trips where rider_id in ('$RIDER_1','$RIDER_2') and status in ('requested','accepted','in_progress') or driver_id in ('$DRIVER_A','$DRIVER_B') and status in ('accepted','in_progress');"); do
  internal_call "$TRIP_ADDR" "$TRIP_SVC/CancelTrip" "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
done

internal_call "$DRIVER_ADDR" "$DRIVER_SVC/UpdateAvailability" \
  "{\"driver_id\":\"$DRIVER_A\",\"availability_status\":\"AVAILABILITY_STATUS_AVAILABLE\"}" > /dev/null
report "$TOKEN_DRIVER_A" "$LAT_1" "$LNG_1"

echo "==> [2/7] rider 1 requests a trip and dispatch assigns driver A"
PICKUP='"pickup":{"latitude":36.19,"longitude":44.01},"dropoff":{"latitude":36.2,"longitude":44.02},"vehicle_class":"economy"'
[ "$(code_of "$TRIP_ADDR" "$TRIP_SVC/RequestTrip" "$TOKEN_RIDER_1" "{\"rider_id\":\"$RIDER_1\",$PICKUP}")" = OK ] \
  || { echo "FAIL: rider 1 could not request a trip" >&2; exit 1; }

TRIP="$(sql ride-trip-postgres "select id from trips where rider_id='$RIDER_1' order by created_at desc limit 1;")"
[ -n "$TRIP" ] || { echo "FAIL: the trip was not created" >&2; exit 1; }
echo "trip=$TRIP"
GET="{\"trip_id\":\"$TRIP\"}"

echo "==> waiting for automatic dispatch (up to 60s)"
ASSIGNED=""
for _ in $(seq 1 30); do
  sleep 2
  ASSIGNED="$(sql ride-trip-postgres "select status || '|' || coalesce(driver_id::text,'') from trips where id='$TRIP';")"
  [ "$ASSIGNED" = "accepted|$DRIVER_A" ] && break
done

if [ "$ASSIGNED" != "accepted|$DRIVER_A" ]; then
  echo "FAIL: the trip was not assigned to driver A within 60s (got: $ASSIGNED)" >&2
  echo "      If trip, location, rider, driver or wallet were just recreated, dispatch may still hold" >&2
  echo "      connections to their old containers: docker compose restart dispatch-service" >&2
  exit 1
fi

echo "==> [3/7] the rider follows the driver on the map"
report "$TOKEN_DRIVER_A" "$LAT_1" "$LNG_1"
RESPONSE="$(track "$TOKEN_RIDER_1")"

if near "$(json_field 'd["location"]["latitude"]' <<<"$RESPONSE")" "$LAT_1" && near "$(json_field 'd["location"]["longitude"]' <<<"$RESPONSE")" "$LNG_1"; then
  pass "rider 1 sees driver A at the reported position"
else
  fail "rider 1 does not see driver A where they reported being: $RESPONSE"
fi

KEYS="$(json_field 'sorted(d.keys())' <<<"$RESPONSE")"
if [ "$KEYS" = "['location', 'updatedAt']" ]; then
  pass "the answer carries only the position and when it was reported"
else
  fail "the answer carries more than the position and its time: $KEYS"
fi

report "$TOKEN_DRIVER_A" "$LAT_2" "$LNG_2"
if near "$(json_field 'd["location"]["latitude"]' <<<"$(track "$TOKEN_RIDER_1")")" "$LAT_2"; then
  pass "rider 1 sees the driver move"
else
  fail "rider 1 did not see the driver move"
fi

echo "==> [4/7] only the rider of the trip sees it"
expect "driver A (the driver of the trip)"  PermissionDenied "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$TOKEN_DRIVER_A" "$GET"
expect "rider 2"                            PermissionDenied "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$TOKEN_RIDER_2"  "$GET"
expect "driver B"                           PermissionDenied "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$TOKEN_DRIVER_B" "$GET"
expect "a trip that does not exist"         PermissionDenied "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$TOKEN_RIDER_1"  "{\"trip_id\":\"$MISSING_ID\"}"
expect "an empty trip id"                   PermissionDenied "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$TOKEN_RIDER_1"  '{"trip_id":""}'
expect "an expired token"                   Unauthenticated  "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$TOKEN_EXPIRED"  "$GET"
expect "a malformed token"                  Unauthenticated  "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "not-a-token"      "$GET"
expect "the internal token"                 OK               "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$INTERNAL_TOKEN" "$GET"
expect "rider 1 still cannot read driver A from location-service directly" \
                                            PermissionDenied "$LOCATION_ADDR" "$LOCATION_SVC/GetLocation" "$TOKEN_RIDER_1" "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRIVER_A\"}"

echo "==> [5/7] a driver who stops reporting is 'not available', not a stale position"
echo "==> waiting 32s for driver A's position to expire (location-service keeps it 30s)"
sleep 32
expect "rider 1 after the position expired" NotFound "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$TOKEN_RIDER_1" "$GET"

report "$TOKEN_DRIVER_A" "$LAT_1" "$LNG_1"
expect "rider 1 once driver A reports again" OK "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$TOKEN_RIDER_1" "$GET"

echo "==> [6/7] while the trip is in progress"
expect "driver A starts the trip" OK "$TRIP_ADDR" "$TRIP_SVC/StartTrip" "$TOKEN_DRIVER_A" "$GET"
report "$TOKEN_DRIVER_A" "$LAT_2" "$LNG_2"
expect "rider 1 sees the driver on the way" OK "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$TOKEN_RIDER_1" "$GET"

echo "==> [7/7] once the trip is completed"
expect "driver A completes the trip" OK "$TRIP_ADDR" "$TRIP_SVC/CompleteTrip" "$TOKEN_DRIVER_A" "$GET"
report "$TOKEN_DRIVER_A" "$LAT_1" "$LNG_1"
expect "rider 1 no longer sees the driver (the trip is over)" FailedPrecondition "$TRIP_ADDR" "$TRIP_SVC/GetDriverLocation" "$TOKEN_RIDER_1" "$GET"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: only the rider of a trip sees the driver's live position, only while the trip is active, and nothing else about the driver"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
