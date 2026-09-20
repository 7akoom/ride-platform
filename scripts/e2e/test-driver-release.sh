#!/usr/bin/env bash
# End-to-end test of DRIVER RELEASE: a driver's availability follows their trips,
# through the real events (trip-service outbox, NATS, dispatch-service). Run from the
# ride-platform repo root:
#   bash scripts/e2e/test-driver-release.sh
#
# Works in either dispatch mode. About 40 seconds (plus up to 40 s at the start if
# driver A's live position has not expired yet). Tokens are minted for local
# development only (scripts/tools/devtoken) and never printed.
#
# The test plays dispatch itself (an internal AcceptTrip assigns the trip to driver A)
# with driver B offline and driver A without a live position, so the real dispatch
# cannot take the trips it creates. A and B are put back as they were at the end.
#
# What it proves:
#   1. a driver who accepts a trip becomes BUSY
#   2. when the trip is completed, or cancelled, they become AVAILABLE again
#   3. a driver who went OFFLINE during the trip is left offline: only busy drivers
#      are released, so a driver's own choice is never overridden
#   4. driver B, who had no trip, is not touched
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
NATS_MONITOR="${NATS_MONITOR_URL:-http://127.0.0.1:8222}"
INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"

DRIVER_A="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
DRIVER_B="4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
RIDER_IDENTITY_1="a0000000-0000-4000-8000-0000000000a1"

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
LOCATION_ADDR="localhost:$(compose_port location-service)"
RIDER_SVC="ride.rider.v1.RiderService"
DRIVER_SVC="ride.driver.v1.DriverService"
TRIP_SVC="ride.trip.v1.TripService"
LOCATION_SVC="ride.location.v1.LocationService"

FAILURES=0
BODY_FILE="$(mktemp)"

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

availability() { driver_field "$1" '"availabilityStatus"'; }

set_availability() { # <driver id> <AVAILABILITY_STATUS_...>
  internal_call "$DRIVER_ADDR" "$DRIVER_SVC/UpdateAvailability" "{\"driver_id\":\"$1\",\"availability_status\":\"$2\"}" > /dev/null
}

# wait_availability <driver id> <AVAILABILITY_STATUS_...> <seconds>
wait_availability() {
  local deadline=$((SECONDS + $3))
  while [ "$SECONDS" -lt "$deadline" ]; do
    [ "$(availability "$1")" = "$2" ] && return 0
    sleep 1
  done

  return 1
}

# stays_availability <driver id> <AVAILABILITY_STATUS_...> <seconds>: it never changes during the time
stays_availability() {
  local deadline=$((SECONDS + $3))
  while [ "$SECONDS" -lt "$deadline" ]; do
    [ "$(availability "$1")" = "$2" ] || return 1
    sleep 1
  done

  return 0
}

request_trip() { # <token> <rider id> -> the new trip's id
  local status
  status="$(http POST "/v1/trips" "$1" "{\"riderId\":\"$2\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicleClass\":\"economy\"}")"
  [ "$status" = 200 ] || { echo "FAIL: could not request a trip ($status): $(head -c 200 "$BODY_FILE")" >&2; exit 1; }
  body_field 'd["trip"]["id"]'
}

accept_as_dispatch() { # <trip id> <driver id>: what dispatch does when it assigns a trip
  internal_call "$TRIP_ADDR" "$TRIP_SVC/AcceptTrip" "{\"trip_id\":\"$1\",\"driver_id\":\"$2\"}" > /dev/null
}

driver_is_nearby() { # is driver A among the live drivers near the pickup point?
  internal_call "$LOCATION_ADDR" "$LOCATION_SVC/FindNearby" '{"entity_type":"ENTITY_TYPE_DRIVER","coordinates":{"latitude":36.19,"longitude":44.01},"radius_meters":5000,"limit":30}' 2>/dev/null \
    | json_field '"'"$DRIVER_A"'" in [e["entityId"] for e in d.get("entities", [])]'
}

cleanup() {
  for id in $(sql ride-trip-postgres "select id from trips where rider_id = '${RIDER_1:-none}' and status in ('requested','accepted','in_progress');" 2>/dev/null); do
    internal_call "$TRIP_ADDR" "$TRIP_SVC/CancelTrip" "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null 2>&1 || true
  done

  sleep 3

  [ -n "${ORIGINAL_A:-}" ] && set_availability "$DRIVER_A" "$ORIGINAL_A" > /dev/null 2>&1 || true
  [ -n "${ORIGINAL_B:-}" ] && set_availability "$DRIVER_B" "$ORIGINAL_B" > /dev/null 2>&1 || true

  rm -f "$BODY_FILE"
}
trap cleanup EXIT

echo "==> [1/5] preparing: the event stream, riders, drivers, tokens, a clean slate"
curl -sS -o /dev/null --max-time 5 "$BASE/v1/zones" -H "Authorization: Bearer x" \
  || { echo "FAIL: the gateway is not answering on $BASE" >&2; exit 1; }

# The trip events stream must capture the lifecycle subjects and dispatch must consume them.
if JSZ="$(curl -sS --max-time 5 "$NATS_MONITOR/jsz?streams=true&consumers=true&config=true" 2> /dev/null)" && [ -n "$JSZ" ]; then
  if python3 - "$JSZ" <<'PYEND'
import json, sys

def covers(pattern, subject):
    p, s = pattern.split("."), subject.split(".")
    for i, token in enumerate(p):
        if token == ">":
            return len(s) > i
        if i >= len(s) or (token != "*" and token != s[i]):
            return False
    return len(p) == len(s)

data = json.loads(sys.argv[1])
streams = [s for a in data.get("account_details", []) for s in a.get("stream_detail", [])]
trip = next((s for s in streams if s.get("name") == "TRIP_EVENTS"), None)

if trip is None:
    print("no TRIP_EVENTS stream is running", file=sys.stderr); sys.exit(1)

subjects = trip.get("config", {}).get("subjects", [])
missing = [w for w in ("trip.accepted", "trip.completed", "trip.cancelled") if not any(covers(p, w) for p in subjects)]
if missing:
    print(f"TRIP_EVENTS ({subjects}) does not capture {missing}", file=sys.stderr); sys.exit(1)

consumers = [c.get("name") for c in trip.get("consumer_detail", [])]
if "dispatch-trip-lifecycle" not in consumers:
    print(f"dispatch-service has no lifecycle consumer (consumers: {consumers}): rebuild and restart it", file=sys.stderr); sys.exit(1)
PYEND
  then
    pass "TRIP_EVENTS captures the lifecycle events and dispatch-service consumes them"
  else
    echo "FAIL: the event flow is not in place (see above)" >&2
    exit 1
  fi
else
  echo "    (the NATS monitor is not reachable on $NATS_MONITOR: the stream was not checked)"
fi

buf build -o "$PROTOSET"

RIDER_1="$(ensure_rider "$RIDER_IDENTITY_1" "Authz Test Rider")"

IDENTITY_DRIVER_A="$(driver_field "$DRIVER_A" '"identityId"')"
[ -n "$IDENTITY_DRIVER_A" ] || { echo "FAIL: driver A has no identity" >&2; exit 1; }

T1="$(mint "$RIDER_IDENTITY_1")"
TA="$(mint "$IDENTITY_DRIVER_A")"

for id in $(sql ride-trip-postgres "select id from trips where rider_id = '$RIDER_1' and status in ('requested','accepted','in_progress') or driver_id in ('$DRIVER_A','$DRIVER_B') and status in ('accepted','in_progress');"); do
  internal_call "$TRIP_ADDR" "$TRIP_SVC/CancelTrip" "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
done

ORIGINAL_A="$(availability "$DRIVER_A")"
ORIGINAL_B="$(availability "$DRIVER_B")"

set_availability "$DRIVER_B" AVAILABILITY_STATUS_OFFLINE
set_availability "$DRIVER_A" AVAILABILITY_STATUS_AVAILABLE

# A driver's live position lasts 30 s. Wait until driver A's has gone, or the real
# dispatch could assign these trips to them before the test does.
for _ in $(seq 1 40); do
  [ "$(driver_is_nearby)" = "False" ] && break
  sleep 1
done
[ "$(driver_is_nearby)" = "False" ] || { echo "FAIL: driver A still has a live position near the pickup; try again in a minute" >&2; exit 1; }

echo "==> [2/5] a driver who accepts a trip becomes busy"
TRIP_1="$(request_trip "$T1" "$RIDER_1")"
echo "    trip 1: $TRIP_1"
[ "$(availability "$DRIVER_A")" = "AVAILABILITY_STATUS_AVAILABLE" ] && pass "driver A is available before the trip" || fail "driver A is not available before the trip"

accept_as_dispatch "$TRIP_1" "$DRIVER_A"
if wait_availability "$DRIVER_A" AVAILABILITY_STATUS_BUSY 20; then pass "driver A became busy after accepting"; else fail "driver A is still $(availability "$DRIVER_A") 20 s after accepting"; fi

echo "==> [3/5] a completed trip releases the driver"
expect "driver A starts the trip"                        200 POST "/v1/trips/$TRIP_1:start" "$TA"
expect "driver A completes the trip"                     200 POST "/v1/trips/$TRIP_1:complete" "$TA"
if wait_availability "$DRIVER_A" AVAILABILITY_STATUS_AVAILABLE 20; then pass "driver A is available again"; else fail "driver A is still $(availability "$DRIVER_A") 20 s after completing"; fi

echo "==> [4/5] a cancelled trip releases the driver"
TRIP_2="$(request_trip "$T1" "$RIDER_1")"
echo "    trip 2: $TRIP_2"
accept_as_dispatch "$TRIP_2" "$DRIVER_A"
if wait_availability "$DRIVER_A" AVAILABILITY_STATUS_BUSY 20; then pass "driver A became busy"; else fail "driver A is still $(availability "$DRIVER_A") after accepting trip 2"; fi

expect "rider 1 cancels trip 2"                          200 POST "/v1/trips/$TRIP_2:cancel" "$T1" '{"reason":"release test"}'
if wait_availability "$DRIVER_A" AVAILABILITY_STATUS_AVAILABLE 20; then pass "driver A is available again"; else fail "driver A is still $(availability "$DRIVER_A") 20 s after the cancellation"; fi

echo "==> [5/5] a driver who went offline is not brought back"
TRIP_3="$(request_trip "$T1" "$RIDER_1")"
echo "    trip 3: $TRIP_3"
accept_as_dispatch "$TRIP_3" "$DRIVER_A"
if wait_availability "$DRIVER_A" AVAILABILITY_STATUS_BUSY 20; then pass "driver A became busy"; else fail "driver A is still $(availability "$DRIVER_A") after accepting trip 3"; fi

expect "driver A goes offline in the middle of the trip"  200 PUT "/v1/drivers/$DRIVER_A/availability" "$TA" '{"availabilityStatus":"AVAILABILITY_STATUS_OFFLINE"}'
expect "driver A starts the trip"                        200 POST "/v1/trips/$TRIP_3:start" "$TA"
expect "driver A completes the trip"                     200 POST "/v1/trips/$TRIP_3:complete" "$TA"
if stays_availability "$DRIVER_A" AVAILABILITY_STATUS_OFFLINE 8; then pass "driver A is still offline: their own choice was not overridden"; else fail "driver A is $(availability "$DRIVER_A") instead of offline"; fi

if [ "$(availability "$DRIVER_B")" = "AVAILABILITY_STATUS_OFFLINE" ]; then pass "driver B, who had no trip, was not touched"; else fail "driver B is $(availability "$DRIVER_B") instead of offline"; fi

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: a driver is busy while on a trip and available again after it (completed or cancelled), and a driver who went offline stays offline"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
