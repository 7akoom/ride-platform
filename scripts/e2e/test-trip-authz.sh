#!/usr/bin/env bash
# End-to-end trip authorization test with REAL user tokens.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-trip-authz.sh
#
# Tokens are minted for local development only (scripts/tools/devtoken) and
# are never printed.
#
# What it proves:
#   1. a rider can request a trip only for themselves
#   2. only the rider or the assigned driver of a trip can read or cancel it
#   3. only the assigned driver can start, complete or record its path
#   4. nobody can pass themselves off as the other party when raising SOS
#   5. no user can accept a trip directly, and a missing trip looks the same
#      as someone else's
#   6. the whole normal flow still works with user tokens, and the internal
#      token still works
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"
TRIP_SVC="localhost:50055"

DRIVER_A="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
DRIVER_B="4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
RIDER_IDENTITY_1="a0000000-0000-4000-8000-0000000000a1"
RIDER_IDENTITY_2="a0000000-0000-4000-8000-0000000000a2"

RIDER_PORT="$(awk '/^  rider-service:$/ {inside=1; next} inside && /^  [A-Za-z0-9_-]+:$/ {exit} inside && /GRPC_ADDRESS/ {gsub(/[^0-9]/, ""); print; exit}' infrastructure/compose/compose.yaml)"
[ -n "$RIDER_PORT" ] || { echo "FAIL: could not read rider-service GRPC_ADDRESS from compose.yaml" >&2; exit 1; }
RIDER_ADDR="localhost:$RIDER_PORT"

FAILURES=0

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

mint() { go run scripts/tools/devtoken/main.go -sub "$1" "${@:2}"; }

internal_call() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$3" "$1" "$2"
}

rider_id_for_identity() { # <identity>
  internal_call "$RIDER_ADDR" ride.rider.v1.RiderService/GetRiderByIdentity "{\"identity_id\":\"$1\"}" 2>/dev/null \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["rider"]["id"])' 2>/dev/null || true
}

ensure_rider() { # <identity> <name>
  local id
  id="$(rider_id_for_identity "$1")"

  if [ -z "$id" ]; then
    internal_call "$RIDER_ADDR" ride.rider.v1.RiderService/CreateRider "{\"identity_id\":\"$1\",\"display_name\":\"$2\"}" > /dev/null
    id="$(rider_id_for_identity "$1")"
  fi

  [ -n "$id" ] || { echo "FAIL: could not find or create rider $2" >&2; exit 1; }
  echo "$id"
}

code_of() { # <token> <method> <json>  -> OK or the gRPC code name
  local output
  output="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$3" "$TRIP_SVC" "ride.trip.v1.TripService/$2" 2>&1 || true)"

  if grep -q '^ERROR:' <<<"$output"; then
    awk '/Code:/ {print $2; exit}' <<<"$output"
  else
    echo OK
  fi
}

expect() { # <label> <expected> <token> <method> <json>
  local actual
  actual="$(code_of "$3" "$4" "$5")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "$actual"
    FAILURES=$((FAILURES + 1))
  fi
}

echo "==> [1/6] preparing: protoset, two riders, two drivers, tokens"
buf build -o "$PROTOSET"

RIDER_1="$(ensure_rider "$RIDER_IDENTITY_1" "Authz Test Rider")"
RIDER_2="$(ensure_rider "$RIDER_IDENTITY_2" "Authz Test Rider Two")"

IDENTITY_DRIVER_A="$(sql ride-driver-postgres "select identity_id from drivers where id='$DRIVER_A';")"
IDENTITY_DRIVER_B="$(sql ride-driver-postgres "select identity_id from drivers where id='$DRIVER_B';")"
[ -n "$IDENTITY_DRIVER_A" ] && [ -n "$IDENTITY_DRIVER_B" ] || { echo "FAIL: the test drivers have no identity" >&2; exit 1; }

TOKEN_RIDER_1="$(mint "$RIDER_IDENTITY_1")"
TOKEN_RIDER_2="$(mint "$RIDER_IDENTITY_2")"
TOKEN_DRIVER_A="$(mint "$IDENTITY_DRIVER_A")"
TOKEN_DRIVER_B="$(mint "$IDENTITY_DRIVER_B")"
TOKEN_EXPIRED="$(mint "$RIDER_IDENTITY_1" -ttl=-1h)"

for id in $(sql ride-trip-postgres "select id from trips where rider_id in ('$RIDER_1','$RIDER_2') and status in ('requested','accepted','in_progress') or driver_id in ('$DRIVER_A','$DRIVER_B') and status in ('accepted','in_progress');"); do
  internal_call "$TRIP_SVC" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
done

sql ride-driver-postgres "update drivers set availability_status='available' where id='$DRIVER_A';" > /dev/null
internal_call localhost:50054 ride.location.v1.LocationService/UpdateLocation \
  "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRIVER_A\",\"coordinates\":{\"latitude\":36.1905,\"longitude\":44.0105}}" > /dev/null

echo "==> [2/6] a rider can request a trip only for themselves"
PICKUP='"pickup":{"latitude":36.19,"longitude":44.01},"dropoff":{"latitude":36.2,"longitude":44.02},"vehicle_class":"economy"'
expect "rider 2 requests a trip for rider 1"        PermissionDenied "$TOKEN_RIDER_2"  RequestTrip "{\"rider_id\":\"$RIDER_1\",$PICKUP}"
expect "driver B requests a trip as rider 1"        PermissionDenied "$TOKEN_DRIVER_B" RequestTrip "{\"rider_id\":\"$RIDER_1\",$PICKUP}"
expect "an expired token requests a trip"           Unauthenticated  "$TOKEN_EXPIRED"  RequestTrip "{\"rider_id\":\"$RIDER_1\",$PICKUP}"
expect "rider 1 requests a trip for themselves"     OK               "$TOKEN_RIDER_1"  RequestTrip "{\"rider_id\":\"$RIDER_1\",$PICKUP}"

TRIP="$(sql ride-trip-postgres "select id from trips where rider_id='$RIDER_1' order by created_at desc limit 1;")"
[ -n "$TRIP" ] || { echo "FAIL: the trip was not created" >&2; exit 1; }
echo "trip=$TRIP"

echo "==> waiting 6s for automatic dispatch to assign driver A"
sleep 6
ASSIGNED="$(sql ride-trip-postgres "select status || '|' || coalesce(driver_id::text,'') from trips where id='$TRIP';")"
[ "$ASSIGNED" = "accepted|$DRIVER_A" ] || { echo "FAIL: the trip was not assigned to driver A (got: $ASSIGNED)" >&2; exit 1; }

echo "==> [3/6] strangers cannot touch the trip"
GET="{\"trip_id\":\"$TRIP\"}"
SOS_RIDER="{\"trip_id\":\"$TRIP\",\"triggered_by\":\"SOS_TRIGGERED_BY_RIDER\",\"location\":{\"latitude\":36.19,\"longitude\":44.01}}"
SOS_DRIVER="{\"trip_id\":\"$TRIP\",\"triggered_by\":\"SOS_TRIGGERED_BY_DRIVER\",\"location\":{\"latitude\":36.19,\"longitude\":44.01}}"
WAYPOINT="{\"trip_id\":\"$TRIP\",\"location\":{\"latitude\":36.191,\"longitude\":44.011}}"

for who in "rider 2:$TOKEN_RIDER_2" "driver B:$TOKEN_DRIVER_B"; do
  name="${who%%:*}"; token="${who#*:}"
  expect "$name reads the trip"       PermissionDenied "$token" GetTrip        "$GET"
  expect "$name reads the path"       PermissionDenied "$token" GetTripPath    "$GET"
  expect "$name cancels the trip"     PermissionDenied "$token" CancelTrip     "{\"trip_id\":\"$TRIP\",\"reason\":\"x\"}"
  expect "$name starts the trip"      PermissionDenied "$token" StartTrip      "$GET"
  expect "$name completes the trip"   PermissionDenied "$token" CompleteTrip   "$GET"
  expect "$name records a waypoint"   PermissionDenied "$token" RecordWaypoint "$WAYPOINT"
  expect "$name raises SOS as rider"  PermissionDenied "$token" TriggerSOS     "$SOS_RIDER"
  expect "$name raises SOS as driver" PermissionDenied "$token" TriggerSOS     "$SOS_DRIVER"
done

echo "==> [4/6] participants are limited to their own role"
expect "rider 1 reads the trip"                     OK               "$TOKEN_RIDER_1"  GetTrip        "$GET"
expect "rider 1 reads the path"                     OK               "$TOKEN_RIDER_1"  GetTripPath    "$GET"
expect "rider 1 starts the trip"                    PermissionDenied "$TOKEN_RIDER_1"  StartTrip      "$GET"
expect "rider 1 completes the trip"                 PermissionDenied "$TOKEN_RIDER_1"  CompleteTrip   "$GET"
expect "rider 1 records a waypoint"                 PermissionDenied "$TOKEN_RIDER_1"  RecordWaypoint "$WAYPOINT"
expect "rider 1 raises SOS claiming to be the driver" PermissionDenied "$TOKEN_RIDER_1" TriggerSOS    "$SOS_DRIVER"
expect "driver A reads the trip"                    OK               "$TOKEN_DRIVER_A" GetTrip        "$GET"
expect "driver A records a waypoint"                OK               "$TOKEN_DRIVER_A" RecordWaypoint "$WAYPOINT"
expect "driver A raises SOS claiming to be the rider" PermissionDenied "$TOKEN_DRIVER_A" TriggerSOS   "$SOS_RIDER"

echo "==> [5/6] no direct accept, and a missing trip looks like someone else's"
expect "driver A accepts a trip directly"           PermissionDenied "$TOKEN_DRIVER_A" AcceptTrip "{\"trip_id\":\"$TRIP\",\"driver_id\":\"$DRIVER_A\"}"
expect "rider 1 accepts a trip"                     PermissionDenied "$TOKEN_RIDER_1"  AcceptTrip "{\"trip_id\":\"$TRIP\",\"driver_id\":\"$DRIVER_A\"}"
expect "rider 1 reads a trip that does not exist"   PermissionDenied "$TOKEN_RIDER_1"  GetTrip     '{"trip_id":"00000000-0000-4000-8000-00000000dead"}'
expect "the internal token reads any trip"          OK               "$INTERNAL_TOKEN" GetTrip     "$GET"

echo "==> [6/6] the normal flow works with the drivers' own tokens"
expect "driver A starts the trip"                   OK               "$TOKEN_DRIVER_A" StartTrip    "$GET"
expect "driver A completes the trip"                OK               "$TOKEN_DRIVER_A" CompleteTrip "$GET"

FINAL="$(sql ride-trip-postgres "select status from trips where id='$TRIP';")"
if [ "$FINAL" = "completed" ]; then
  printf '  ok    the trip ended completed\n'
else
  printf '  FAIL  the trip should be completed, it is %s\n' "$FINAL"
  FAILURES=$((FAILURES + 1))
fi

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: only the participants of a trip can act on it, in their own role, and the normal flow works with user tokens"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
