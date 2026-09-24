#!/usr/bin/env bash
# End-to-end test of the client HTTP API THROUGH THE GATEWAY, with REAL user tokens.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-gateway-http.sh
#
# Everything the mobile apps will do goes through http://localhost:8080, so this
# is the test that proves the user's token really reaches the backends and that
# their ownership checks answer with the right HTTP status:
#   401 no/invalid credentials, 403 not yours, 404 not found, 409 already exists.
#
# What it proves:
#   1. rider profile, driver profile, driver availability, location reports,
#      fare estimates, push devices and the notification inbox all work for their
#      owner over HTTP, and answer 403 for everyone else
#   2. RPCs meant only for other services (Send, FindNearby, CalculateFare,
#      ClaimQuote...) have no route at all (coupons are staff routes, under
#      /v1/admin)
#   3. the internal service token is refused at the gateway, and cannot be
#      smuggled in through a Grpc-Metadata-* header
#   4. CORS preflight still works
#
# Tokens are minted for local development only (scripts/tools/devtoken) and are
# never printed. It registers one throw-away push token and removes it again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"

DRIVER_A="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
DRIVER_B="4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
RIDER_IDENTITY_1="a0000000-0000-4000-8000-0000000000a1"
RIDER_IDENTITY_2="a0000000-0000-4000-8000-0000000000a2"
MISSING_ID="00000000-0000-4000-8000-00000000dead"
DEVICE_TOKEN="authz-http:device-1"

compose_port() { # <compose service name>
  awk -v svc="$1" '
    $0 ~ ("^  " svc ":$") {inside=1; next}
    inside && /^  [A-Za-z0-9_-]+:$/ {exit}
    inside && /GRPC_ADDRESS/ {gsub(/[^0-9]/, ""); print; exit}
  ' infrastructure/compose/compose.yaml
}

RIDER_ADDR="localhost:$(compose_port rider-service)"
DRIVER_ADDR="localhost:$(compose_port driver-service)"
NOTIFICATION_ADDR="localhost:$(compose_port notification-service)"
RIDER_SVC="ride.rider.v1.RiderService"
DRIVER_SVC="ride.driver.v1.DriverService"
NOTIFICATION_SVC="ride.notification.v1.NotificationService"

FAILURES=0
BODY_FILE="$(mktemp)"
trap 'rm -f "$BODY_FILE"' EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1" "${@:2}"; }

internal_call() { # <address> <service/method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$3" "$1" "$2"
}

json_field() { # <python expression over d> (reads stdin)
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

body_field() { # <python expression over d>: from the last HTTP response
  json_field "$1" < "$BODY_FILE"
}

# http <method> <path> <token> [json body] -> the HTTP status; the body goes to $BODY_FILE
http() {
  local method="$1" path="$2" token="$3" body="${4:-}"
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$method" "$BASE$path")

  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")

  curl "${args[@]}"
}

expect() { # <label> <expected status> <method> <path> <token> [json body]
  local actual
  actual="$(http "$3" "$4" "$5" "${6:-}")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s: %s\n' "$1" "$2" "$actual" "$(head -c 200 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

# Only that the request got past authentication and authorization; what the
# business logic answered (a duplicate, OSRM down...) is not this test's concern.
expect_authorized() { # <label> <method> <path> <token> [json body]
  local actual
  actual="$(http "$2" "$3" "$4" "${5:-}")"

  if [ "$actual" != 401 ] && [ "$actual" != 403 ] && [ "$actual" != 404 ] && [ "$actual" != 405 ]; then
    printf '  ok    %s -> authorized (%s)\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected to be authorized, got %s: %s\n' "$1" "$actual" "$(head -c 200 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

# An RPC without a route answers 404 (no such path) or, when the path exists for
# another method, 405 or 501 depending on the grpc-gateway version.
expect_no_route() { # <label> <method> <path> <token> [json body]
  local actual
  actual="$(http "$2" "$3" "$4" "${5:-}")"

  case "$actual" in
    404 | 405 | 501) printf '  ok    %s -> no route (%s)\n' "$1" "$actual" ;;
    *)
      printf '  FAIL  %s -> expected no route (404/405/501), got %s: %s\n' "$1" "$actual" "$(head -c 200 "$BODY_FILE")"
      FAILURES=$((FAILURES + 1))
      ;;
  esac
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

echo "==> [1/8] preparing: the gateway, protoset, riders, drivers, tokens"
curl -sS -o /dev/null --max-time 5 "$BASE/v1/zones" -H "Authorization: Bearer x" \
  || { echo "FAIL: the gateway is not answering on $BASE" >&2; exit 1; }

buf build -o "$PROTOSET"

RIDER_1="$(ensure_rider "$RIDER_IDENTITY_1" "Authz Test Rider")"
RIDER_2="$(ensure_rider "$RIDER_IDENTITY_2" "Authz Test Rider Two")"
RIDER_1_NAME="$(internal_call "$RIDER_ADDR" "$RIDER_SVC/GetRider" "{\"rider_id\":\"$RIDER_1\"}" | json_field 'd["rider"]["displayName"]')"

IDENTITY_DRIVER_A="$(driver_identity "$DRIVER_A")"
IDENTITY_DRIVER_B="$(driver_identity "$DRIVER_B")"
[ -n "$IDENTITY_DRIVER_A" ] && [ -n "$IDENTITY_DRIVER_B" ] || { echo "FAIL: the test drivers have no identity" >&2; exit 1; }

T1="$(mint "$RIDER_IDENTITY_1")"
T2="$(mint "$RIDER_IDENTITY_2")"
TA="$(mint "$IDENTITY_DRIVER_A")"
TB="$(mint "$IDENTITY_DRIVER_B")"
TEXPIRED="$(mint "$RIDER_IDENTITY_1" -ttl=-1h)"

internal_call "$NOTIFICATION_ADDR" "$NOTIFICATION_SVC/UnregisterDevice" "{\"device_token\":\"$DEVICE_TOKEN\"}" > /dev/null 2>&1 || true

echo "==> [2/8] the rider's profile"
expect "rider 1 finds their profile by identity"        200 GET   "/v1/identities/$RIDER_IDENTITY_1/rider" "$T1"
if [ "$(body_field 'd["rider"]["id"]')" = "$RIDER_1" ]; then pass "the answer is rider 1's profile"; else fail "the answer is not rider 1's profile"; fi
expect "rider 2 looks up rider 1 by identity"           403 GET   "/v1/identities/$RIDER_IDENTITY_1/rider" "$T2"
expect "rider 1 reads their profile"                    200 GET   "/v1/riders/$RIDER_1" "$T1"
expect "rider 2 reads rider 1's profile"                403 GET   "/v1/riders/$RIDER_1" "$T2"
expect "driver A reads rider 1's profile"               403 GET   "/v1/riders/$RIDER_1" "$TA"
expect "nobody reads rider 1's profile"                 401 GET   "/v1/riders/$RIDER_1" ""
expect "rider 1 renames themselves (same name)"         200 PATCH "/v1/riders/$RIDER_1" "$T1" "{\"displayName\":\"$RIDER_1_NAME\"}"
expect "rider 2 renames rider 1"                        403 PATCH "/v1/riders/$RIDER_1" "$T2" '{"displayName":"hijacked"}'
expect "rider 1 creates their profile again"            409 POST  "/v1/riders" "$T1" "{\"identityId\":\"$RIDER_IDENTITY_1\",\"displayName\":\"$RIDER_1_NAME\"}"
expect "rider 2 creates a profile for rider 1's identity" 403 POST "/v1/riders" "$T2" "{\"identityId\":\"$RIDER_IDENTITY_1\",\"displayName\":\"hijacked\"}"

echo "==> [3/8] the driver's profile and availability"
expect "driver A finds their profile by identity"       200 GET   "/v1/identities/$IDENTITY_DRIVER_A/driver" "$TA"
expect "driver B looks up driver A by identity"         403 GET   "/v1/identities/$IDENTITY_DRIVER_A/driver" "$TB"
expect "driver A reads their profile"                   200 GET   "/v1/drivers/$DRIVER_A" "$TA"
expect "driver B reads driver A's profile"              403 GET   "/v1/drivers/$DRIVER_A" "$TB"
expect "rider 1 reads driver A's profile"               403 GET   "/v1/drivers/$DRIVER_A" "$T1"
expect "driver A goes available"                        200 PUT   "/v1/drivers/$DRIVER_A/availability" "$TA" '{"availabilityStatus":"AVAILABILITY_STATUS_AVAILABLE"}'
expect "driver B takes driver A offline"                403 PUT   "/v1/drivers/$DRIVER_A/availability" "$TB" '{"availabilityStatus":"AVAILABILITY_STATUS_OFFLINE"}'
expect "rider 1 takes driver A offline"                 403 PUT   "/v1/drivers/$DRIVER_A/availability" "$T1" '{"availabilityStatus":"AVAILABILITY_STATUS_OFFLINE"}'

echo "==> [4/8] positions and service zones"
POSITION='{"entityType":"ENTITY_TYPE_DRIVER","coordinates":{"latitude":36.1905,"longitude":44.0105}}'
expect "driver A reports their position"                200 PUT   "/v1/locations/$DRIVER_A" "$TA" "$POSITION"
expect "driver B moves driver A"                        403 PUT   "/v1/locations/$DRIVER_A" "$TB" "$POSITION"
expect "rider 1 moves driver A"                         403 PUT   "/v1/locations/$DRIVER_A" "$T1" "$POSITION"
expect "driver A reads their position"                  200 GET   "/v1/locations/$DRIVER_A?entity_type=ENTITY_TYPE_DRIVER" "$TA"
expect "rider 1 reads driver A's position directly"     403 GET   "/v1/locations/$DRIVER_A?entity_type=ENTITY_TYPE_DRIVER" "$T1"
expect "rider 1 checks a point against the service zones" 200 GET "/v1/zones:check?coordinates.latitude=36.19&coordinates.longitude=44.01" "$T1"
expect "driver A lists the service zones"               200 GET   "/v1/zones" "$TA"
expect_no_route "creating a zone has no route" POST "/v1/zones" "$T1" '{"city":"x","name":"x"}'

echo "==> [5/8] fare estimates"
ESTIMATE() { echo "{\"riderId\":\"$1\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicleClass\":\"economy\"}"; }
expect_authorized "rider 1 estimates a fare for themselves"  POST "/v1/fare-estimates" "$T1" "$(ESTIMATE "$RIDER_1")"
expect "rider 2 estimates a fare for rider 1"           403 POST  "/v1/fare-estimates" "$T2" "$(ESTIMATE "$RIDER_1")"
expect "driver A estimates a fare for rider 1"          403 POST  "/v1/fare-estimates" "$TA" "$(ESTIMATE "$RIDER_1")"

echo "==> [6/8] push devices and the notification inbox"
DEVICE="{\"recipientType\":\"RECIPIENT_TYPE_RIDER\",\"recipientId\":\"$RIDER_1\",\"deviceToken\":\"$DEVICE_TOKEN\",\"platform\":\"PLATFORM_ANDROID\",\"locale\":\"en\"}"
expect "rider 2 registers a device for rider 1"         403 POST  "/v1/devices" "$T2" "$DEVICE"
expect "rider 1 registers their device (token with ':')" 200 POST "/v1/devices" "$T1" "$DEVICE"
expect "rider 1 lists their notifications"             200 GET   "/v1/notifications?recipient_type=RECIPIENT_TYPE_RIDER&recipient_id=$RIDER_1&limit=5" "$T1"
expect "rider 2 lists rider 1's notifications"         403 GET   "/v1/notifications?recipient_type=RECIPIENT_TYPE_RIDER&recipient_id=$RIDER_1&limit=5" "$T2"
expect "rider 1 lists them as a driver"                403 GET   "/v1/notifications?recipient_type=RECIPIENT_TYPE_DRIVER&recipient_id=$RIDER_1" "$T1"
expect_authorized "rider 1 marks their notifications read"   POST "/v1/notifications:read" "$T1" "{\"recipientType\":\"RECIPIENT_TYPE_RIDER\",\"recipientId\":\"$RIDER_1\",\"notificationIds\":[\"$MISSING_ID\"]}"
expect "rider 2 marks rider 1's notifications read"    403 POST  "/v1/notifications:read" "$T2" "{\"recipientType\":\"RECIPIENT_TYPE_RIDER\",\"recipientId\":\"$RIDER_1\"}"
expect "rider 2 unregisters rider 1's device"          403 POST  "/v1/devices:unregister" "$T2" "{\"deviceToken\":\"$DEVICE_TOKEN\"}"
expect "rider 1 unregisters their device"              200 POST  "/v1/devices:unregister" "$T1" "{\"deviceToken\":\"$DEVICE_TOKEN\"}"
if [ "$(body_field 'd["removed"]')" = "True" ]; then pass "the device was still there: the refused attempt removed nothing"; else fail "the device was already gone after a refused attempt"; fi

echo "==> [7/8] the door is shut for everything else"
expect_no_route "Send has no route" POST "/v1/notifications" "$T1" '{"recipientType":"RECIPIENT_TYPE_RIDER","recipientId":"x","eventKey":"x"}'
expect_no_route "FindNearby has no route" POST "/v1/locations:nearby" "$T1" '{}'
expect_no_route "coupons have no rider route" GET "/v1/coupons/ANYTHING" "$T1"
expect_no_route "CalculateFare has no route" POST "/v1/fares:calculate" "$T1" '{}'
expect "an expired token"                               401 GET   "/v1/riders/$RIDER_1" "$TEXPIRED"
expect "a token that is not a JWT"                      401 GET   "/v1/riders/$RIDER_1" "not-a-token"

STATUS="$(http GET "/v1/riders/$RIDER_1" "$INTERNAL_TOKEN")"
if [ "$STATUS" = 401 ]; then pass "the internal service token is refused at the gateway (401)"; else fail "the internal service token was not refused at the gateway (got $STATUS)"; fi

STATUS="$(curl -sS -o "$BODY_FILE" -w '%{http_code}' "$BASE/v1/riders/$RIDER_1" \
  -H "Authorization: Bearer $T2" -H "Grpc-Metadata-Authorization: Bearer $INTERNAL_TOKEN")"
if [ "$STATUS" = 403 ]; then pass "the internal token cannot be smuggled in through Grpc-Metadata-Authorization"; else fail "a Grpc-Metadata-Authorization header changed the outcome (got $STATUS instead of 403)"; fi

echo "==> [8/8] CORS preflight"
STATUS="$(curl -sS -o /dev/null -D "$BODY_FILE" -w '%{http_code}' -X OPTIONS "$BASE/v1/riders/$RIDER_1" \
  -H "Origin: http://localhost:3000" -H "Access-Control-Request-Method: GET" -H "Access-Control-Request-Headers: Authorization")"
if [ "$STATUS" = 204 ] && grep -qi '^access-control-allow-origin: http://localhost:3000' "$BODY_FILE"; then
  pass "a preflight from the admin origin is answered"
else
  fail "the CORS preflight failed (status $STATUS)"
fi

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: the client HTTP API works through the gateway for each owner, refuses everyone else, and keeps the internal token out"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
