#!/usr/bin/env bash
# End-to-end test of trips with stops on the way, on the real services,
# through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-trip-stops.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken), grpcurl, and OSRM running (routes are checked on it).
#
# What it proves:
#   1. a route passes through its via points and is longer for the detour; a
#      quote is priced through the stops and says so; at most 2 stops
#   2. a trip requested with a quote must have the quote's stops, keeps them,
#      and pays the quote; only its driver marks a stop, only while the trip
#      is in progress and at the stop, once; the event is written
#   3. a trip without a quote is priced through its stops when it completes,
#      exactly as estimated
#   4. a booking keeps its stops
#
# It creates a throw-away city and zone (in the desert near Rutba, away from
# any real zone but on OSRM's map), two riders, a driver and a staff member,
# and removes them again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
TRIP_ADDR="localhost:50055"
PROTOSET="/tmp/ride.binpb"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
at() { python3 -c "import datetime as t; print((t.datetime.now(t.timezone.utc) + t.timedelta(minutes=$1)).strftime('%Y-%m-%dT%H:%M:%SZ'))"; }
RIDER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
DRIVER_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
PLATE="E2E-STOPS-$RUN"
CITY="E2E Stops City $RUN"
UNKNOWN="$(uuid)"

INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' services/trip-service/.env | tr -d '"')"
[ -n "$INTERNAL_TOKEN" ] || { echo "ABORT: INTERNAL_SERVICE_TOKEN is missing from services/trip-service/.env" >&2; exit 2; }

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
OTHER_ID=""
CITY_ID=""
ZONE_ID=""
DRIVER_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  sql ride-staff-postgres "delete from staff_members where id = '$OWNER_STAFF_ID';" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID$OTHER_ID" ]; then
    sql ride-trip-postgres "
      delete from scheduled_trips where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');
      update trips set status = 'cancelled', cancelled_at = now()
       where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}') and status in ('requested', 'accepted', 'in_progress');" > /dev/null 2>&1 || true
  fi
  if [ -n "$CITY_ID" ]; then
    sql ride-pricing-postgres "
      delete from fares where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');
      delete from rider_trip_stats where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');
      delete from fare_quotes where zone_id = '${ZONE_ID:-$UNKNOWN}';
      delete from pricing_configs where city_id = '$CITY_ID';" > /dev/null 2>&1 || true
  fi
  sql ride-location-postgres "
    delete from zones where city_id in (select id from cities where name like 'E2E Stops City%');
    delete from cities where name like 'E2E Stops City%';" > /dev/null 2>&1 || true
  sql ride-driver-postgres "delete from drivers where vehicle_plate_number = '$PLATE';" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID$OTHER_ID" ]; then
    sql ride-rider-postgres "delete from riders where id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');" > /dev/null 2>&1 || true
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

internal_call() { # <address> <service/method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$3" "$1" "$2"
}

report_position() { # <latitude> <longitude>
  curl -sS -o /dev/null -X PUT "$BASE/v1/locations/$DRIVER_ID" -H "Authorization: Bearer $DRIVER" \
    -H 'Content-Type: application/json' \
    -d "{\"entityType\":\"ENTITY_TYPE_DRIVER\",\"coordinates\":{\"latitude\":$1,\"longitude\":$2}}" || true
}

# fare_of <trip id>: "<kind> <total>" once pricing recorded it.
fare_of() {
  local fare=""
  for _ in $(seq 1 30); do
    fare="$(sql ride-pricing-postgres "select kind || ' ' || trim(trailing '.' from trim(trailing '0' from total::text)) from fares where trip_id = '$1';" 2> /dev/null || true)"
    [ -n "$fare" ] && break
    sleep 1
  done
  echo "$fare"
}

PICKUP='{"latitude":33.035,"longitude":40.285}'
DROPOFF='{"latitude":33.045,"longitude":40.3}'
STOP_AT='{"latitude":33.08,"longitude":40.24}'
ONE_STOP="[{\"coordinates\":$STOP_AT,\"address\":\"Bakery\"}]"
TWO_STOPS="[{\"coordinates\":$STOP_AT,\"address\":\"Bakery\"},{\"coordinates\":{\"latitude\":33.06,\"longitude\":40.27},\"address\":\"Pharmacy\"}]"
THREE_STOPS="[{\"coordinates\":$STOP_AT},{\"coordinates\":$STOP_AT},{\"coordinates\":$STOP_AT}]"

buf build -o "$PROTOSET"
RIDER="$(mint "$RIDER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"

echo "==> [0/4] a served zone priced by distance, two riders, a driver"
sql ride-staff-postgres "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
  values ('$OWNER_STAFF_ID', '$OWNER_IDENTITY', 'e2e-stops-$RUN@ride.test', 'E2E Stops', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values ('$OWNER_STAFF_ID', '$OWNER_ROLE');" > /dev/null
expect "a city" 200 POST /v1/admin/cities "$OWNER" "{\"name\":\"$CITY\",\"timeZone\":\"Asia/Baghdad\",\"center\":$PICKUP}"
CITY_ID="$(body_field 'd["city"]["id"]')"
expect "a zone" 200 POST /v1/admin/zones "$OWNER" "{\"cityId\":\"$CITY_ID\",\"name\":\"E2E\",\"boundary\":[{\"latitude\":33,\"longitude\":40.2},{\"latitude\":33,\"longitude\":40.35},{\"latitude\":33.1,\"longitude\":40.35},{\"latitude\":33.1,\"longitude\":40.2}]}"
ZONE_ID="$(body_field 'd["zone"]["id"]')"
# Only distance is charged, surge is off: a longer route is a dearer trip.
expect "the city's card" 200 POST /v1/admin/rate-cards "$OWNER" "{\"cityId\":\"$CITY_ID\",\"baseFare\":\"1000\",\"perKmRate\":\"1000\",\"perMinuteRate\":\"0\",\"minimumFare\":\"0\",\"maxSurgePercent\":\"0\"}"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Stops Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "another rider" 200 POST /v1/riders "$OTHER" "{\"identityId\":\"$OTHER_IDENTITY\",\"displayName\":\"E2E Stops Other\"}"
OTHER_ID="$(body_field 'd["rider"]["id"]')"
expect "a driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Stops Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
sql ride-driver-postgres "update drivers set status = 'active', availability_status = 'available' where id = '$DRIVER_ID';" > /dev/null

echo "==> [1/4] routes and prices through stops"
ROUTE='"origin":{"latitude":36.1911,"longitude":44.0092},"destination":{"latitude":36.2367,"longitude":43.9631}'
expect "a route in Erbil" 200 POST /v1/routes:compute "$RIDER" "{$ROUTE}"
DIRECT="$(body_field 'd["distanceMeters"]')"
expect "the same, by way of a point east" 200 POST /v1/routes:compute "$RIDER" "{$ROUTE,\"via\":[{\"latitude\":36.2,\"longitude\":44.05}]}"
check "is longer" True "$(body_field "d['distanceMeters'] > $DIRECT + 1000")"
POINT='{"latitude":36.2,"longitude":44.0}'
SIX="$POINT,$POINT,$POINT,$POINT,$POINT,$POINT"
expect "through six points" 400 POST /v1/routes:compute "$RIDER" "{$ROUTE,\"via\":[$SIX]}"

class_field() { body_field "next($2 for q in d['quotes'] if q['vehicleClass'] == '$1')"; }
expect "a quote, straight there" 200 POST /v1/fare-quotes "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF}"
PLAIN="$(class_field economy 'q["fare"]["total"]')"
PLAIN_KM="$(class_field economy 'q["fare"].get("distanceKm", 0)')"
expect "a quote by way of the bakery" 200 POST /v1/fare-quotes "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"stops\":[$STOP_AT]}"
QUOTE="$(class_field economy 'q["quoteId"]')"
QUOTED="$(class_field economy 'q["fare"]["total"]')"
check "is longer and dearer" True "$(class_field economy "q['fare'].get('distanceKm', 0) > $PLAIN_KM and float(q['fare']['total']) > float('$PLAIN')")"
expect "a quote with three stops" 400 POST /v1/fare-quotes "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"stops\":[$STOP_AT,$STOP_AT,$STOP_AT]}"

echo "==> [2/4] a quoted trip through a stop"
expect "the quote, without its stop" 400 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"quoteId\":\"$QUOTE\"}"
check "does not match it" True "$(body_field '"does not match" in d["message"]')"
expect "a new quote by way of the bakery" 200 POST /v1/fare-quotes "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"stops\":[$STOP_AT]}"
QUOTE="$(class_field economy 'q["quoteId"]')"
QUOTED="$(class_field economy 'q["fare"]["total"]')"
expect "three stops" 400 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"stops\":$THREE_STOPS}"
expect "with the quote and its stop" 200 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"quoteId\":\"$QUOTE\",\"stops\":$ONE_STOP}"
TRIP="$(body_field 'd["trip"]["id"]')"
check "keeps its stop and its price" "1 Bakery $QUOTED" "$(body_field '" ".join((str(len(d["trip"]["stops"])), d["trip"]["stops"][0]["address"], d["trip"]["quotedFare"]))')"
internal_call "$TRIP_ADDR" ride.trip.v1.TripService/AcceptTrip "{\"trip_id\":\"$TRIP\",\"driver_id\":\"$DRIVER_ID\"}" > /dev/null 2>&1 || true
report_position 33.0801 40.2401
expect "the driver, before starting" 400 POST "/v1/trips/$TRIP:reach-stop" "$DRIVER" '{"position":1}'
expect "the driver starts" 200 POST "/v1/trips/$TRIP:start" "$DRIVER" '{}'
expect "a second stop it does not have" 404 POST "/v1/trips/$TRIP:reach-stop" "$DRIVER" '{"position":2}'
expect "the rider marks it" 403 POST "/v1/trips/$TRIP:reach-stop" "$RIDER" '{"position":1}'
report_position 33.035 40.285
expect "the driver, still at the pickup" 400 POST "/v1/trips/$TRIP:reach-stop" "$DRIVER" '{"position":1}'
report_position 33.0801 40.2401
expect "the driver, at the bakery" 200 POST "/v1/trips/$TRIP:reach-stop" "$DRIVER" '{"position":1}'
REACHED="$(body_field 'd["trip"]["stops"][0].get("reachedAt", "")')"
check "the stop is reached" True "$([ -n "$REACHED" ] && echo True || echo False)"
expect "again" 200 POST "/v1/trips/$TRIP:reach-stop" "$DRIVER" '{"position":1}'
check "changes nothing" "$REACHED" "$(body_field 'd["trip"]["stops"][0].get("reachedAt", "")')"
check "one trip.stop_reached event" 1 "$(sql ride-trip-postgres "select count(*) from outbox_events where event_type = 'trip.stop_reached' and aggregate_id = '$TRIP';")"
expect "the rider reads the trip" 200 GET "/v1/trips/$TRIP" "$RIDER"
check "and sees the stop reached" "$REACHED" "$(body_field 'd["trip"]["stops"][0].get("reachedAt", "")')"
expect "the driver completes" 200 POST "/v1/trips/$TRIP:complete" "$DRIVER" '{}'
check "the fare is the quote" "trip $QUOTED" "$(fare_of "$TRIP")"

echo "==> [3/4] an unquoted trip, priced through its stops"
expect "an estimate by way of two stops" 200 POST /v1/fare-estimates "$OTHER" "{\"riderId\":\"$OTHER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"stops\":[$STOP_AT,{\"latitude\":33.06,\"longitude\":40.27}]}"
ESTIMATE="$(body_field 'd["fare"]["total"]')"
expect "a trip with the two stops" 200 POST /v1/trips "$OTHER" "{\"riderId\":\"$OTHER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"stops\":$TWO_STOPS}"
TRIP="$(body_field 'd["trip"]["id"]')"
check "in order" "Bakery Pharmacy" "$(body_field '" ".join(s["address"] for s in d["trip"]["stops"])')"
internal_call "$TRIP_ADDR" ride.trip.v1.TripService/AcceptTrip "{\"trip_id\":\"$TRIP\",\"driver_id\":\"$DRIVER_ID\"}" > /dev/null 2>&1 || true
expect "the driver starts" 200 POST "/v1/trips/$TRIP:start" "$DRIVER" '{}'
report_position 33.0601 40.2701
expect "the pharmacy first: a rider may change the order" 200 POST "/v1/trips/$TRIP:reach-stop" "$DRIVER" '{"position":2}'
check "only the pharmacy is reached" "False True" "$(body_field '" ".join(str(s.get("reachedAt") is not None) for s in d["trip"]["stops"])')"
expect "the driver completes, the bakery skipped" 200 POST "/v1/trips/$TRIP:complete" "$DRIVER" '{}'
check "priced through both, as estimated" "trip $ESTIMATE" "$(fare_of "$TRIP")"

echo "==> [4/4] a booking with a stop"
expect "booked ahead by way of the bakery" 200 POST /v1/scheduled-trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"scheduledAt\":\"$(at 120)\",\"idempotencyKey\":\"stops-$RUN\",\"stops\":$ONE_STOP}"
check "keeps it" "1 Bakery" "$(body_field 'str(len(d["scheduledTrip"]["stops"])) + " " + d["scheduledTrip"]["stops"][0]["address"]')"
expect "three stops" 400 POST /v1/scheduled-trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"scheduledAt\":\"$(at 120)\",\"idempotencyKey\":\"stops3-$RUN\",\"stops\":$THREE_STOPS}"
expect "the rider's list" 200 GET "/v1/riders/$RIDER_ID/scheduled-trips" "$RIDER"
check "shows it" "Bakery" "$(body_field 'd["scheduledTrips"][0]["stops"][0]["address"]')"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: routes, quotes and fares go through a trip's stops; a quoted trip has its quote's stops; only the driver marks a stop, at it, once; bookings keep their stops"
