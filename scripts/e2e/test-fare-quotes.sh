#!/usr/bin/env bash
# End-to-end test of rate cards, surge and fare quotes, and of a trip that
# pays exactly its quote, on the real services, through the gateway. Run
# from the ride-platform repo root:
#   bash scripts/e2e/test-fare-quotes.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken).
#
# What it proves:
#   1. prices are staff business: pricing.manage (not the operations role)
#      sets a city's cards per class, with a minimum fare and fees; the base
#      card cannot be retired; unknown places and bad values are refused
#   2. a rider gets one quote per class, cheapest first, priced with the
#      city's cards (minimum fare, first-ride discount), with whether a free
#      driver of that class is near and how far; not for someone else
#   3. a zone's own card wins over the city's; a zone surge staff start
#      shows with its reason, and ending it or retiring the card undoes it
#   4. a trip requested with a quote keeps its price and class; a quote is
#      the rider's, used once, only before it expires, only for the quoted
#      points; the completed trip's fare is exactly the quoted one
#
# It creates a throw-away city and zone (far from any real one), two riders,
# a driver and two staff members, and removes them again. The city's cards
# turn weather and demand surge off, so the numbers do not depend on the sky.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
TRIP_ADDR="localhost:50055"
PROTOSET="/tmp/ride.binpb"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
DRIVER_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OPERATOR_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
OPERATOR_STAFF_ID="$(uuid)"
PLATE="E2E-QUOTE-$RUN"
CITY="E2E Quote City $RUN"
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
  sql ride-staff-postgres "delete from staff_members where id in ('$OWNER_STAFF_ID', '$OPERATOR_STAFF_ID');" > /dev/null 2>&1 || true
  if [ -n "$CITY_ID" ]; then
    sql ride-pricing-postgres "
      delete from fares where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');
      delete from coupon_redemptions where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');
      delete from rider_trip_stats where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');
      delete from fare_quotes where zone_id = '${ZONE_ID:-$UNKNOWN}' or city_id = '$CITY_ID';
      delete from zone_surges where zone_id = '${ZONE_ID:-$UNKNOWN}';
      delete from pricing_configs where city_id = '$CITY_ID' or zone_id = '${ZONE_ID:-$UNKNOWN}';" > /dev/null 2>&1 || true
  fi
  sql ride-location-postgres "
    delete from zones where city_id in (select id from cities where name like 'E2E Quote City%');
    delete from cities where name like 'E2E Quote City%';" > /dev/null 2>&1 || true
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

add_staff() { # <staff id> <identity> <role>
  sql ride-staff-postgres "
    insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
    values ('$1', '$2', 'e2e-quote-$1@ride.test', 'E2E Quote', 'active', now(), now());
    insert into staff_member_roles (staff_id, role_id) values ('$1', '$3');" > /dev/null
}

# The driver's position lasts 30 s: report it before each quote.
report_position() {
  curl -sS -o /dev/null -X PUT "$BASE/v1/locations/$DRIVER_ID" -H "Authorization: Bearer $DRIVER" \
    -H 'Content-Type: application/json' \
    -d '{"entityType":"ENTITY_TYPE_DRIVER","coordinates":{"latitude":20.052,"longitude":20.052}}' || true
}

# A card for the city or zone: rates are zero so a fare is its base (or the
# minimum), and weather and demand never count.
card() { # <place json> <class> <base> <minimum> <max surge>
  echo "{$1,\"vehicleClass\":\"$2\",\"baseFare\":\"$3\",\"perKmRate\":\"0\",\"perMinuteRate\":\"0\",\"minimumFare\":\"$4\",\"freeWaitingMinutes\":3,\"waitingPerMinute\":\"100\",\"cancellationFee\":\"1000\",\"cancellationGraceMinutes\":2,\"noShowFee\":\"2000\",\"maxSurgePercent\":\"$5\",\"demandSurge\":false,\"weatherSurge\":false}"
}

PICKUP='{"latitude":20.05,"longitude":20.05}'
DROPOFF='{"latitude":20.07,"longitude":20.07}'
quote_body() { echo "{\"riderId\":\"$1\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF}"; }

# class_field <class> <python expression over q>: a field of that class's quote.
class_field() {
  body_field "next(($2) for q in d[\"quotes\"] if q[\"vehicleClass\"] == \"$1\")"
}

buf build -o "$PROTOSET"
RIDER="$(mint "$RIDER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"
OPERATOR="$(mint "$OPERATOR_IDENTITY")"

echo "==> [0/4] a served zone, two riders, a free driver, two staff members"
add_staff "$OWNER_STAFF_ID" "$OWNER_IDENTITY" "$OWNER_ROLE"
add_staff "$OPERATOR_STAFF_ID" "$OPERATOR_IDENTITY" "$OPERATIONS_ROLE"
expect "a city" 200 POST /v1/admin/cities "$OPERATOR" "{\"name\":\"$CITY\",\"timeZone\":\"Asia/Baghdad\",\"center\":{\"latitude\":20.05,\"longitude\":20.05}}"
CITY_ID="$(body_field 'd["city"]["id"]')"
expect "a zone" 200 POST /v1/admin/zones "$OPERATOR" "{\"cityId\":\"$CITY_ID\",\"name\":\"E2E\",\"boundary\":[{\"latitude\":20,\"longitude\":20},{\"latitude\":20,\"longitude\":20.1},{\"latitude\":20.1,\"longitude\":20.1},{\"latitude\":20.1,\"longitude\":20}]}"
ZONE_ID="$(body_field 'd["zone"]["id"]')"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Quote Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "another rider" 200 POST /v1/riders "$OTHER" "{\"identityId\":\"$OTHER_IDENTITY\",\"displayName\":\"E2E Other Rider\"}"
OTHER_ID="$(body_field 'd["rider"]["id"]')"
expect "an economy driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Quote Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
sql ride-driver-postgres "update drivers set status = 'active', availability_status = 'available' where id = '$DRIVER_ID';" > /dev/null

echo "==> [1/4] rate cards are staff business"
CITY_PLACE="\"cityId\":\"$CITY_ID\""
expect "the operations role sets a card" 403 POST /v1/admin/rate-cards "$OPERATOR" "$(card "$CITY_PLACE" economy 1000 3000 0)"
expect "a rider lists cards" 403 GET /v1/admin/rate-cards "$RIDER"
expect "a zone and a city at once" 400 POST /v1/admin/rate-cards "$OWNER" "$(card "$CITY_PLACE,\"zoneId\":\"$ZONE_ID\"" economy 1000 3000 0)"
expect "a city that does not exist" 404 POST /v1/admin/rate-cards "$OWNER" "$(card "\"cityId\":\"$UNKNOWN\"" economy 1000 3000 0)"
expect "a negative base fare" 400 POST /v1/admin/rate-cards "$OWNER" "$(card "$CITY_PLACE" economy -1 3000 0)"
expect "the city's economy card" 200 POST /v1/admin/rate-cards "$OWNER" "$(card "$CITY_PLACE" economy 1000 3000 0)"
check "with its fees and who set it" "3000 1000 2000 $OWNER_IDENTITY" "$(body_field '" ".join((d["rateCard"]["minimumFare"], d["rateCard"]["cancellationFee"], d["rateCard"]["noShowFee"], d["rateCard"]["createdBy"]))')"
expect "the city's comfort card" 200 POST /v1/admin/rate-cards "$OWNER" "$(card "$CITY_PLACE" comfort 5000 0 0)"
expect "the city's cards" 200 GET "/v1/admin/rate-cards?city_id=$CITY_ID" "$OWNER"
check "comfort and economy" "comfort,economy" "$(body_field '",".join(sorted(c["vehicleClass"] for c in d["rateCards"]))')"
expect "retiring the base card" 400 POST /v1/admin/rate-cards:retire "$OWNER" '{}'

echo "==> [2/4] quotes for every class"
report_position
expect "another rider quotes for this one" 403 POST /v1/fare-quotes "$OTHER" "$(quote_body "$RIDER_ID")"
expect "the rider's quotes" 200 POST /v1/fare-quotes "$RIDER" "$(quote_body "$RIDER_ID")"
check "cheapest first" "economy,comfort" "$(body_field '",".join(q["vehicleClass"] for q in d["quotes"])')"
check "in the zone and city" "$ZONE_ID $CITY_ID" "$(body_field 'd["zoneId"] + " " + d["cityId"]')"
# 1000 raised to the 3000 minimum, then the first-ride discount (50%).
check "economy: the minimum, half off" "1500 2000 First ride discount" "$(class_field economy 'q["fare"]["total"] + " " + q["fare"]["minimumFareAdjustment"] + " " + q["fare"]["appliedDiscountLabel"]')"
check "comfort: its own card" 2500 "$(class_field comfort 'q["fare"]["total"]')"
check "an economy driver is near" True "$(class_field economy 'q["driversAvailable"] and q["pickupEtaMinutes"] >= 1')"
check "no comfort driver" "False 0" "$(class_field comfort 'str(q.get("driversAvailable", False)) + " " + str(q.get("pickupEtaMinutes", 0))')"
check "a quote holds for minutes" True "$(body_field '__import__("datetime").datetime.fromisoformat(d["quotes"][0]["expiresAt"].replace("Z", "+00:00")) > __import__("datetime").datetime.now(__import__("datetime").timezone.utc)')"

echo "==> [3/4] a zone's card, a zone surge"
ZONE_PLACE="\"zoneId\":\"$ZONE_ID\""
expect "the zone's economy card" 200 POST /v1/admin/rate-cards "$OWNER" "$(card "$ZONE_PLACE" economy 1000 4000 300)"
expect "a surge on the zone" 200 POST /v1/admin/zone-surges "$OWNER" "{\"zoneId\":\"$ZONE_ID\",\"surgePercent\":\"200\",\"reason\":\"E2E concert\",\"durationMinutes\":30}"
SURGE_ID="$(body_field 'd["zoneSurge"]["id"]')"
expect "a surge too long" 400 POST /v1/admin/zone-surges "$OWNER" "{\"zoneId\":\"$ZONE_ID\",\"surgePercent\":\"50\",\"reason\":\"x\",\"durationMinutes\":2000}"
report_position
expect "quotes during the surge" 200 POST /v1/fare-quotes "$RIDER" "$(quote_body "$RIDER_ID")"
# 4000 (the zone's minimum) tripled by +200%, then half off.
check "economy: the zone's card and the surge" "6000 200 E2E concert" "$(class_field economy 'q["fare"]["total"] + " " + q["fare"]["surge"]["totalPercent"] + " " + q["fare"]["surge"]["label"]')"
check "comfort: the city's card allows no surge" "2500 0" "$(class_field comfort 'q["fare"]["total"] + " " + q["fare"]["surge"]["totalPercent"]')"
expect "the surge ends" 200 POST "/v1/admin/zone-surges/$SURGE_ID:end" "$OWNER" '{}'
expect "ending it again" 400 POST "/v1/admin/zone-surges/$SURGE_ID:end" "$OWNER" '{}'
expect "the zone's card retires" 200 POST /v1/admin/rate-cards:retire "$OWNER" "{$ZONE_PLACE,\"vehicleClass\":\"economy\"}"
report_position
expect "quotes after" 200 POST /v1/fare-quotes "$RIDER" "$(quote_body "$RIDER_ID")"
check "economy: the city's card again, no zone surge" "1500 0" "$(class_field economy 'q["fare"]["total"] + " " + q["fare"]["surge"]["zonePercent"]')"
ECONOMY_QUOTE="$(class_field economy 'q["quoteId"]')"
COMFORT_QUOTE="$(class_field comfort 'q["quoteId"]')"

echo "==> [4/4] a trip pays its quote"
TRIP_BODY="\"pickup\":$PICKUP,\"dropoff\":$DROPOFF"
expect "another rider uses the quote" 404 POST /v1/trips "$OTHER" "{\"riderId\":\"$OTHER_ID\",$TRIP_BODY,\"quoteId\":\"$ECONOMY_QUOTE\"}"
expect "the quote for other points" 400 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":{\"latitude\":20.09,\"longitude\":20.09},\"quoteId\":\"$ECONOMY_QUOTE\"}"
expect "the quote for another class" 400 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",$TRIP_BODY,\"vehicleClass\":\"comfort\",\"quoteId\":\"$ECONOMY_QUOTE\"}"
sql ride-pricing-postgres "update fare_quotes set created_at = now() - interval '10 minutes', expires_at = now() - interval '1 minute' where id = '$COMFORT_QUOTE';" > /dev/null
expect "an expired quote" 400 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",$TRIP_BODY,\"quoteId\":\"$COMFORT_QUOTE\"}"
expect "the trip with its quote" 200 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",$TRIP_BODY,\"quoteId\":\"$ECONOMY_QUOTE\"}"
TRIP_ID="$(body_field 'd["trip"]["id"]')"
check "its price and class" "$ECONOMY_QUOTE 1500 IQD economy" "$(body_field '" ".join((d["trip"]["quoteId"], d["trip"]["quotedFare"], d["trip"]["currencyCode"], d["trip"]["vehicleClass"]))')"
expect "the rider cancels" 200 POST "/v1/trips/$TRIP_ID:cancel" "$RIDER" '{"reason":"e2e"}'
expect "the used quote again" 400 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",$TRIP_BODY,\"quoteId\":\"$ECONOMY_QUOTE\"}"

report_position
expect "a fresh quote" 200 POST /v1/fare-quotes "$RIDER" "$(quote_body "$RIDER_ID")"
FRESH_QUOTE="$(class_field economy 'q["quoteId"]')"
FRESH_TOTAL="$(class_field economy 'q["fare"]["total"]')"
# Prices go up after the quote: the trip still pays what it was quoted.
expect "the city's economy card goes up" 200 POST /v1/admin/rate-cards "$OWNER" "$(card "$CITY_PLACE" economy 9000 9000 0)"
expect "the trip" 200 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",$TRIP_BODY,\"quoteId\":\"$FRESH_QUOTE\"}"
TRIP_ID="$(body_field 'd["trip"]["id"]')"
# Dispatch may already have given the trip to this driver (the only one near).
grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" \
  -d "{\"trip_id\":\"$TRIP_ID\",\"driver_id\":\"$DRIVER_ID\"}" "$TRIP_ADDR" ride.trip.v1.TripService/AcceptTrip > /dev/null 2>&1 || true
expect "the driver's trip" 200 GET "/v1/trips/$TRIP_ID" "$DRIVER"
check "accepted by the driver" "TRIP_STATUS_ACCEPTED $DRIVER_ID" "$(body_field 'd["trip"]["status"] + " " + d["trip"]["driverId"]')"
expect "the driver starts" 200 POST "/v1/trips/$TRIP_ID:start" "$DRIVER" '{}'
expect "the driver completes" 200 POST "/v1/trips/$TRIP_ID:complete" "$DRIVER" '{}'

FARE=""
for _ in $(seq 1 30); do
  FARE="$(sql ride-pricing-postgres "select trim(trailing '.' from trim(trailing '0' from total::text)) || ' ' || quote_id from fares where trip_id = '$TRIP_ID';" 2> /dev/null || true)"
  [ -n "$FARE" ] && break
  sleep 1
done
check "the fare is the quote" "$FRESH_TOTAL $FRESH_QUOTE" "$FARE"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: staff set the prices, riders get a quote per class, and a trip pays exactly its quote"
