#!/usr/bin/env bash
# End-to-end test of DISPATCH WORKING BY OFFERS: the real dispatch-service puts a
# requested trip to the nearest eligible driver, who accepts or rejects it in the
# app; a rejected or expired offer goes on to the next driver. Run from the
# ride-platform repo root:
#   bash scripts/e2e/test-dispatch-offers.sh
#
# It restarts dispatch-service with DISPATCH_OFFER_TTL=15s for the test and back
# to its normal setting at the end. About 60 seconds. Tokens are minted for local
# development only (scripts/tools/devtoken) and never printed.
#
# It uses two drivers if both can take trips (their wallet must allow it) and says
# so; with only driver A the "goes on to the next driver" checks become "is never
# offered the same trip twice".
#
# What it proves, with nothing but the real services and the driver's own token:
#   1. a requested trip is OFFERED to the nearest driver, not assigned: it stays
#      requested until the driver answers
#   2. the driver sees it (GET /v1/drivers/{id}/offer) and rejects it; it is never
#      offered to the same driver again, and goes on to the next one
#   3. accepting makes the driver the driver of the trip
#   4. an offer nobody answers expires, and the trip goes on
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
REPORTER=""

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

# wait_offer <driver id> <token> <trip id> <seconds>: the driver is offered that trip within the time
wait_offer() {
  local deadline=$((SECONDS + $4)) status
  while [ "$SECONDS" -lt "$deadline" ]; do
    status="$(http GET "/v1/drivers/$1/offer" "$2")"

    if [ "$status" = 200 ] && [ "$(body_field 'd["offer"]["tripId"]')" = "$3" ]; then
      return 0
    fi

    sleep 1
  done

  return 1
}

# never_offered <driver id> <token> <seconds>: the driver is offered nothing during the time
never_offered() {
  local deadline=$((SECONDS + $3)) status
  while [ "$SECONDS" -lt "$deadline" ]; do
    status="$(http GET "/v1/drivers/$1/offer" "$2")"
    [ "$status" = 404 ] || return 1

    sleep 1
  done

  return 0
}

trip_status() { # <trip id> <token>
  http GET "/v1/trips/$1" "$2" > /dev/null
  body_field 'd["trip"]["status"]'
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

report_positions() { # a driver's position expires after 30 s, so both keep reporting
  curl -sS -o /dev/null -X PUT "$BASE/v1/locations/$DRIVER_A" -H "Authorization: Bearer $TA" -H 'Content-Type: application/json' \
    -d '{"entityType":"ENTITY_TYPE_DRIVER","coordinates":{"latitude":36.1905,"longitude":44.0105}}' || true
  curl -sS -o /dev/null -X PUT "$BASE/v1/locations/$DRIVER_B" -H "Authorization: Bearer $TB" -H 'Content-Type: application/json' \
    -d '{"entityType":"ENTITY_TYPE_DRIVER","coordinates":{"latitude":36.1915,"longitude":44.0115}}' || true
}

dispatch_mode() { # <fragment of the startup log line to wait for> <seconds>
  local deadline=$((SECONDS + $2))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if docker logs ride-dispatch-service 2>&1 | grep -q "$1"; then return 0; fi
    sleep 1
  done

  return 1
}

cleanup() {
  [ -n "$REPORTER" ] && kill "$REPORTER" > /dev/null 2>&1 || true

  for id in $(sql ride-trip-postgres "select id from trips where rider_id in ('${RIDER_1:-none}','${RIDER_2:-none}') and status in ('requested','accepted','in_progress');" 2>/dev/null); do
    internal_call "$TRIP_ADDR" "$TRIP_SVC/CancelTrip" "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null 2>&1 || true
  done

  sql ride-trip-postgres "update trip_offers set status='expired' where driver_id in ('$DRIVER_A','$DRIVER_B') and status='pending';" > /dev/null 2>&1 || true

  [ -n "${ORIGINAL_A:-}" ] && set_availability "$DRIVER_A" "$ORIGINAL_A" > /dev/null 2>&1 || true
  [ -n "${ORIGINAL_B:-}" ] && set_availability "$DRIVER_B" "$ORIGINAL_B" > /dev/null 2>&1 || true

  if [ "${DISPATCH_CHANGED:-no}" = yes ]; then
    (cd infrastructure/compose && docker compose up -d --no-deps --force-recreate dispatch-service > /dev/null 2>&1) || true
  fi

  rm -f "$BODY_FILE"
}
trap cleanup EXIT

echo "==> [1/5] preparing: riders, drivers (available, near the pickup), tokens, a clean slate"
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
set_availability "$DRIVER_A" AVAILABILITY_STATUS_AVAILABLE
set_availability "$DRIVER_B" AVAILABILITY_STATUS_AVAILABLE

http GET "/v1/drivers/$DRIVER_A/standing" "$TA" > /dev/null
[ "$(body_field 'd["canTakeTrips"]')" = "True" ] || { echo "FAIL: driver A cannot take trips (wallet): $(head -c 200 "$BODY_FILE")" >&2; exit 1; }

http GET "/v1/drivers/$DRIVER_B/standing" "$TB" > /dev/null
if [ "$(body_field 'd["canTakeTrips"]')" = "True" ]; then
  TWO_DRIVERS=yes
  echo "    two drivers can take trips: the 'goes on to the next driver' checks are real"
else
  TWO_DRIVERS=no
  set_availability "$DRIVER_B" AVAILABILITY_STATUS_OFFLINE
  echo "    driver B cannot take trips (wallet): testing with driver A only"
fi

report_positions
( while true; do sleep 5; report_positions; done ) &
REPORTER=$!

echo "==> [2/5] switching dispatch-service to offers (DISPATCH_OFFER_TTL=15s)"
DISPATCH_CHANGED=yes
(cd infrastructure/compose && DISPATCH_OFFER_TTL=15s docker compose up -d --no-deps --force-recreate dispatch-service > /dev/null 2>&1)
dispatch_mode "dispatch works by offers" 40 \
  || { echo "FAIL: dispatch-service did not start in offers mode (does its compose block pass DISPATCH_OFFER_TTL?)" >&2; docker logs --tail 8 ride-dispatch-service >&2; exit 1; }
pass "dispatch-service works by offers"
sleep 3

echo "==> [3/5] a requested trip is OFFERED to the nearest driver, not assigned"
TRIP_1="$(request_trip "$T1" "$RIDER_1")"
echo "    trip 1: $TRIP_1"

if wait_offer "$DRIVER_A" "$TA" "$TRIP_1" 30; then pass "driver A, the nearest, is offered trip 1"; else fail "driver A was not offered trip 1 within 30 s"; fi
if [ "$(body_field 'd["offer"]["expiresAt"] > d["offer"]["offeredAt"]')" = "True" ]; then pass "the offer has a window to answer in"; else fail "the offer has no window"; fi
if [ "$(trip_status "$TRIP_1" "$T1")" = "TRIP_STATUS_REQUESTED" ]; then pass "the trip is still requested: nobody was assigned"; else fail "the trip is not requested any more: $(trip_status "$TRIP_1" "$T1")"; fi

if [ "$TWO_DRIVERS" = yes ]; then
  expect "driver B is not offered it at the same time"   404 GET "/v1/drivers/$DRIVER_B/offer" "$TB"
fi

echo "==> [4/5] a rejected offer goes on, and never comes back"
expect "driver A rejects trip 1"                       200 POST "/v1/trips/$TRIP_1:reject-offer" "$TA" "{\"driverId\":\"$DRIVER_A\"}"

if [ "$TWO_DRIVERS" = yes ]; then
  if wait_offer "$DRIVER_B" "$TB" "$TRIP_1" 20; then pass "the trip goes on to driver B"; else fail "driver B was not offered trip 1 within 20 s of A rejecting it"; fi
  if never_offered "$DRIVER_A" "$TA" 4; then pass "driver A is not offered trip 1 again"; else fail "driver A was offered trip 1 again"; fi

  expect "driver B accepts trip 1"                     200 POST "/v1/trips/$TRIP_1:accept-offer" "$TB" "{\"driverId\":\"$DRIVER_B\"}"
  ACCEPTED_BY="$DRIVER_B"
  ACCEPTED_TOKEN="$TB"
  ACCEPTED_TRIP="$TRIP_1"
else
  if never_offered "$DRIVER_A" "$TA" 8; then pass "with nobody else to offer it to, driver A is not offered trip 1 again"; else fail "driver A was offered trip 1 again"; fi
  if [ "$(trip_status "$TRIP_1" "$T1")" = "TRIP_STATUS_REQUESTED" ]; then pass "and the trip is still waiting for a driver"; else fail "the trip is not waiting any more"; fi

  expect "rider 1 cancels trip 1"                      200 POST "/v1/trips/$TRIP_1:cancel" "$T1" '{"reason":"offers test"}'
  TRIP_2="$(request_trip "$T1" "$RIDER_1")"
  echo "    trip 2: $TRIP_2"

  if wait_offer "$DRIVER_A" "$TA" "$TRIP_2" 30; then pass "driver A is offered trip 2"; else fail "driver A was not offered trip 2 within 30 s"; fi
  expect "driver A accepts trip 2"                     200 POST "/v1/trips/$TRIP_2:accept-offer" "$TA" "{\"driverId\":\"$DRIVER_A\"}"
  ACCEPTED_BY="$DRIVER_A"
  ACCEPTED_TOKEN="$TA"
  ACCEPTED_TRIP="$TRIP_2"
fi

if [ "$(body_field 'd["trip"]["status"]')" = "TRIP_STATUS_ACCEPTED" ] && [ "$(body_field 'd["trip"]["driverId"]')" = "$ACCEPTED_BY" ]; then
  pass "the trip is accepted, and the driver who accepted is its driver"
else
  fail "the accepted trip is not as expected: $(head -c 300 "$BODY_FILE")"
fi
if [ "$(trip_status "$ACCEPTED_TRIP" "$T1")" = "TRIP_STATUS_ACCEPTED" ]; then pass "the rider sees it accepted"; else fail "the rider does not see the trip accepted"; fi

expect "the driver starts the trip"                    200 POST "/v1/trips/$ACCEPTED_TRIP:start" "$ACCEPTED_TOKEN"
expect "and completes it"                              200 POST "/v1/trips/$ACCEPTED_TRIP:complete" "$ACCEPTED_TOKEN"

echo "==> [5/5] an offer nobody answers expires, and the trip goes on"
TRIP_3="$(request_trip "$T2" "$RIDER_2")"
echo "    trip 3: $TRIP_3"

if wait_offer "$DRIVER_A" "$TA" "$TRIP_3" 30; then pass "driver A is offered trip 3"; else fail "driver A was not offered trip 3 within 30 s"; fi
echo "    driver A does nothing; waiting for the offer to expire (15 s)"

if [ "$TWO_DRIVERS" = yes ]; then
  if wait_offer "$DRIVER_B" "$TB" "$TRIP_3" 35; then pass "the unanswered offer expired and the trip went on to driver B"; else fail "driver B was not offered trip 3 within 35 s"; fi
  expect "driver A's expired offer is gone"            404 GET "/v1/drivers/$DRIVER_A/offer" "$TA"
else
  sleep 20
  expect "driver A's expired offer is gone"            404 GET "/v1/drivers/$DRIVER_A/offer" "$TA"
  if never_offered "$DRIVER_A" "$TA" 6; then pass "and it is not made again"; else fail "driver A was offered trip 3 again"; fi
fi
if [ "$(trip_status "$TRIP_3" "$T2")" = "TRIP_STATUS_REQUESTED" ]; then pass "the trip is still waiting: nobody was assigned"; else fail "the trip is not waiting any more"; fi

expect "rider 2 cancels trip 3"                        200 POST "/v1/trips/$TRIP_3:cancel" "$T2" '{"reason":"offers test"}'

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: dispatch works by offers: a trip is offered to the nearest driver, a rejected or unanswered offer goes on, and accepting makes the driver the driver of the trip"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
