#!/usr/bin/env bash
# End-to-end test of the trip history THROUGH THE GATEWAY, with REAL user tokens:
# GET /v1/trips:active (the trip an app resumes) and GET /v1/trips (the history).
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-trip-history.sh
#
# It takes about 15 seconds. Tokens are minted for local development only
# (scripts/tools/devtoken) and are never printed. It creates three trips for the
# test rider (cancelling two of them, and the third at the end) and puts the test
# drivers offline meanwhile so dispatch cannot take the trips; it puts driver A
# back to available at the end.
#
# What it proves:
#   1. a rider with no active trip gets 404; with one, gets exactly that trip
#   2. the history is newest first, pages without gaps or repeats, and ends with an
#      empty next page token
#   3. a driver sees only their own trips
#   4. nobody sees anyone else's: another rider, a driver naming a rider, naming both
#      profiles or neither, the internal token; and a page token taken from one
#      rider's history shows another rider nothing
#   5. bad input is a 400: a negative page size, a page token that is not one
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

driver_identity() { # <driver id>
  internal_call "$DRIVER_ADDR" "$DRIVER_SVC/GetDriver" "{\"driver_id\":\"$1\"}" | json_field 'd["driver"]["identityId"]'
}

set_availability() { # <driver id> <AVAILABILITY_STATUS_...>
  internal_call "$DRIVER_ADDR" "$DRIVER_SVC/UpdateAvailability" "{\"driver_id\":\"$1\",\"availability_status\":\"$2\"}" > /dev/null
}

echo "==> [1/6] preparing: the gateway, protoset, riders, drivers, tokens, a clean slate"
curl -sS -o /dev/null --max-time 5 "$BASE/v1/zones" -H "Authorization: Bearer x" \
  || { echo "FAIL: the gateway is not answering on $BASE" >&2; exit 1; }

buf build -o "$PROTOSET"

RIDER_1="$(ensure_rider "$RIDER_IDENTITY_1" "Authz Test Rider")"
RIDER_2="$(ensure_rider "$RIDER_IDENTITY_2" "Authz Test Rider Two")"

IDENTITY_DRIVER_A="$(driver_identity "$DRIVER_A")"
IDENTITY_DRIVER_B="$(driver_identity "$DRIVER_B")"
[ -n "$IDENTITY_DRIVER_A" ] && [ -n "$IDENTITY_DRIVER_B" ] || { echo "FAIL: the test drivers have no identity" >&2; exit 1; }

T1="$(mint "$RIDER_IDENTITY_1")"
T2="$(mint "$RIDER_IDENTITY_2")"
TA="$(mint "$IDENTITY_DRIVER_A")"
TEXPIRED="$(mint "$RIDER_IDENTITY_1" -ttl=-1h)"

for id in $(sql ride-trip-postgres "select id from trips where rider_id in ('$RIDER_1','$RIDER_2') and status in ('requested','accepted','in_progress') or driver_id in ('$DRIVER_A','$DRIVER_B') and status in ('accepted','in_progress');"); do
  internal_call "$TRIP_ADDR" "$TRIP_SVC/CancelTrip" "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
done

availability_of() { # <driver id>
  internal_call "$DRIVER_ADDR" "$DRIVER_SVC/GetDriver" "{\"driver_id\":\"$1\"}" | json_field 'd["driver"]["availabilityStatus"]'
}

ORIGINAL_A="$(availability_of "$DRIVER_A")"
ORIGINAL_B="$(availability_of "$DRIVER_B")"

# Offline drivers cannot be assigned, so the trips this test requests stay as they are.
set_availability "$DRIVER_A" AVAILABILITY_STATUS_OFFLINE
set_availability "$DRIVER_B" AVAILABILITY_STATUS_OFFLINE

restore() {
  set_availability "$DRIVER_A" "$ORIGINAL_A" > /dev/null 2>&1 || true
  set_availability "$DRIVER_B" "$ORIGINAL_B" > /dev/null 2>&1 || true
}
trap 'restore; rm -f "$BODY_FILE"' EXIT

echo "==> [2/6] no active trip"
expect "rider 1 with no active trip"                    404 GET "/v1/trips:active?rider_id=$RIDER_1" "$T1"
if [ "$(body_field 'd["message"]')" = "no active trip" ]; then pass "it says so"; else fail "the message is not 'no active trip': $(head -c 200 "$BODY_FILE")"; fi
expect "driver A with no active trip"                   404 GET "/v1/trips:active?driver_id=$DRIVER_A" "$TA"

echo "==> [3/6] rider 1 takes three trips (the first two are cancelled, the third stays open)"
REQUEST="{\"riderId\":\"$RIDER_1\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicleClass\":\"economy\"}"
TRIPS=()

for n in 1 2 3; do
  STATUS="$(http POST "/v1/trips" "$T1" "$REQUEST")"
  [ "$STATUS" = 200 ] || { echo "FAIL: rider 1 could not request trip $n ($STATUS): $(head -c 200 "$BODY_FILE")" >&2; exit 1; }
  TRIPS+=("$(body_field 'd["trip"]["id"]')")

  if [ "$n" -lt 3 ]; then
    expect "rider 1 cancels trip $n" 200 POST "/v1/trips/${TRIPS[$((n - 1))]}:cancel" "$T1" '{"reason":"history test"}'
  fi

  sleep 1
done

TRIP_1="${TRIPS[0]}"
TRIP_2="${TRIPS[1]}"
TRIP_3="${TRIPS[2]}"
echo "    trips: $TRIP_1 $TRIP_2 $TRIP_3"

echo "==> [4/6] the active trip"
expect "rider 1 resumes their trip"                     200 GET "/v1/trips:active?rider_id=$RIDER_1" "$T1"
if [ "$(body_field 'd["trip"]["id"]')" = "$TRIP_3" ]; then pass "it is the open trip"; else fail "it is not the open trip: $(head -c 200 "$BODY_FILE")"; fi
if [ "$(body_field 'd["trip"]["status"]')" = "TRIP_STATUS_REQUESTED" ]; then pass "and it is still requested"; else fail "its status is not TRIP_STATUS_REQUESTED"; fi
expect "driver A still has no active trip"              404 GET "/v1/trips:active?driver_id=$DRIVER_A" "$TA"

echo "==> [5/6] the history"
expect "rider 1 reads page 1 (two trips)"               200 GET "/v1/trips?rider_id=$RIDER_1&page_size=2" "$T1"
IDS_1="$(body_field '" ".join(t["id"] for t in d["trips"])')"
TOKEN="$(body_field 'd.get("nextPageToken","")')"

if [ "$IDS_1" = "$TRIP_3 $TRIP_2" ]; then pass "page 1 is the two newest trips, newest first"; else fail "page 1 is '$IDS_1', expected '$TRIP_3 $TRIP_2'"; fi
if [ -n "$TOKEN" ]; then pass "and there is a next page"; else fail "there is no next page token, but rider 1 has at least three trips"; fi

expect "rider 1 reads page 2"                           200 GET "/v1/trips?rider_id=$RIDER_1&page_size=2&page_token=$TOKEN" "$T1"
IDS_2="$(body_field '" ".join(t["id"] for t in d["trips"])')"
FIRST_2="$(body_field 'd["trips"][0]["id"]')"

if [ "$FIRST_2" = "$TRIP_1" ]; then pass "page 2 starts with the third-newest trip"; else fail "page 2 starts with $FIRST_2, expected $TRIP_1"; fi

OVERLAP="$(python3 -c "print(len(set('$IDS_1'.split()) & set('$IDS_2'.split())))")"
if [ "$OVERLAP" = 0 ]; then pass "no trip appears on both pages"; else fail "$OVERLAP trip(s) appear on both pages"; fi

expect "rider 1 reads with the default page size"       200 GET "/v1/trips?rider_id=$RIDER_1" "$T1"
COUNT="$(body_field 'len(d["trips"])')"
if [ "$COUNT" -ge 3 ] && [ "$COUNT" -le 20 ]; then pass "the default page holds at most 20 trips ($COUNT)"; else fail "the default page holds $COUNT trips"; fi

expect "rider 1 asks for a huge page"                   200 GET "/v1/trips?rider_id=$RIDER_1&page_size=1000" "$T1"
COUNT="$(body_field 'len(d["trips"])')"
if [ "$COUNT" -le 50 ]; then pass "it is served as at most 50 ($COUNT)"; else fail "a page held $COUNT trips"; fi

ORDERED="$(body_field '(lambda ts: all(a >= b for a, b in zip(ts, ts[1:])))([__import__("datetime").datetime.fromisoformat(t["requestedAt"].replace("Z", "+00:00")) for t in d["trips"]])')"
if [ "$ORDERED" = "True" ]; then pass "the trips are in newest-first order"; else fail "the trips are not in newest-first order"; fi

# Walk the whole history page by page: every trip once, and the walk ends.
SEEN=""
NEXT=""
for _ in $(seq 1 200); do
  STATUS="$(http GET "/v1/trips?rider_id=$RIDER_1&page_size=25${NEXT:+&page_token=$NEXT}" "$T1")"
  [ "$STATUS" = 200 ] || { fail "walking the history failed with $STATUS"; break; }
  SEEN="$SEEN $(body_field '" ".join(t["id"] for t in d["trips"])')"
  NEXT="$(body_field 'd.get("nextPageToken","")')"
  [ -n "$NEXT" ] || break
done

TOTAL="$(sql ride-trip-postgres "select count(*) from trips where rider_id='$RIDER_1';")"
WALKED="$(python3 -c "print(len('$SEEN'.split()))")"
UNIQUE="$(python3 -c "print(len(set('$SEEN'.split())))")"

if [ -z "$NEXT" ] && [ "$WALKED" = "$TOTAL" ] && [ "$UNIQUE" = "$TOTAL" ]; then
  pass "paging through the whole history gives every trip once ($TOTAL) and ends"
else
  fail "paging gave $WALKED trips ($UNIQUE unique) for $TOTAL in the database, and did not end cleanly (token '$NEXT')"
fi

expect "driver A reads their history"                   200 GET "/v1/trips?driver_id=$DRIVER_A&page_size=50" "$TA"
FOREIGN="$(body_field 'sum(1 for t in d["trips"] if t["driverId"] != "'"$DRIVER_A"'")')"
if [ "$FOREIGN" = 0 ]; then pass "every trip in it is driver A's ($(body_field 'len(d["trips"])') trips)"; else fail "$FOREIGN trip(s) in driver A's history are not theirs"; fi

echo "==> [6/6] only your own, and bad input"
expect "rider 2 asks for rider 1's active trip"         403 GET "/v1/trips:active?rider_id=$RIDER_1" "$T2"
expect "rider 2 asks for rider 1's history"             403 GET "/v1/trips?rider_id=$RIDER_1" "$T2"
expect "driver A asks for rider 1's history"            403 GET "/v1/trips?rider_id=$RIDER_1" "$TA"
expect "rider 1 asks for driver A's history"            403 GET "/v1/trips?driver_id=$DRIVER_A" "$T1"
expect "rider 1 asks for driver A's active trip"        403 GET "/v1/trips:active?driver_id=$DRIVER_A" "$T1"
expect "rider 1 names both profiles"                    403 GET "/v1/trips?rider_id=$RIDER_1&driver_id=$DRIVER_A" "$T1"
expect "rider 1 names neither"                          403 GET "/v1/trips" "$T1"
expect "a profile that does not exist"                  403 GET "/v1/trips?rider_id=00000000-0000-4000-8000-00000000dead" "$T1"
expect "nobody asks for rider 1's history"              401 GET "/v1/trips?rider_id=$RIDER_1" ""
expect "an expired token"                               401 GET "/v1/trips?rider_id=$RIDER_1" "$TEXPIRED"
expect "the internal service token"                     401 GET "/v1/trips?rider_id=$RIDER_1" "$INTERNAL_TOKEN"

expect "rider 2 with rider 1's page token"              200 GET "/v1/trips?rider_id=$RIDER_2&page_token=$TOKEN" "$T2"
LEAK="$(body_field 'sum(1 for t in d["trips"] if t["riderId"] != "'"$RIDER_2"'")')"
if [ "$LEAK" = 0 ]; then pass "another rider's page token shows rider 2 none of rider 1's trips"; else fail "rider 2 saw $LEAK of rider 1's trips through a page token"; fi

expect "a negative page size"                           400 GET "/v1/trips?rider_id=$RIDER_1&page_size=-1" "$T1"
expect "a page token that is not one"                   400 GET "/v1/trips?rider_id=$RIDER_1&page_token=not-a-token" "$T1"
expect "a page token of the wrong kind"                 400 GET "/v1/trips?rider_id=$RIDER_1&page_token=aGVsbG8" "$T1"
expect_no_route "the history cannot be written to"           POST "/v1/trips:active" "$T1"

expect "rider 1 cancels the open trip"                  200 POST "/v1/trips/$TRIP_3:cancel" "$T1" '{"reason":"history test"}'
expect "and has no active trip again"                   404 GET "/v1/trips:active?rider_id=$RIDER_1" "$T1"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: the active trip and the history work through the gateway: newest first, paged without gaps, only your own, bad input refused"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
