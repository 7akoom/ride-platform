#!/usr/bin/env bash
# End-to-end location authorization test with REAL user tokens.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-location-authz.sh
#
# Tokens are minted for local development only (scripts/tools/devtoken) and
# are never printed.
#
# What it proves:
#   1. a driver writes and reads only their own position, a rider only theirs
#   2. nobody can move or read someone else's position, and a denied write
#      changes nothing
#   3. a driver id is never accepted as a rider entity (or the reverse), and an
#      unspecified entity type or empty id is refused
#   4. FindNearby (the live position of every nearby driver) is internal only
#   5. zones: everyone can check and read, only the internal caller can change
#   6. the internal token still reaches everything (dispatch, pricing and trip
#      depend on it)
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"

DRIVER_A="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
DRIVER_B="4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
RIDER_IDENTITY_1="a0000000-0000-4000-8000-0000000000a1"
RIDER_IDENTITY_2="a0000000-0000-4000-8000-0000000000a2"
MISSING_ID="00000000-0000-4000-8000-00000000dead"

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
[ -n "$RIDER_PORT" ] && [ -n "$DRIVER_PORT" ] && [ -n "$LOCATION_PORT" ] \
  || { echo "FAIL: could not read the rider/driver/location GRPC_ADDRESS from compose.yaml" >&2; exit 1; }

RIDER_ADDR="localhost:$RIDER_PORT"
DRIVER_ADDR="localhost:$DRIVER_PORT"
LOCATION_ADDR="localhost:$LOCATION_PORT"
RIDER_SVC="ride.rider.v1.RiderService"
DRIVER_SVC="ride.driver.v1.DriverService"
LOCATION_SVC="ride.location.v1.LocationService"

LAT_A="36.1905"
LNG_A="44.0105"

FAILURES=0

mint() { go run scripts/tools/devtoken/main.go -sub "$1" "${@:2}"; }

internal_call() { # <address> <service/method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$3" "$1" "$2"
}

json_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

code_of() { # <token> <method> <json>  -> OK or the gRPC code name
  local output
  output="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$3" "$LOCATION_ADDR" "$LOCATION_SVC/$2" 2>&1 || true)"

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
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "${actual:-no response}"
    FAILURES=$((FAILURES + 1))
  fi
}

# For calls whose business result does not matter here (a zone that does not
# exist): only that the authorization layer let the caller through.
expect_authorized() { # <label> <token> <method> <json>
  local actual
  actual="$(code_of "$2" "$3" "$4")"

  if [ -n "$actual" ] && [ "$actual" != "PermissionDenied" ] && [ "$actual" != "Unauthenticated" ]; then
    printf '  ok    %s -> authorized (%s)\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected to pass authorization, got %s\n' "$1" "${actual:-no response}"
    FAILURES=$((FAILURES + 1))
  fi
}

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

loc_update() { # <entity type> <entity id> <lat> <lng>
  echo "{\"entity_type\":\"$1\",\"entity_id\":\"$2\",\"coordinates\":{\"latitude\":$3,\"longitude\":$4}}"
}

loc_get() { # <entity type> <entity id>
  echo "{\"entity_type\":\"$1\",\"entity_id\":\"$2\"}"
}

DRV="ENTITY_TYPE_DRIVER"
RDR="ENTITY_TYPE_RIDER"

echo "==> [1/7] preparing: protoset, two riders, two drivers, tokens"
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

echo "==> [2/7] a driver writes and reads only their own position"
expect "driver A reports their own position"        OK               "$TOKEN_DRIVER_A" UpdateLocation "$(loc_update $DRV "$DRIVER_A" "$LAT_A" "$LNG_A")"
expect "driver A reads their own position"          OK               "$TOKEN_DRIVER_A" GetLocation    "$(loc_get $DRV "$DRIVER_A")"
expect "driver B moves driver A"                    PermissionDenied "$TOKEN_DRIVER_B" UpdateLocation "$(loc_update $DRV "$DRIVER_A" 10.0 10.0)"
expect "driver B reads driver A's position"         PermissionDenied "$TOKEN_DRIVER_B" GetLocation    "$(loc_get $DRV "$DRIVER_A")"
expect "driver A moves a driver that does not exist" PermissionDenied "$TOKEN_DRIVER_A" UpdateLocation "$(loc_update $DRV "$MISSING_ID" "$LAT_A" "$LNG_A")"

LAT_NOW="$(internal_call "$LOCATION_ADDR" "$LOCATION_SVC/GetLocation" "$(loc_get $DRV "$DRIVER_A")" | json_field 'd["coordinates"]["latitude"]')"
if python3 -c "import sys; sys.exit(0 if abs(float('$LAT_NOW') - $LAT_A) < 1e-6 else 1)"; then
  printf '  ok    the denied move changed nothing\n'
else
  printf '  FAIL  driver A was moved by someone else (latitude is now %s)\n' "$LAT_NOW"
  FAILURES=$((FAILURES + 1))
fi

echo "==> [3/7] a rider writes and reads only their own position"
expect "rider 1 reports their own position"         OK               "$TOKEN_RIDER_1" UpdateLocation "$(loc_update $RDR "$RIDER_1" "$LAT_A" "$LNG_A")"
expect "rider 1 reads their own position"           OK               "$TOKEN_RIDER_1" GetLocation    "$(loc_get $RDR "$RIDER_1")"
expect "rider 2 moves rider 1"                      PermissionDenied "$TOKEN_RIDER_2" UpdateLocation "$(loc_update $RDR "$RIDER_1" 10.0 10.0)"
expect "rider 2 reads rider 1's position"           PermissionDenied "$TOKEN_RIDER_2" GetLocation    "$(loc_get $RDR "$RIDER_1")"

echo "==> [4/7] roles cannot be swapped or spoofed"
expect "rider 1 moves driver A (spoofing)"          PermissionDenied "$TOKEN_RIDER_1"  UpdateLocation "$(loc_update $DRV "$DRIVER_A" 10.0 10.0)"
expect "rider 1 reads driver A directly"            PermissionDenied "$TOKEN_RIDER_1"  GetLocation    "$(loc_get $DRV "$DRIVER_A")"
expect "driver A writes under the rider type"       PermissionDenied "$TOKEN_DRIVER_A" UpdateLocation "$(loc_update $RDR "$DRIVER_A" "$LAT_A" "$LNG_A")"
expect "rider 1 writes under the driver type"       PermissionDenied "$TOKEN_RIDER_1"  UpdateLocation "$(loc_update $DRV "$RIDER_1" "$LAT_A" "$LNG_A")"
expect "an unspecified entity type"                 PermissionDenied "$TOKEN_DRIVER_A" UpdateLocation "$(loc_update ENTITY_TYPE_UNSPECIFIED "$DRIVER_A" "$LAT_A" "$LNG_A")"
expect "an empty entity id"                         PermissionDenied "$TOKEN_DRIVER_A" UpdateLocation "$(loc_update $DRV "" "$LAT_A" "$LNG_A")"
expect "an empty entity id on read"                 PermissionDenied "$TOKEN_DRIVER_A" GetLocation    "$(loc_get $DRV "")"

echo "==> [5/7] FindNearby is internal only"
NEARBY="{\"entity_type\":\"$DRV\",\"coordinates\":{\"latitude\":$LAT_A,\"longitude\":$LNG_A},\"radius_meters\":5000,\"limit\":5}"
expect "driver A asks who is nearby"                PermissionDenied "$TOKEN_DRIVER_A" FindNearby "$NEARBY"
expect "rider 1 asks who is nearby"                 PermissionDenied "$TOKEN_RIDER_1"  FindNearby "$NEARBY"
expect "the internal token asks who is nearby"      OK               "$INTERNAL_TOKEN" FindNearby "$NEARBY"

echo "==> [6/7] zones: everyone may look, only the internal caller may change"
COORD="{\"coordinates\":{\"latitude\":$LAT_A,\"longitude\":$LNG_A}}"
expect "rider 1 checks the service zone"            OK               "$TOKEN_RIDER_1"  CheckServiceZone "$COORD"
expect "driver A lists zones"                       OK               "$TOKEN_DRIVER_A" ListZones        '{}'
expect_authorized "rider 1 reads a zone that does not exist" "$TOKEN_RIDER_1" GetZone "{\"zone_id\":\"$MISSING_ID\"}"
expect "rider 1 creates a zone"                     PermissionDenied "$TOKEN_RIDER_1"  CreateZone   '{"city":"x","name":"x","boundary":[{"latitude":1,"longitude":1},{"latitude":1,"longitude":2},{"latitude":2,"longitude":2}]}'
expect "driver A edits a zone"                      PermissionDenied "$TOKEN_DRIVER_A" UpdateZone   "{\"zone_id\":\"$MISSING_ID\",\"name\":\"x\"}"
expect "driver A switches a zone off"               PermissionDenied "$TOKEN_DRIVER_A" SetZoneActive "{\"zone_id\":\"$MISSING_ID\",\"active\":false}"

echo "==> [7/7] bad credentials and the internal path"
expect "an expired token"                           Unauthenticated  "$TOKEN_EXPIRED" GetLocation "$(loc_get $RDR "$RIDER_1")"
expect "a malformed token"                          Unauthenticated  "not-a-token"    GetLocation "$(loc_get $RDR "$RIDER_1")"
expect "the internal token moves any driver"        OK               "$INTERNAL_TOKEN" UpdateLocation "$(loc_update $DRV "$DRIVER_B" "$LAT_A" "$LNG_A")"
expect "the internal token reads any driver"        OK               "$INTERNAL_TOKEN" GetLocation    "$(loc_get $DRV "$DRIVER_B")"
expect "the internal token reads any rider"         OK               "$INTERNAL_TOKEN" GetLocation    "$(loc_get $RDR "$RIDER_1")"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: users write and read only their own position, FindNearby is internal, and the internal path is intact"
  echo "Now re-run scripts/e2e/test-trip-authz.sh: dispatch finds the driver through FindNearby with the internal token."
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
