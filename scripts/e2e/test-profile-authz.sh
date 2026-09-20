#!/usr/bin/env bash
# End-to-end profile authorization test (rider-service and driver-service)
# with REAL user tokens.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-profile-authz.sh
#
# Tokens are minted for local development only (scripts/tools/devtoken) and
# are never printed.
#
# What it proves:
#   1. a user can read and update only their own rider or driver profile
#   2. a user can create or look up a profile only for their own identity
#   3. someone else's profile, a missing profile and a malformed id are all
#      denied the same way
#   4. a driver's token gets nothing from rider-service, and a rider's token
#      gets nothing from driver-service
#   5. a denied write changes nothing
#   6. the internal token still reaches everything (dispatch, trip, wallet and
#      notification depend on it)
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
[ -n "$RIDER_PORT" ] && [ -n "$DRIVER_PORT" ] || { echo "FAIL: could not read the rider/driver GRPC_ADDRESS from compose.yaml" >&2; exit 1; }

RIDER_ADDR="localhost:$RIDER_PORT"
DRIVER_ADDR="localhost:$DRIVER_PORT"
RIDER_SVC="ride.rider.v1.RiderService"
DRIVER_SVC="ride.driver.v1.DriverService"

FAILURES=0

mint() { go run scripts/tools/devtoken/main.go -sub "$1" "${@:2}"; }

internal_call() { # <address> <service/method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$3" "$1" "$2"
}

json_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

code_of() { # <address> <service> <token> <method> <json>  -> OK or the gRPC code name
  local output
  output="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $3" -d "$5" "$1" "$2/$4" 2>&1 || true)"

  if grep -q '^ERROR:' <<<"$output"; then
    awk '/Code:/ {print $2; exit}' <<<"$output"
  else
    echo OK
  fi
}

expect() { # <label> <expected> <address> <service> <token> <method> <json>
  local actual
  actual="$(code_of "$3" "$4" "$5" "$6" "$7")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "${actual:-no response}"
    FAILURES=$((FAILURES + 1))
  fi
}

# For calls whose business result does not matter here (a duplicate create, a
# validation error): only that the authorization layer let the caller through.
expect_authorized() { # <label> <address> <service> <token> <method> <json>
  local actual
  actual="$(code_of "$2" "$3" "$4" "$5" "$6")"

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

rider_name() { # <rider id>
  internal_call "$RIDER_ADDR" "$RIDER_SVC/GetRider" "{\"rider_id\":\"$1\"}" | json_field 'd["rider"]["displayName"]'
}

driver_field() { # <driver id> <python expression over d["driver"]>
  internal_call "$DRIVER_ADDR" "$DRIVER_SVC/GetDriver" "{\"driver_id\":\"$1\"}" | json_field "$2"
}

echo "==> [1/6] preparing: protoset, two riders, two drivers, tokens"
buf build -o "$PROTOSET"

RIDER_1="$(ensure_rider "$RIDER_IDENTITY_1" "Authz Test Rider")"
RIDER_2="$(ensure_rider "$RIDER_IDENTITY_2" "Authz Test Rider Two")"

IDENTITY_DRIVER_A="$(driver_field "$DRIVER_A" 'd["driver"]["identityId"]')"
IDENTITY_DRIVER_B="$(driver_field "$DRIVER_B" 'd["driver"]["identityId"]')"
[ -n "$IDENTITY_DRIVER_A" ] && [ -n "$IDENTITY_DRIVER_B" ] || { echo "FAIL: the test drivers have no identity" >&2; exit 1; }

TOKEN_RIDER_1="$(mint "$RIDER_IDENTITY_1")"
TOKEN_RIDER_2="$(mint "$RIDER_IDENTITY_2")"
TOKEN_DRIVER_A="$(mint "$IDENTITY_DRIVER_A")"
TOKEN_DRIVER_B="$(mint "$IDENTITY_DRIVER_B")"
TOKEN_EXPIRED="$(mint "$RIDER_IDENTITY_1" -ttl=-1h)"

RIDER_1_NAME="$(rider_name "$RIDER_1")"

# Driver A must stay available for the other end-to-end tests.
internal_call "$DRIVER_ADDR" "$DRIVER_SVC/UpdateAvailability" \
  "{\"driver_id\":\"$DRIVER_A\",\"availability_status\":\"AVAILABILITY_STATUS_AVAILABLE\"}" > /dev/null

echo "==> [2/6] rider-service: a rider reaches only their own profile"
r() { echo "$RIDER_ADDR $RIDER_SVC"; }
# shellcheck disable=SC2046
expect "rider 1 reads their own profile by identity"      OK               $(r) "$TOKEN_RIDER_1" GetRiderByIdentity "{\"identity_id\":\"$RIDER_IDENTITY_1\"}"
expect "rider 1 reads their own profile by id"            OK               $(r) "$TOKEN_RIDER_1" GetRider           "{\"rider_id\":\"$RIDER_1\"}"
expect "rider 1 renames themselves (same name)"           OK               $(r) "$TOKEN_RIDER_1" UpdateRiderProfile "{\"rider_id\":\"$RIDER_1\",\"display_name\":\"$RIDER_1_NAME\"}"
expect_authorized "rider 1 creates their own profile again" $(r) "$TOKEN_RIDER_1" CreateRider "{\"identity_id\":\"$RIDER_IDENTITY_1\",\"display_name\":\"$RIDER_1_NAME\"}"

expect "rider 2 looks up rider 1 by identity"             PermissionDenied $(r) "$TOKEN_RIDER_2" GetRiderByIdentity "{\"identity_id\":\"$RIDER_IDENTITY_1\"}"
expect "rider 2 reads rider 1 by id"                      PermissionDenied $(r) "$TOKEN_RIDER_2" GetRider           "{\"rider_id\":\"$RIDER_1\"}"
expect "rider 2 renames rider 1"                          PermissionDenied $(r) "$TOKEN_RIDER_2" UpdateRiderProfile "{\"rider_id\":\"$RIDER_1\",\"display_name\":\"hijacked\"}"
expect "rider 2 creates a profile for rider 1's identity" PermissionDenied $(r) "$TOKEN_RIDER_2" CreateRider         "{\"identity_id\":\"$RIDER_IDENTITY_1\",\"display_name\":\"hijacked\"}"
expect "rider 1 reads a rider that does not exist"        PermissionDenied $(r) "$TOKEN_RIDER_1" GetRider           "{\"rider_id\":\"$MISSING_ID\"}"
expect "rider 1 reads a malformed rider id"               PermissionDenied $(r) "$TOKEN_RIDER_1" GetRider           '{"rider_id":"not-a-uuid"}'
expect "rider 1 reads an empty rider id"                  PermissionDenied $(r) "$TOKEN_RIDER_1" GetRider           '{"rider_id":""}'

if [ "$(rider_name "$RIDER_1")" = "$RIDER_1_NAME" ]; then
  printf '  ok    the denied rename changed nothing\n'
else
  printf '  FAIL  rider 1 was renamed by someone else\n'
  FAILURES=$((FAILURES + 1))
fi

expect "driver A reads rider 1 by id"                     PermissionDenied $(r) "$TOKEN_DRIVER_A" GetRider           "{\"rider_id\":\"$RIDER_1\"}"
expect "driver A looks up rider 1 by identity"            PermissionDenied $(r) "$TOKEN_DRIVER_A" GetRiderByIdentity "{\"identity_id\":\"$RIDER_IDENTITY_1\"}"

echo "==> [3/6] driver-service: a driver reaches only their own profile"
d() { echo "$DRIVER_ADDR $DRIVER_SVC"; }

PROFILE_A="$(driver_field "$DRIVER_A" 'json.dumps({"driver_id": d["driver"]["id"], "display_name": d["driver"]["displayName"], "vehicle": d["driver"]["vehicle"]})')"

# shellcheck disable=SC2046
expect "driver A reads their own profile by identity"     OK               $(d) "$TOKEN_DRIVER_A" GetDriverByIdentity "{\"identity_id\":\"$IDENTITY_DRIVER_A\"}"
expect "driver A reads their own profile by id"           OK               $(d) "$TOKEN_DRIVER_A" GetDriver           "{\"driver_id\":\"$DRIVER_A\"}"
expect "driver A goes available"                          OK               $(d) "$TOKEN_DRIVER_A" UpdateAvailability  "{\"driver_id\":\"$DRIVER_A\",\"availability_status\":\"AVAILABILITY_STATUS_AVAILABLE\"}"
expect_authorized "driver A re-saves their own profile"   $(d) "$TOKEN_DRIVER_A" UpdateDriverProfile "$PROFILE_A"
expect_authorized "driver A creates their own profile again" $(d) "$TOKEN_DRIVER_A" CreateDriver "{\"identity_id\":\"$IDENTITY_DRIVER_A\",\"display_name\":\"x\"}"

expect "driver B looks up driver A by identity"           PermissionDenied $(d) "$TOKEN_DRIVER_B" GetDriverByIdentity "{\"identity_id\":\"$IDENTITY_DRIVER_A\"}"
expect "driver B reads driver A by id"                    PermissionDenied $(d) "$TOKEN_DRIVER_B" GetDriver           "{\"driver_id\":\"$DRIVER_A\"}"
expect "driver B takes driver A offline"                  PermissionDenied $(d) "$TOKEN_DRIVER_B" UpdateAvailability  "{\"driver_id\":\"$DRIVER_A\",\"availability_status\":\"AVAILABILITY_STATUS_OFFLINE\"}"
expect "driver B edits driver A's profile"                PermissionDenied $(d) "$TOKEN_DRIVER_B" UpdateDriverProfile "$PROFILE_A"
expect "driver B creates a profile for driver A's identity" PermissionDenied $(d) "$TOKEN_DRIVER_B" CreateDriver       "{\"identity_id\":\"$IDENTITY_DRIVER_A\",\"display_name\":\"hijacked\"}"
expect "driver A reads a driver that does not exist"      PermissionDenied $(d) "$TOKEN_DRIVER_A" GetDriver           "{\"driver_id\":\"$MISSING_ID\"}"
expect "driver A changes availability of a missing driver" PermissionDenied $(d) "$TOKEN_DRIVER_A" UpdateAvailability "{\"driver_id\":\"$MISSING_ID\",\"availability_status\":\"AVAILABILITY_STATUS_OFFLINE\"}"
expect "driver A reads a malformed driver id"             PermissionDenied $(d) "$TOKEN_DRIVER_A" GetDriver           '{"driver_id":"not-a-uuid"}'

if [ "$(driver_field "$DRIVER_A" 'd["driver"]["availabilityStatus"]')" = "AVAILABILITY_STATUS_AVAILABLE" ]; then
  printf '  ok    the denied availability change changed nothing\n'
else
  printf '  FAIL  driver A was taken offline by someone else\n'
  FAILURES=$((FAILURES + 1))
fi

expect "rider 1 reads driver A by id"                     PermissionDenied $(d) "$TOKEN_RIDER_1" GetDriver           "{\"driver_id\":\"$DRIVER_A\"}"
expect "rider 1 looks up driver A by identity"            PermissionDenied $(d) "$TOKEN_RIDER_1" GetDriverByIdentity "{\"identity_id\":\"$IDENTITY_DRIVER_A\"}"
expect "rider 1 takes driver A offline"                   PermissionDenied $(d) "$TOKEN_RIDER_1" UpdateAvailability  "{\"driver_id\":\"$DRIVER_A\",\"availability_status\":\"AVAILABILITY_STATUS_OFFLINE\"}"

echo "==> [4/6] bad credentials"
# shellcheck disable=SC2046
expect "an expired token on rider-service"                Unauthenticated  $(r) "$TOKEN_EXPIRED" GetRider "{\"rider_id\":\"$RIDER_1\"}"
# shellcheck disable=SC2046
expect "a malformed token on rider-service"               Unauthenticated  $(r) "not-a-token"    GetRider "{\"rider_id\":\"$RIDER_1\"}"
# shellcheck disable=SC2046
expect "an expired token on driver-service"               Unauthenticated  $(d) "$TOKEN_EXPIRED" GetDriver "{\"driver_id\":\"$DRIVER_A\"}"
# shellcheck disable=SC2046
expect "a malformed token on driver-service"              Unauthenticated  $(d) "not-a-token"    GetDriver "{\"driver_id\":\"$DRIVER_A\"}"

echo "==> [5/6] the internal path is intact"
# shellcheck disable=SC2046
expect "the internal token reads any rider"               OK               $(r) "$INTERNAL_TOKEN" GetRider           "{\"rider_id\":\"$RIDER_2\"}"
# shellcheck disable=SC2046
expect "the internal token looks up a rider by identity"  OK               $(r) "$INTERNAL_TOKEN" GetRiderByIdentity "{\"identity_id\":\"$RIDER_IDENTITY_2\"}"
# shellcheck disable=SC2046
expect "the internal token reads any driver"              OK               $(d) "$INTERNAL_TOKEN" GetDriver           "{\"driver_id\":\"$DRIVER_B\"}"
# shellcheck disable=SC2046
expect "the internal token looks up a driver by identity" OK               $(d) "$INTERNAL_TOKEN" GetDriverByIdentity "{\"identity_id\":\"$IDENTITY_DRIVER_B\"}"

echo "==> [6/6] result"
echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: users reach only their own rider or driver profile, denied writes change nothing, and the internal path is intact"
  echo "Now re-run scripts/e2e/test-trip-authz.sh: it drives dispatch, which reads drivers with the internal token."
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
