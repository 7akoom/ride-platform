#!/usr/bin/env bash
# End-to-end pricing authorization test with REAL user tokens.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-pricing-authz.sh
#
# Tokens are minted for local development only (scripts/tools/devtoken) and
# are never printed.
#
# What it proves:
#   1. a rider may ask for a fare estimate only for their own rider profile
#   2. another rider, a driver, a missing rider id and an empty one are all
#      denied the same way
#   3. every other pricing RPC (CalculateFare, CreateCoupon, GetCoupon) is
#      closed to end users
#   4. the internal token still reaches everything (trip, wallet and the
#      auto-fare consumer depend on it)
#
# The estimate itself needs OSRM and the weather feed; a failure there is not an
# authorization failure, so the "own estimate" check only requires that the
# caller got past authorization (and prints what came back).
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"

DRIVER_A="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
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
PRICING_PORT="$(compose_port pricing-service)"
[ -n "$RIDER_PORT" ] && [ -n "$DRIVER_PORT" ] && [ -n "$PRICING_PORT" ] \
  || { echo "FAIL: could not read the rider/driver/pricing GRPC_ADDRESS from compose.yaml" >&2; exit 1; }

RIDER_ADDR="localhost:$RIDER_PORT"
DRIVER_ADDR="localhost:$DRIVER_PORT"
PRICING_ADDR="localhost:$PRICING_PORT"
RIDER_SVC="ride.rider.v1.RiderService"
DRIVER_SVC="ride.driver.v1.DriverService"
PRICING_SVC="ride.pricing.v1.PricingService"

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
  output="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$3" "$PRICING_ADDR" "$PRICING_SVC/$2" 2>&1 || true)"

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

# Only that the authorization layer let the caller through; what the business
# logic answered (OSRM down, no such coupon...) is not this test's concern.
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

estimate() { # <rider id>
  echo "{\"rider_id\":\"$1\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\"}"
}

echo "==> [1/5] preparing: protoset, two riders, a driver, tokens"
buf build -o "$PROTOSET"

RIDER_1="$(ensure_rider "$RIDER_IDENTITY_1" "Authz Test Rider")"
RIDER_2="$(ensure_rider "$RIDER_IDENTITY_2" "Authz Test Rider Two")"

IDENTITY_DRIVER_A="$(internal_call "$DRIVER_ADDR" "$DRIVER_SVC/GetDriver" "{\"driver_id\":\"$DRIVER_A\"}" | json_field 'd["driver"]["identityId"]')"
[ -n "$IDENTITY_DRIVER_A" ] || { echo "FAIL: the test driver has no identity" >&2; exit 1; }

TOKEN_RIDER_1="$(mint "$RIDER_IDENTITY_1")"
TOKEN_RIDER_2="$(mint "$RIDER_IDENTITY_2")"
TOKEN_DRIVER_A="$(mint "$IDENTITY_DRIVER_A")"
TOKEN_EXPIRED="$(mint "$RIDER_IDENTITY_1" -ttl=-1h)"

echo "==> [2/5] a rider estimates only for themselves"
expect_authorized "rider 1 estimates for rider 1"   "$TOKEN_RIDER_1" EstimateFare "$(estimate "$RIDER_1")"
expect "rider 2 estimates for rider 1"              PermissionDenied "$TOKEN_RIDER_2" EstimateFare "$(estimate "$RIDER_1")"
expect "rider 1 estimates for rider 2"              PermissionDenied "$TOKEN_RIDER_1" EstimateFare "$(estimate "$RIDER_2")"
expect "driver A estimates for rider 1"             PermissionDenied "$TOKEN_DRIVER_A" EstimateFare "$(estimate "$RIDER_1")"
expect "driver A estimates with their own driver id" PermissionDenied "$TOKEN_DRIVER_A" EstimateFare "$(estimate "$DRIVER_A")"
expect "rider 1 estimates for a rider that does not exist" PermissionDenied "$TOKEN_RIDER_1" EstimateFare "$(estimate "$MISSING_ID")"
expect "rider 1 estimates with an empty rider id"   PermissionDenied "$TOKEN_RIDER_1" EstimateFare "$(estimate "")"

echo "==> [3/5] every other pricing RPC is closed to end users"
COUPON='{"code":"AUTHZ-TEST","discount_type":"DISCOUNT_TYPE_PERCENTAGE","discount_value":"10"}'
for who in "rider 1:$TOKEN_RIDER_1" "driver A:$TOKEN_DRIVER_A"; do
  name="${who%%:*}"; token="${who#*:}"
  expect "$name calculates a fare"                  PermissionDenied "$token" CalculateFare "{\"trip_id\":\"$MISSING_ID\",\"rider_id\":\"$RIDER_1\"}"
  expect "$name creates a coupon"                   PermissionDenied "$token" CreateCoupon  "$COUPON"
  expect "$name reads a coupon"                     PermissionDenied "$token" GetCoupon     '{"code":"AUTHZ-TEST"}'
done

echo "==> [4/5] bad credentials"
expect "an expired token"                           Unauthenticated  "$TOKEN_EXPIRED" EstimateFare "$(estimate "$RIDER_1")"
expect "a malformed token"                          Unauthenticated  "not-a-token"    EstimateFare "$(estimate "$RIDER_1")"

echo "==> [5/5] the internal path is intact"
expect_authorized "the internal token estimates for rider 2" "$INTERNAL_TOKEN" EstimateFare "$(estimate "$RIDER_2")"
expect_authorized "the internal token reads a coupon"        "$INTERNAL_TOKEN" GetCoupon     '{"code":"AUTHZ-DOES-NOT-EXIST"}'

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: a rider estimates only for themselves, the other pricing RPCs are internal, and the internal path is intact"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
