#!/usr/bin/env bash
# End-to-end test of the second set of business reports (service levels,
# driver offers, ratings, money flows, live), on the real services, through
# the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-analytics-metrics.sh
#
# Needs the local identity signing key, grpcurl, buf, analytics-service with
# migration 00004, wallet-service answering SummarizeLedger, driver-service
# answering GetDriverSupply, and pricing writing discount_amount on
# fare.calculated.
#
# What it proves:
#   1. a real trip (quote, accepted, arrived, driven, completed, rated both
#      ways) shows in the service levels (how long each step took), the
#      ratings and the fare's discount; an offer the driver turns down shows
#      in the driver offers
#   2. a staff adjustment shows in the money flows (the wallets' ledger), with
#      what wallets hold now
#   3. the live view counts today's trips on the city's clock and the drivers
#   4. only analytics.read reads them; bad ranges are refused
#
# It creates a throw-away city and zone (far from any real one), a rider, a
# driver and two staff members, and removes them again.
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
DRIVER_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OPS_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
OPS_STAFF_ID="$(uuid)"
PLATE="E2E-MX-$RUN"
CITY="E2E Metrics City $RUN"
UNKNOWN="$(uuid)"

INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' services/trip-service/.env | tr -d '"')"
[ -n "$INTERNAL_TOKEN" ] || { echo "ABORT: INTERNAL_SERVICE_TOKEN is missing from services/trip-service/.env" >&2; exit 2; }

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
CITY_ID=""
ZONE_ID=""
DRIVER_ID=""
TRIP=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  sql ride-staff-postgres "delete from staff_members where id in ('$OWNER_STAFF_ID', '$OPS_STAFF_ID');" > /dev/null 2>&1 || true
  if [ -n "$CITY_ID" ]; then
    sql ride-pricing-postgres "
      delete from fares where rider_id = '${RIDER_ID:-$UNKNOWN}';
      delete from rider_trip_stats where rider_id = '${RIDER_ID:-$UNKNOWN}';
      delete from fare_quotes where zone_id = '${ZONE_ID:-$UNKNOWN}';
      delete from pricing_configs where city_id = '$CITY_ID';" > /dev/null 2>&1 || true
    sql ride-analytics-postgres "
      delete from offer_facts where trip_id in (select trip_id from trip_facts where city_id = '$CITY_ID');
      delete from trip_facts where city_id = '$CITY_ID';
      delete from rider_signups where rider_id = '${RIDER_ID:-$UNKNOWN}';
      delete from driver_signups where driver_id = '${DRIVER_ID:-$UNKNOWN}';" > /dev/null 2>&1 || true
  fi
  if [ -n "$RIDER_ID" ]; then
    sql ride-trip-postgres "
      update trips set status = 'cancelled', cancelled_at = now()
       where rider_id = '$RIDER_ID' and status in ('requested', 'accepted', 'in_progress');" > /dev/null 2>&1 || true
    sql ride-wallet-postgres "
      delete from wallet_adjustments where owner_id = '$RIDER_ID';
      delete from wallet_transactions where wallet_id in (select id from wallets where owner_id in ('$RIDER_ID', '${DRIVER_ID:-$UNKNOWN}'));
      delete from trip_settlements where rider_id = '$RIDER_ID';
      delete from wallets where owner_id in ('$RIDER_ID', '${DRIVER_ID:-$UNKNOWN}');" > /dev/null 2>&1 || true
    sql ride-rider-postgres "delete from riders where id = '$RIDER_ID';" > /dev/null 2>&1 || true
  fi
  sql ride-location-postgres "
    delete from zones where city_id in (select id from cities where name like 'E2E Metrics City%');
    delete from cities where name like 'E2E Metrics City%';" > /dev/null 2>&1 || true
  sql ride-driver-postgres "delete from drivers where vehicle_plate_number = '$PLATE';" > /dev/null 2>&1 || true
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

add_staff() { # <staff id> <identity> <role>
  sql ride-staff-postgres "
    insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
    values ('$1', '$2', 'e2e-metrics-$1@ride.test', 'E2E Metrics', 'active', now(), now());
    insert into staff_member_roles (staff_id, role_id) values ('$1', '$3');" > /dev/null
}

rating_body() { # <rated_by> <stars>
  echo "{\"ratedBy\":\"$1\",\"stars\":$2}"
}

# until_report <path> <python condition over d>: polls a report as the owner
# until the condition holds (events take a moment to arrive).
until_report() {
  for _ in $(seq 1 30); do
    if [ "$(http GET "$1" "$OWNER")" = 200 ] && [ "$(body_field "$2")" = True ]; then
      return 0
    fi
    sleep 1
  done
  return 0
}

PICKUP='{"latitude":31.05,"longitude":31.05}'
DROPOFF='{"latitude":31.07,"longitude":31.07}'

sql ride-analytics-postgres "select to_regclass('public.offer_facts') is not null;" | grep -q t \
  || { echo "ABORT: the offer_facts table does not exist: apply analytics migration 00004 (goose up)" >&2; exit 2; }

buf build -o "$PROTOSET"
RIDER="$(mint "$RIDER_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"
OPS="$(mint "$OPS_IDENTITY")"
TODAY="$(TZ=Asia/Baghdad date +%F)"

echo "==> [0/4] a served zone, a rider, a free driver, the owner and an operator"
add_staff "$OWNER_STAFF_ID" "$OWNER_IDENTITY" "$OWNER_ROLE"
add_staff "$OPS_STAFF_ID" "$OPS_IDENTITY" "$OPERATIONS_ROLE"
expect "a city" 200 POST /v1/admin/cities "$OWNER" "{\"name\":\"$CITY\",\"timeZone\":\"Asia/Baghdad\",\"center\":$PICKUP}"
CITY_ID="$(body_field 'd["city"]["id"]')"
expect "a zone" 200 POST /v1/admin/zones "$OWNER" "{\"cityId\":\"$CITY_ID\",\"name\":\"E2E\",\"boundary\":[{\"latitude\":31,\"longitude\":31},{\"latitude\":31,\"longitude\":31.1},{\"latitude\":31.1,\"longitude\":31.1},{\"latitude\":31.1,\"longitude\":31}]}"
ZONE_ID="$(body_field 'd["zone"]["id"]')"
expect "the city's card" 200 POST /v1/admin/rate-cards "$OWNER" "{\"cityId\":\"$CITY_ID\",\"baseFare\":\"1000\",\"perKmRate\":\"0\",\"perMinuteRate\":\"0\",\"minimumFare\":\"4000\",\"maxSurgePercent\":\"0\"}"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Metrics Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "a driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Metrics Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
sql ride-driver-postgres "update drivers set status = 'active', availability_status = 'available' where id = '$DRIVER_ID';" > /dev/null

echo "==> [1/4] one trip from quote to ratings"
expect "a quote" 200 POST /v1/fare-quotes "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF}"
QUOTE="$(body_field 'next(q["quoteId"] for q in d["quotes"] if q["vehicleClass"] == "economy")')"
expect "the trip" 200 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"quoteId\":\"$QUOTE\"}"
TRIP="$(body_field 'd["trip"]["id"]')"
# Dispatch may already have given it to this driver (the only one near).
internal_call "$TRIP_ADDR" ride.trip.v1.TripService/AcceptTrip "{\"trip_id\":\"$TRIP\",\"driver_id\":\"$DRIVER_ID\"}" > /dev/null 2>&1 || true
report_position 31.0505 31.0505
expect "the driver arrives" 200 POST "/v1/trips/$TRIP:arrived" "$DRIVER" '{}'
expect "the driver starts" 200 POST "/v1/trips/$TRIP:start" "$DRIVER" '{}'
expect "the driver completes" 200 POST "/v1/trips/$TRIP:complete" "$DRIVER" '{}'
expect "the rider gives 4 stars" 200 POST "/v1/trips/$TRIP:rate" "$RIDER" "$(rating_body RATED_BY_RIDER 4)"
expect "the driver gives 5 stars" 200 POST "/v1/trips/$TRIP:rate" "$DRIVER" "$(rating_body RATED_BY_DRIVER 5)"
DISCOUNT=""
for _ in $(seq 1 30); do
  DISCOUNT="$(sql ride-pricing-postgres "select trim(trailing '.' from trim(trailing '0' from discount_amount::text)) from fares where trip_id = '$TRIP';" 2> /dev/null || true)"
  [ -n "$DISCOUNT" ] && break
  sleep 1
done
[ -n "$DISCOUNT" ] || DISCOUNT="none"

echo "    and a trip offered to the driver, who turns it down"
# Offline, after dispatch has freed them from the first trip, so dispatch
# does not hand them the next one before the offer.
sleep 3
sql ride-driver-postgres "update drivers set availability_status = 'offline' where id = '$DRIVER_ID';" > /dev/null
expect "another trip" 200 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF}"
SECOND="$(body_field 'd["trip"]["id"]')"
internal_call "$TRIP_ADDR" ride.trip.v1.TripService/OfferTrip "{\"trip_id\":\"$SECOND\",\"driver_id\":\"$DRIVER_ID\",\"ttl_seconds\":30}" > /dev/null \
  || echo "  (could not offer the trip; the next checks will say why)"
expect "the driver turns it down" 200 POST "/v1/trips/$SECOND:reject-offer" "$DRIVER" "{\"driverId\":\"$DRIVER_ID\"}"
expect "the rider cancels it" 200 POST "/v1/trips/$SECOND:cancel" "$RIDER" '{"reason":"e2e"}'

echo "==> [2/4] the trips in the reports"
SCOPE="scope.city_id=$CITY_ID"
until_report "/v1/analytics/ratings?$SCOPE" 'd["riders"].get("count", "0") == "1" and d["drivers"].get("count", "0") == "1"'
until_report "/v1/analytics/driver-offers?$SCOPE" 'd["totals"].get("rejected", "0") == "1"'
expect "service levels" 200 GET "/v1/analytics/service-levels?$SCOPE" "$OWNER"
check "two requested, one completed, each step measured once, 50%" "2 1 1 1 1 50.00" \
  "$(body_field '" ".join([d["totals"].get("requestedCount", "0"), d["totals"].get("completedCount", "0"), d["totals"]["match"].get("count", "0"), d["totals"]["pickup"].get("count", "0"), d["totals"]["ride"].get("count", "0"), d["completionRatePercent"]])')"
check "on Baghdad's clock, today last" "Asia/Baghdad $TODAY" "$(body_field 'd["timeZone"] + " " + d["days"][-1]["date"]')"
expect "ratings" 200 GET "/v1/analytics/ratings?$SCOPE" "$OWNER"
check "drivers 4.00 (one 4-star), riders 5.00 (one 5-star)" "4.00 0,0,0,1,0 5.00 0,0,0,0,1" \
  "$(body_field '" ".join([d["drivers"]["average"], ",".join(d["drivers"]["byStars"]), d["riders"]["average"], ",".join(d["riders"]["byStars"])])')"
expect "revenue" 200 GET "/v1/analytics/revenue?$SCOPE" "$OWNER"
check "the fare's discount, as pricing has it" "$DISCOUNT" \
  "$(body_field 'format(__import__("decimal").Decimal(d.get("discountTotal", "0")).normalize(), "f") if d.get("totalTrips", "0") == "1" else "no trip"')"
expect "driver offers" 200 GET "/v1/analytics/driver-offers?$SCOPE" "$OWNER"
check "one offer, turned down: 0% accepted, every day listed" "1 1 0 0.00 30" \
  "$(body_field '" ".join([d["totals"].get("offered", "0"), d["totals"].get("rejected", "0"), d["totals"].get("accepted", "0"), d["acceptanceRatePercent"], str(len(d["days"]))])')"

echo "==> [3/4] money and now"
expect "the owner credits the rider 2500" 200 POST "/v1/admin/wallets/$RIDER_ID/adjustments" "$OWNER" \
  "{\"ownerType\":\"OWNER_TYPE_RIDER\",\"amount\":\"2500\",\"reason\":\"e2e analytics\",\"idempotencyKey\":\"e2e-metrics-$RUN\"}"
expect "money flows" 200 GET "/v1/analytics/money-flows?range.from_date=$TODAY&range.to_date=$TODAY" "$OWNER"
check "today's rider adjustments hold the 2500" True \
  "$(body_field 'any(l["ownerType"] == "rider" and l["type"] == "adjustment" and __import__("decimal").Decimal(l["credited"]) >= 2500 for l in d["totals"])')"
check "the headline agrees" True "$(body_field '__import__("decimal").Decimal(d["headline"]["adjustmentsIn"]) >= 2500')"
check "what wallets hold, in the currency" "True IQD Asia/Baghdad" \
  "$(body_field 'str(__import__("decimal").Decimal(d["held"]["riderBalances"]) >= 2500) + " " + d["currency"] + " " + d["timeZone"]')"
expect "live, the city" 200 GET "/v1/analytics/live?$SCOPE" "$OWNER"
check "today two trips, one completed, one cancelled; none under way" "2 1 1 0 0 0" \
  "$(body_field '" ".join([d["today"].get("requestedCount", "0"), d["today"].get("completedCount", "0"), d["today"].get("cancelledCount", "0"), d["trips"].get("waiting", "0"), d["trips"].get("onTheWay", "0"), d["trips"].get("inProgress", "0")])')"
check "drivers counted, today on Baghdad's clock" "True $TODAY" \
  "$(body_field 'str(int(d.get("driversAvailable", "0")) + int(d.get("driversOffline", "0")) + int(d.get("driversBusy", "0")) >= 1) + " " + d["today"]["date"]')"

echo "==> [4/4] who may read them, and bad requests"
for report in service-levels driver-offers ratings money-flows live; do
  expect "an operator, $report" 403 GET "/v1/analytics/$report" "$OPS"
  expect "nobody, $report" 401 GET "/v1/analytics/$report" ""
done
expect "money flows over two years" 400 GET "/v1/analytics/money-flows?range.from_date=2024-01-01&range.to_date=2026-01-01" "$OWNER"
expect "live in an unknown city" 400 GET "/v1/analytics/live?scope.city_id=$UNKNOWN" "$OWNER"
expect "ratings, backwards" 400 GET "/v1/analytics/ratings?range.from_date=2026-10-05&range.to_date=2026-10-01" "$OWNER"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: a real trip shows in service levels, ratings and discounts; a staff credit shows in the money flows with what wallets hold; the live view counts today's trips and the drivers; only analytics.read reads them"
