#!/usr/bin/env bash
# End-to-end test of trips for someone else and trips booked ahead, on the
# real services, through the gateway. Run from the ride-platform repo root
# (takes about a minute):
#   bash scripts/e2e/test-scheduled-trips.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken) and trip-service with migration 00014. The
# scheduler's own timing is not waited for: a booking is made due by moving
# its time in the database, then the scheduler (every 15 seconds by default)
# dispatches it.
#
# What it proves:
#   1. a rider requests a trip for someone else: the trip carries their name
#      and phone, the phone only while the trip is under way
#   2. a booking is checked (the time, the zone, the passenger), made once per
#      key, listed only by its rider, at most 3 upcoming, and cancelled free
#   3. a due booking becomes a trip with the booking's id, the passenger and
#      the booking's class
#   4. a booking that cannot be dispatched past its time fails, with the
#      reason, and the rider's notification event is written
#
# It creates a throw-away city and zone (far from any real one), a rider and
# a staff member, and removes them again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
at() { python3 -c "import datetime as t; print((t.datetime.now(t.timezone.utc) + t.timedelta(minutes=$1)).strftime('%Y-%m-%dT%H:%M:%SZ'))"; }
RIDER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
OPERATOR_IDENTITY="$(uuid)"
OPERATOR_STAFF_ID="$(uuid)"
UNKNOWN="$(uuid)"
CITY="E2E Schedule City $RUN"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
OTHER_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  local riders="'${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}'"
  sql ride-trip-postgres "
    delete from scheduled_trips where rider_id in ($riders);
    delete from trip_offers where trip_id in (select id from trips where rider_id in ($riders));
    delete from trips where rider_id in ($riders);" > /dev/null 2>&1 || true
  sql ride-staff-postgres "delete from staff_members where id = '$OPERATOR_STAFF_ID';" > /dev/null 2>&1 || true
  sql ride-location-postgres "
    delete from zones where city_id in (select id from cities where name like 'E2E Schedule City%');
    delete from cities where name like 'E2E Schedule City%';" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID$OTHER_ID" ]; then
    sql ride-rider-postgres "delete from riders where id in ($riders);" > /dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1"; }

body_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE"
}

http() { # <method> <path> <token> [json body]
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
    printf '  FAIL  %s -> expected %s, got %s: %s\n' "$1" "$2" "$actual" "$(head -c 300 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

check() { # <label> <expected> <actual>
  if [ "$2" = "$3" ]; then
    printf '  ok    %s -> %s\n' "$1" "$3"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "$3"
    FAILURES=$((FAILURES + 1))
  fi
}

message() { body_field 'd.get("message", "")'; }

PICKUP='{"latitude":24.05,"longitude":24.05}'
DROPOFF='{"latitude":24.07,"longitude":24.07}'
PASSENGER='"passengerName":"Sara","passengerPhone":"+9647500000002"'

book() { # <minutes ahead> <key> [extra json fields]
  echo "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"pickupAddress\":\"Home\",\"dropoffAddress\":\"Airport\",\"vehicleClass\":\"comfort\",\"scheduledAt\":\"$(at "$1")\",\"idempotencyKey\":\"$2\"${3:+,$3}}"
}

# status_of <booking id>: waits up to 45 seconds for it to leave 'scheduled'.
status_of() {
  local got=scheduled
  for _ in $(seq 1 45); do
    got="$(sql ride-trip-postgres "select status from scheduled_trips where id = '$1'")"
    [ "$got" != scheduled ] && break
    sleep 1
  done
  echo "$got"
}

[ "$(sql ride-trip-postgres "select to_regclass('public.scheduled_trips') is not null")" = t ] \
  || { echo "ABORT: the scheduled trip table does not exist: apply trip-service migration 00014 (goose up)" >&2; exit 2; }

RIDER="$(mint "$RIDER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
OPERATOR="$(mint "$OPERATOR_IDENTITY")"

echo "==> [0/4] a served zone, two riders"
sql ride-staff-postgres "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
  values ('$OPERATOR_STAFF_ID', '$OPERATOR_IDENTITY', 'e2e-schedule-$RUN@ride.test', 'E2E Schedule', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values ('$OPERATOR_STAFF_ID', '$OPERATIONS_ROLE');" > /dev/null
expect "a city in Baghdad's time" 200 POST /v1/admin/cities "$OPERATOR" "{\"name\":\"$CITY\",\"timeZone\":\"Asia/Baghdad\",\"center\":$PICKUP}"
CITY_ID="$(body_field 'd["city"]["id"]')"
expect "a zone" 200 POST /v1/admin/zones "$OPERATOR" "{\"cityId\":\"$CITY_ID\",\"name\":\"E2E\",\"boundary\":[{\"latitude\":24,\"longitude\":24},{\"latitude\":24,\"longitude\":24.1},{\"latitude\":24.1,\"longitude\":24.1},{\"latitude\":24.1,\"longitude\":24}]}"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Schedule Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "another rider" 200 POST /v1/riders "$OTHER" "{\"identityId\":\"$OTHER_IDENTITY\",\"displayName\":\"E2E Schedule Other\"}"
OTHER_ID="$(body_field 'd["rider"]["id"]')"

echo "==> [1/4] a trip for someone else"
expect "a name without a phone" 400 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"passengerName\":\"Sara\"}"
expect "for Sara" 200 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,$PASSENGER}"
TRIP="$(body_field 'd["trip"]["id"]')"
check "the trip carries her" "Sara +9647500000002 False" "$(body_field '" ".join((d["trip"]["passengerName"], d["trip"]["passengerPhone"], str(d["trip"]["scheduled"])))')"
expect "cancelled" 200 POST "/v1/trips/$TRIP:cancel" "$RIDER" '{"reason":"e2e"}'
expect "read after" 200 GET "/v1/trips/$TRIP" "$RIDER"
check "her name stays, her phone does not" "Sara " "$(body_field '" ".join((d["trip"]["passengerName"], d["trip"].get("passengerPhone", "")))')"

echo "==> [2/4] booking ahead"
BOOK=/v1/scheduled-trips
expect "in 10 minutes" 400 POST "$BOOK" "$RIDER" "$(book 10 "x1-$RUN")"
expect "in 8 days" 400 POST "$BOOK" "$RIDER" "$(book $((8 * 24 * 60)) "x2-$RUN")"
expect "half a passenger" 400 POST "$BOOK" "$RIDER" "$(book 120 "x3-$RUN" '"passengerName":"Sara"')"
expect "outside every zone" 400 POST "$BOOK" "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":{\"latitude\":10,\"longitude\":10},\"dropoff\":$DROPOFF,\"scheduledAt\":\"$(at 120)\",\"idempotencyKey\":\"x4-$RUN\"}"
expect "for another rider" 403 POST "$BOOK" "$OTHER" "$(book 120 "b1-$RUN")"
expect "in two hours, for Sara" 200 POST "$BOOK" "$RIDER" "$(book 120 "b1-$RUN" "$PASSENGER")"
FIRST="$(body_field 'd["scheduledTrip"]["id"]')"
check "scheduled, comfort, for Sara, Baghdad's time" "scheduled comfort Sara Asia/Baghdad 16" \
  "$(body_field '" ".join((d["scheduledTrip"]["status"], d["scheduledTrip"]["vehicleClass"], d["scheduledTrip"]["passengerName"], d["scheduledTrip"]["timeZone"], str(len(d["scheduledTrip"]["scheduledLocal"]))))')"
expect "the same key again" 200 POST "$BOOK" "$RIDER" "$(book 120 "b1-$RUN" "$PASSENGER")"
check "is the same booking" "$FIRST" "$(body_field 'd["scheduledTrip"]["id"]')"
expect "two more" 200 POST "$BOOK" "$RIDER" "$(book 180 "b2-$RUN")"
SECOND="$(body_field 'd["scheduledTrip"]["id"]')"
expect "and a third" 200 POST "$BOOK" "$RIDER" "$(book 240 "b3-$RUN")"
THIRD="$(body_field 'd["scheduledTrip"]["id"]')"
expect "a fourth upcoming" 400 POST "$BOOK" "$RIDER" "$(book 300 "b4-$RUN")"
expect "the rider's list" 200 GET "/v1/riders/$RIDER_ID/scheduled-trips" "$RIDER"
check "three, soonest first" "3 $FIRST" "$(body_field '" ".join((str(len(d["scheduledTrips"])), d["scheduledTrips"][0]["id"]))')"
expect "another rider reads it" 403 GET "/v1/riders/$RIDER_ID/scheduled-trips" "$OTHER"
expect "another rider cancels it" 403 POST "$BOOK/$THIRD:cancel" "$OTHER" "{\"riderId\":\"$RIDER_ID\"}"
expect "the rider cancels the third" 200 POST "$BOOK/$THIRD:cancel" "$RIDER" "{\"riderId\":\"$RIDER_ID\"}"
check "cancelled" cancelled "$(body_field 'd["scheduledTrip"]["status"]')"
expect "cancelling it again" 400 POST "$BOOK/$THIRD:cancel" "$RIDER" "{\"riderId\":\"$RIDER_ID\"}"

echo "==> [3/4] a due booking becomes a trip"
sql ride-trip-postgres "update scheduled_trips set scheduled_at = now() + interval '5 minutes', next_attempt_at = now() where id = '$FIRST';" > /dev/null
check "dispatched by the scheduler" dispatched "$(status_of "$FIRST")"
expect "the trip, with the booking's id" 200 GET "/v1/trips/$FIRST" "$RIDER"
check "requested, booked ahead, comfort, for Sara" "TRIP_STATUS_REQUESTED True comfort Sara +9647500000002 Home" \
  "$(body_field '" ".join((d["trip"]["status"], str(d["trip"]["scheduled"]), d["trip"]["vehicleClass"], d["trip"]["passengerName"], d["trip"]["passengerPhone"], d["trip"]["pickupAddress"]))')"
expect "the list, with the past" 200 GET "/v1/riders/$RIDER_ID/scheduled-trips?include_past=true" "$RIDER"
check "shows the trip" "dispatched $FIRST" "$(body_field "next(' '.join((s['status'], s['tripId'])) for s in d['scheduledTrips'] if s['id'] == '$FIRST')")"

echo "==> [4/4] a booking that cannot be dispatched"
# The rider is on the trip above; the second booking is past its time and grace.
sql ride-trip-postgres "update scheduled_trips set scheduled_at = now() - interval '11 minutes', next_attempt_at = now() where id = '$SECOND';" > /dev/null
check "failed by the scheduler" failed "$(status_of "$SECOND")"
expect "the list, with the past" 200 GET "/v1/riders/$RIDER_ID/scheduled-trips?include_past=true" "$RIDER"
check "says why" "rider already has an active trip" "$(body_field "next(s['failureReason'] for s in d['scheduledTrips'] if s['id'] == '$SECOND')")"
check "the rider will be told" 1 "$(sql ride-trip-postgres "select count(*) from outbox_events where event_type = 'trip.schedule_failed' and aggregate_id = '$SECOND'")"
expect "the dispatched trip is cancelled like any other" 200 POST "/v1/trips/$FIRST:cancel" "$RIDER" '{"reason":"e2e"}'

echo
if [ "$FAILURES" -ne 0 ]; then
  echo "FAIL: $FAILURES check(s) failed"
  exit 1
fi

echo "PASS: trips carry their passenger; bookings are checked, kept once, dispatched as trips with their id, or fail with the reason"
