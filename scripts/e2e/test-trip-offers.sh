#!/usr/bin/env bash
# End-to-end test of DRIVER OFFERS in trip-service: a trip put to one driver at a
# time, who accepts or rejects it. Run from the ride-platform repo root:
#   bash scripts/e2e/test-trip-offers.sh
#
# Needs migration 00009 applied to the trip database. About 20 seconds. Tokens are
# minted for local development only (scripts/tools/devtoken) and never printed.
#
# Dispatch does not use offers yet, so this test plays dispatch itself: it calls
# OfferTrip with the internal service token. The drivers go OFFLINE meanwhile, so
# the automatic dispatch of today cannot take the trips this test creates, and are
# put back as they were at the end.
#
# What it proves:
#   1. only dispatch can offer a trip (users get PermissionDenied, and there is no
#      HTTP route for it)
#   2. the driver sees the offer, and what they see does not identify the rider;
#      nobody else can see it
#   3. the rules: one live offer per trip, one per driver, never twice to the same
#      driver, none for a driver who is on a trip
#   4. a driver answers only as themselves; reject passes the trip on; accept makes
#      them the driver of the trip
#   5. an offer expires on its own, and a cancelled trip withdraws its offer at once
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"

DRIVER_A="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
DRIVER_B="4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
RIDER_IDENTITY_1="a0000000-0000-4000-8000-0000000000a1"
RIDER_IDENTITY_2="a0000000-0000-4000-8000-0000000000a2"

compose_port() { # <compose service name>
  awk -v svc="$1" '
    $0 ~ ("^  " svc ":$") {inside=1; next}
    inside && /^  [A-Za-z0-9_-]+:$/ {exit}
    inside && /GRPC_ADDRESS/ {gsub(/[^0-9]/, ""); print; exit}
  ' infrastructure/compose/compose.yaml
}

RIDER_ADDR="localhost:$(compose_port rider-service)"
DRIVER_ADDR="localhost:$(compose_port driver-service)"
TRIP_ADDR="localhost:$(compose_port trip-service)"
RIDER_SVC="ride.rider.v1.RiderService"
DRIVER_SVC="ride.driver.v1.DriverService"
TRIP_SVC="ride.trip.v1.TripService"

FAILURES=0
BODY_FILE="$(mktemp)"
trap 'rm -f "$BODY_FILE"' EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1" "${@:2}"; }

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

internal_call() { # <address> <service/method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$3" "$1" "$2"
}

json_field() { # <python expression over d> (reads stdin)
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

body_field() { json_field "$1" < "$BODY_FILE"; }

pass() { printf '  ok    %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

# code_of <token> <service/method> <json> -> OK or the gRPC code name, calling trip-service directly
code_of() {
  local output
  output="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$3" "$TRIP_ADDR" "$2" 2>&1 || true)"

  if grep -q '^ERROR:' <<<"$output"; then
    awk '/Code:/ {print $2; exit}' <<<"$output"
  else
    echo OK
  fi
}

offer() { # <trip> <driver> <ttl seconds> -> the gRPC code of the internal OfferTrip
  code_of "$INTERNAL_TOKEN" "$TRIP_SVC/OfferTrip" "{\"trip_id\":\"$1\",\"driver_id\":\"$2\",\"ttl_seconds\":${3:-15}}"
}

expect_offer() { # <label> <expected gRPC code> <trip> <driver> [ttl]
  local actual
  actual="$(offer "$3" "$4" "${5:-15}")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "${actual:-no response}"
    FAILURES=$((FAILURES + 1))
  fi
}

http() { # <method> <path> <token> [json body] -> the HTTP status; the body goes to $BODY_FILE
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

expect_no_route() { # <label> <method> <path> <token>
  local actual
  actual="$(http "$2" "$3" "$4")"

  case "$actual" in
    404 | 405 | 501) printf '  ok    %s -> no route (%s)\n' "$1" "$actual" ;;
    *)
      printf '  FAIL  %s -> expected no route (404/405/501), got %s\n' "$1" "$actual"
      FAILURES=$((FAILURES + 1))
      ;;
  esac
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

driver_field() { # <driver id> <python expression over the driver object>
  internal_call "$DRIVER_ADDR" "$DRIVER_SVC/GetDriver" "{\"driver_id\":\"$1\"}" | json_field "d[\"driver\"][$2]"
}

set_availability() { # <driver id> <AVAILABILITY_STATUS_...>
  internal_call "$DRIVER_ADDR" "$DRIVER_SVC/UpdateAvailability" "{\"driver_id\":\"$1\",\"availability_status\":\"$2\"}" > /dev/null
}

request_trip() { # <token> <rider id> -> the new trip's id
  local status
  status="$(http POST "/v1/trips" "$1" "{\"riderId\":\"$2\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicleClass\":\"economy\"}")"
  [ "$status" = 200 ] || { echo "FAIL: could not request a trip ($status): $(head -c 200 "$BODY_FILE")" >&2; exit 1; }
  body_field 'd["trip"]["id"]'
}

echo "==> [1/7] preparing: the gateway, the offers table, riders, drivers, tokens, a clean slate"
curl -sS -o /dev/null --max-time 5 "$BASE/v1/zones" -H "Authorization: Bearer x" \
  || { echo "FAIL: the gateway is not answering on $BASE" >&2; exit 1; }

[ "$(sql ride-trip-postgres "select to_regclass('public.trip_offers') is not null;")" = t ] \
  || { echo "FAIL: the trip_offers table does not exist: apply the trip-service migrations (goose up)" >&2; exit 1; }

buf build -o "$PROTOSET"

RIDER_1="$(ensure_rider "$RIDER_IDENTITY_1" "Authz Test Rider")"
RIDER_2="$(ensure_rider "$RIDER_IDENTITY_2" "Authz Test Rider Two")"

IDENTITY_DRIVER_A="$(driver_field "$DRIVER_A" '"identityId"')"
IDENTITY_DRIVER_B="$(driver_field "$DRIVER_B" '"identityId"')"
[ -n "$IDENTITY_DRIVER_A" ] && [ -n "$IDENTITY_DRIVER_B" ] || { echo "FAIL: the test drivers have no identity" >&2; exit 1; }

T1="$(mint "$RIDER_IDENTITY_1")"
T2="$(mint "$RIDER_IDENTITY_2")"
TA="$(mint "$IDENTITY_DRIVER_A")"
TB="$(mint "$IDENTITY_DRIVER_B")"

for id in $(sql ride-trip-postgres "select id from trips where rider_id in ('$RIDER_1','$RIDER_2') and status in ('requested','accepted','in_progress') or driver_id in ('$DRIVER_A','$DRIVER_B') and status in ('accepted','in_progress');"); do
  internal_call "$TRIP_ADDR" "$TRIP_SVC/CancelTrip" "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
done

sql ride-trip-postgres "update trip_offers set status='expired' where driver_id in ('$DRIVER_A','$DRIVER_B') and status='pending';" > /dev/null

ORIGINAL_A="$(driver_field "$DRIVER_A" '"availabilityStatus"')"
ORIGINAL_B="$(driver_field "$DRIVER_B" '"availabilityStatus"')"
set_availability "$DRIVER_A" AVAILABILITY_STATUS_OFFLINE
set_availability "$DRIVER_B" AVAILABILITY_STATUS_OFFLINE

restore() {
  sql ride-trip-postgres "update trip_offers set status='expired' where driver_id in ('$DRIVER_A','$DRIVER_B') and status='pending';" > /dev/null 2>&1 || true
  set_availability "$DRIVER_A" "$ORIGINAL_A" > /dev/null 2>&1 || true
  set_availability "$DRIVER_B" "$ORIGINAL_B" > /dev/null 2>&1 || true
}
trap 'restore; rm -f "$BODY_FILE"' EXIT

echo "==> [2/7] only dispatch offers a trip"
TRIP_1="$(request_trip "$T1" "$RIDER_1")"
echo "    trip 1: $TRIP_1"

OFFER_ARGS="{\"trip_id\":\"$TRIP_1\",\"driver_id\":\"$DRIVER_A\",\"ttl_seconds\":20}"
for who in "driver A:$TA" "driver B:$TB" "rider 1:$T1"; do
  CODE="$(code_of "${who#*:}" "$TRIP_SVC/OfferTrip" "$OFFER_ARGS")"
  if [ "$CODE" = PermissionDenied ]; then pass "${who%%:*} cannot offer a trip -> PermissionDenied"; else fail "${who%%:*} offering a trip -> $CODE"; fi
done

expect_no_route "there is no HTTP route to offer a trip"    POST "/v1/trips/$TRIP_1:offer" "$T1"
expect_offer "dispatch offers trip 1 to driver A (20 s)"    OK "$TRIP_1" "$DRIVER_A" 20

echo "==> [3/7] the driver sees it; nobody else does"
expect "driver A reads their offer"                          200 GET "/v1/drivers/$DRIVER_A/offer" "$TA"
if [ "$(body_field 'd["offer"]["tripId"]')" = "$TRIP_1" ]; then pass "it is trip 1"; else fail "it is not trip 1: $(head -c 200 "$BODY_FILE")"; fi
if [ "$(body_field 'sorted(d["offer"].keys())')" = "['dropoff', 'expiresAt', 'offeredAt', 'paymentMethod', 'pickup', 'tripId', 'vehicleClass']" ]; then
  pass "the offer shows the route, the vehicle class, the payment method and the times, and does not identify the rider"
else
  fail "the offer has unexpected fields: $(body_field 'sorted(d["offer"].keys())')"
fi
LEFT="$(body_field '(__import__("datetime").datetime.fromisoformat(d["offer"]["expiresAt"].replace("Z", "+00:00")) - __import__("datetime").datetime.now(__import__("datetime").timezone.utc)).total_seconds()')"
if python3 -c "import sys; sys.exit(0 if 5 < float('$LEFT') <= 20 else 1)"; then pass "it has about 20 seconds left ($LEFT)"; else fail "the offer has $LEFT seconds left, expected about 20"; fi

expect "driver B has no offer"                               404 GET "/v1/drivers/$DRIVER_B/offer" "$TB"
expect "driver B reads driver A's offer"                     403 GET "/v1/drivers/$DRIVER_A/offer" "$TB"
expect "rider 1 reads driver A's offer"                      403 GET "/v1/drivers/$DRIVER_A/offer" "$T1"
expect "nobody reads driver A's offer"                       401 GET "/v1/drivers/$DRIVER_A/offer" ""

echo "==> [4/7] the rules"
TRIP_2="$(request_trip "$T2" "$RIDER_2")"
echo "    trip 2: $TRIP_2"

expect_offer "another driver while A's offer is live"        Aborted "$TRIP_1" "$DRIVER_B"
expect_offer "the same driver again"                         AlreadyExists "$TRIP_1" "$DRIVER_A"
expect_offer "driver A, who already has a live offer, for trip 2" FailedPrecondition "$TRIP_2" "$DRIVER_A"
expect_offer "a trip that does not exist"                    NotFound "00000000-0000-4000-8000-00000000dead" "$DRIVER_A"

echo "==> [5/7] answering"
expect "driver B answers as driver A"                        403 POST "/v1/trips/$TRIP_1:accept-offer" "$TB" "{\"driverId\":\"$DRIVER_A\"}"
expect "driver A rejects as driver B"                        403 POST "/v1/trips/$TRIP_1:reject-offer" "$TA" "{\"driverId\":\"$DRIVER_B\"}"
expect "rider 1 accepts as driver A"                         403 POST "/v1/trips/$TRIP_1:accept-offer" "$T1" "{\"driverId\":\"$DRIVER_A\"}"
expect "driver B accepts a trip never offered to them"       404 POST "/v1/trips/$TRIP_1:accept-offer" "$TB" "{\"driverId\":\"$DRIVER_B\"}"
expect "driver A rejects the offer"                          200 POST "/v1/trips/$TRIP_1:reject-offer" "$TA" "{\"driverId\":\"$DRIVER_A\"}"
expect "driver A no longer has an offer"                     404 GET "/v1/drivers/$DRIVER_A/offer" "$TA"
expect "driver A rejects it again"                           404 POST "/v1/trips/$TRIP_1:reject-offer" "$TA" "{\"driverId\":\"$DRIVER_A\"}"
expect_offer "driver A is never offered trip 1 again"        AlreadyExists "$TRIP_1" "$DRIVER_A"
expect_offer "dispatch offers trip 1 to driver B"            OK "$TRIP_1" "$DRIVER_B" 20
expect "driver B reads their offer"                          200 GET "/v1/drivers/$DRIVER_B/offer" "$TB"
expect "driver B accepts"                                    200 POST "/v1/trips/$TRIP_1:accept-offer" "$TB" "{\"driverId\":\"$DRIVER_B\"}"
if [ "$(body_field 'd["trip"]["status"]')" = "TRIP_STATUS_ACCEPTED" ] && [ "$(body_field 'd["trip"]["driverId"]')" = "$DRIVER_B" ]; then
  pass "the trip is accepted, and driver B is its driver"
else
  fail "the accepted trip is not as expected: $(head -c 300 "$BODY_FILE")"
fi
expect "rider 1 sees the trip accepted by driver B"          200 GET "/v1/trips/$TRIP_1" "$T1"
if [ "$(body_field 'd["trip"]["driverId"]')" = "$DRIVER_B" ]; then pass "it says driver B"; else fail "the trip's driver is not driver B"; fi
expect "driver B has no offer any more"                      404 GET "/v1/drivers/$DRIVER_B/offer" "$TB"
expect "the same offer cannot be accepted twice"             400 POST "/v1/trips/$TRIP_1:accept-offer" "$TB" "{\"driverId\":\"$DRIVER_B\"}"

echo "==> [6/7] a driver on a trip is not offered another"
expect_offer "driver B, on trip 1, for trip 2"               FailedPrecondition "$TRIP_2" "$DRIVER_B"

echo "==> [7/7] expiry, and a cancelled trip withdraws its offer"
expect_offer "dispatch offers trip 2 to driver A for 5 s"    OK "$TRIP_2" "$DRIVER_A" 5
expect "driver A sees it"                                    200 GET "/v1/drivers/$DRIVER_A/offer" "$TA"
echo "    waiting 6 s for the offer to expire"
sleep 6
expect "the expired offer is not shown"                      404 GET "/v1/drivers/$DRIVER_A/offer" "$TA"
expect "the expired offer cannot be accepted"                400 POST "/v1/trips/$TRIP_2:accept-offer" "$TA" "{\"driverId\":\"$DRIVER_A\"}"
expect_offer "an expired offer is not made again"            AlreadyExists "$TRIP_2" "$DRIVER_A"

expect "rider 2 cancels trip 2"                              200 POST "/v1/trips/$TRIP_2:cancel" "$T2" '{"reason":"offers test"}'
TRIP_3="$(request_trip "$T2" "$RIDER_2")"
expect_offer "dispatch offers trip 3 to driver A"            OK "$TRIP_3" "$DRIVER_A" 30
expect "driver A sees it"                                    200 GET "/v1/drivers/$DRIVER_A/offer" "$TA"
expect "rider 2 cancels trip 3"                              200 POST "/v1/trips/$TRIP_3:cancel" "$T2" '{"reason":"offers test"}'
expect "driver A no longer sees the offer of a cancelled trip" 404 GET "/v1/drivers/$DRIVER_A/offer" "$TA"
expect "and cannot accept it"                                400 POST "/v1/trips/$TRIP_3:accept-offer" "$TA" "{\"driverId\":\"$DRIVER_A\"}"

echo "==> cleaning up: driver B finishes trip 1"
expect "driver B starts trip 1"                              200 POST "/v1/trips/$TRIP_1:start" "$TB"
expect "driver B completes trip 1"                           200 POST "/v1/trips/$TRIP_1:complete" "$TB"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: driver offers work: only dispatch offers, the driver answers only as themselves, the rules hold, offers expire, and a cancelled trip withdraws its offer"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
