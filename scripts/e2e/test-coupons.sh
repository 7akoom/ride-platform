#!/usr/bin/env bash
# End-to-end test of coupons and the automatic discounts on the real
# services, through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-coupons.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken).
#
# What it proves:
#   1. coupons are staff business: promotions.manage (not the operations
#      role) creates them; bad codes, values, places and caps are refused
#   2. a rider's code is checked per class and each quote says what became
#      of it: applied (a percentage with its cap, a fixed amount), not for
#      this class, below its minimum, unknown
#   3. a trip requested with a quote holds the coupon's use while it runs:
#      the last use is not offered to anyone else; cancelling frees it; a
#      completed trip keeps it and pays the discounted price; the use log
#      shows it all
#   4. staff change a coupon's limits and turn it off; a coupon for new
#      riders only; one use per rider
#   5. the first-ride and loyalty discounts follow the staff's settings, with
#      their caps, and a larger one wins over a coupon (never both)
#
# It creates a throw-away city and zone (far from any real one), three
# riders, a driver and two staff members, and removes them again; the
# promotion settings are put back as they were.
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
SECOND_IDENTITY="$(uuid)"
DRIVER_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OPERATOR_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
OPERATOR_STAFF_ID="$(uuid)"
PLATE="E2E-CPN-$RUN"
CITY="E2E Coupon City $RUN"
PREFIX="E2E-$RUN"
UNKNOWN="$(uuid)"
UNTIL="$(python3 -c 'import datetime; print((datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(days=1)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
PAST="$(python3 -c 'import datetime; print((datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(days=1)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"

INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' services/trip-service/.env | tr -d '"')"
[ -n "$INTERNAL_TOKEN" ] || { echo "ABORT: INTERNAL_SERVICE_TOKEN is missing from services/trip-service/.env" >&2; exit 2; }

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
SECOND_ID=""
CITY_ID=""
ZONE_ID=""
DRIVER_ID=""
SETTINGS=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  if [ -n "$SETTINGS" ]; then
    sql ride-pricing-postgres "update promotion_settings set $SETTINGS;" > /dev/null 2>&1 || true
  fi
  sql ride-staff-postgres "delete from staff_members where id in ('$OWNER_STAFF_ID', '$OPERATOR_STAFF_ID');" > /dev/null 2>&1 || true
  # Quotes and fares refer to the coupons and cards: they go first.
  sql ride-pricing-postgres "
    delete from fares where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${SECOND_ID:-$UNKNOWN}');
    delete from rider_trip_stats where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${SECOND_ID:-$UNKNOWN}');
    delete from fare_quotes where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${SECOND_ID:-$UNKNOWN}');
    delete from coupon_redemptions where coupon_id in (select id from coupons where code like '$PREFIX-%');
    delete from coupons where code like '$PREFIX-%';
    delete from pricing_configs where city_id = '${CITY_ID:-$UNKNOWN}';" > /dev/null 2>&1 || true
  sql ride-location-postgres "
    delete from zones where city_id in (select id from cities where name like 'E2E Coupon City%');
    delete from cities where name like 'E2E Coupon City%';" > /dev/null 2>&1 || true
  sql ride-driver-postgres "delete from drivers where vehicle_plate_number = '$PLATE';" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID$SECOND_ID" ]; then
    sql ride-rider-postgres "delete from riders where id in ('${RIDER_ID:-$UNKNOWN}', '${SECOND_ID:-$UNKNOWN}');" > /dev/null 2>&1 || true
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
    values ('$1', '$2', 'e2e-coupon-$1@ride.test', 'E2E Coupon', 'active', now(), now());
    insert into staff_member_roles (staff_id, role_id) values ('$1', '$3');" > /dev/null
}

# A card for the city: rates are zero so a fare is its base, and no surge.
card() { # <class> <base>
  echo "{\"cityId\":\"$CITY_ID\",\"vehicleClass\":\"$1\",\"baseFare\":\"$2\",\"perKmRate\":\"0\",\"perMinuteRate\":\"0\",\"maxSurgePercent\":\"0\",\"demandSurge\":false,\"weatherSurge\":false}"
}

settings() { # <first ride %> <first ride cap> <loyalty every> <loyalty %> <loyalty cap>
  echo "{\"firstRidePercent\":\"$1\",\"firstRideMaxAmount\":\"$2\",\"loyaltyEvery\":$3,\"loyaltyPercent\":\"$4\",\"loyaltyMaxAmount\":\"$5\"}"
}

PICKUP='{"latitude":20.05,"longitude":20.05}'
DROPOFF='{"latitude":20.07,"longitude":20.07}'
quote_body() { # <rider id> [code]
  echo "{\"riderId\":\"$1\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"couponCode\":\"${2:-}\"}"
}

# class_field <class> <python expression over q>: a field of that class's quote.
class_field() {
  body_field "next(($2) for q in d[\"quotes\"] if q[\"vehicleClass\"] == \"$1\")"
}

# What the class's quote says: coupon status, discount, total.
QUOTE_SUMMARY='q["fare"].get("couponStatus", "NONE").replace("COUPON_STATUS_", "") + " " + q["fare"]["discountAmount"] + " " + q["fare"]["total"]'

quote() { # <label> <token> <rider id> [code]
  expect "$1" 200 POST /v1/fare-quotes "$2" "$(quote_body "$3" "${4:-}")"
}

redemption_status() { # <code> <trip id>: that trip's hold on the coupon
  http GET "/v1/admin/coupons/$1/redemptions" "$OWNER" > /dev/null
  body_field "next((r[\"status\"] for r in d.get(\"redemptions\", []) if r.get(\"tripId\") == \"$2\"), \"none\")"
}

wait_for_redemption() { # <code> <trip id> <status>
  local got=""
  for _ in $(seq 1 30); do
    got="$(redemption_status "$1" "$2")"
    [ "$got" = "$3" ] && break
    sleep 1
  done
  echo "$got"
}

buf build -o "$PROTOSET"
RIDER="$(mint "$RIDER_IDENTITY")"
SECOND="$(mint "$SECOND_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"
OPERATOR="$(mint "$OPERATOR_IDENTITY")"

SETTINGS="$(sql ride-pricing-postgres "select format('first_ride_percent = %s, first_ride_max_amount = %s, loyalty_every = %s, loyalty_percent = %s, loyalty_max_amount = %s, updated_by = %s, updated_at = %L', first_ride_percent, coalesce(first_ride_max_amount::text, 'null'), loyalty_every, loyalty_percent, coalesce(loyalty_max_amount::text, 'null'), coalesce(quote_literal(updated_by::text), 'null'), updated_at) from promotion_settings")"
[ -n "$SETTINGS" ] || { echo "ABORT: promotion_settings is empty (is pricing migration 00014 applied?)" >&2; exit 2; }

echo "==> [0/5] a served zone with prices, two riders, a driver, two staff members"
add_staff "$OWNER_STAFF_ID" "$OWNER_IDENTITY" "$OWNER_ROLE"
add_staff "$OPERATOR_STAFF_ID" "$OPERATOR_IDENTITY" "$OPERATIONS_ROLE"
expect "a city" 200 POST /v1/admin/cities "$OPERATOR" "{\"name\":\"$CITY\",\"timeZone\":\"Asia/Baghdad\",\"center\":{\"latitude\":20.05,\"longitude\":20.05}}"
CITY_ID="$(body_field 'd["city"]["id"]')"
expect "a zone" 200 POST /v1/admin/zones "$OPERATOR" "{\"cityId\":\"$CITY_ID\",\"name\":\"E2E\",\"boundary\":[{\"latitude\":20,\"longitude\":20},{\"latitude\":20,\"longitude\":20.1},{\"latitude\":20.1,\"longitude\":20.1},{\"latitude\":20.1,\"longitude\":20}]}"
ZONE_ID="$(body_field 'd["zone"]["id"]')"
expect "economy costs 5000" 200 POST /v1/admin/rate-cards "$OWNER" "$(card economy 5000)"
expect "comfort costs 8000" 200 POST /v1/admin/rate-cards "$OWNER" "$(card comfort 8000)"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Coupon Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "a second rider" 200 POST /v1/riders "$SECOND" "{\"identityId\":\"$SECOND_IDENTITY\",\"displayName\":\"E2E Coupon Second\"}"
SECOND_ID="$(body_field 'd["rider"]["id"]')"
expect "a driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Coupon Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
sql ride-driver-postgres "update drivers set status = 'active', availability_status = 'available' where id = '$DRIVER_ID';" > /dev/null
expect "no automatic discounts for now" 200 PUT /v1/admin/promotion-settings "$OWNER" "$(settings 0 "" 0 0 "")"

echo "==> [1/5] coupons are staff business"
ONE="$PREFIX-ONE"
COUPON="{\"code\":\"$(echo "$ONE" | tr '[:upper:]' '[:lower:]')\",\"description\":\"E2E\",\"discountType\":\"DISCOUNT_TYPE_PERCENTAGE\",\"discountValue\":\"20\",\"maxDiscountAmount\":\"750\",\"validUntil\":\"$UNTIL\",\"maxRedemptions\":1,\"cityId\":\"$CITY_ID\",\"vehicleClasses\":[\"economy\"]}"
expect "the operations role makes a coupon" 403 POST /v1/admin/coupons "$OPERATOR" "$COUPON"
expect "a rider lists coupons" 403 GET /v1/admin/coupons "$RIDER"
expect "a code with a space" 400 POST /v1/admin/coupons "$OWNER" "{\"code\":\"E2E $RUN\",\"discountType\":\"DISCOUNT_TYPE_FIXED_AMOUNT\",\"discountValue\":\"500\",\"validUntil\":\"$UNTIL\"}"
expect "150 percent" 400 POST /v1/admin/coupons "$OWNER" "{\"code\":\"$PREFIX-BAD\",\"discountType\":\"DISCOUNT_TYPE_PERCENTAGE\",\"discountValue\":\"150\",\"validUntil\":\"$UNTIL\"}"
expect "a cap on a fixed amount" 400 POST /v1/admin/coupons "$OWNER" "{\"code\":\"$PREFIX-BAD\",\"discountType\":\"DISCOUNT_TYPE_FIXED_AMOUNT\",\"discountValue\":\"500\",\"maxDiscountAmount\":\"400\",\"validUntil\":\"$UNTIL\"}"
expect "already over" 400 POST /v1/admin/coupons "$OWNER" "{\"code\":\"$PREFIX-BAD\",\"discountType\":\"DISCOUNT_TYPE_FIXED_AMOUNT\",\"discountValue\":\"500\",\"validFrom\":\"$PAST\",\"validUntil\":\"$PAST\"}"
expect "a city that does not exist" 404 POST /v1/admin/coupons "$OWNER" "{\"code\":\"$PREFIX-BAD\",\"discountType\":\"DISCOUNT_TYPE_FIXED_AMOUNT\",\"discountValue\":\"500\",\"validUntil\":\"$UNTIL\",\"cityId\":\"$UNKNOWN\"}"
expect "a zone and a city" 400 POST /v1/admin/coupons "$OWNER" "{\"code\":\"$PREFIX-BAD\",\"discountType\":\"DISCOUNT_TYPE_FIXED_AMOUNT\",\"discountValue\":\"500\",\"validUntil\":\"$UNTIL\",\"cityId\":\"$CITY_ID\",\"zoneId\":\"$ZONE_ID\"}"
expect "20% off economy in the city, at most 750, once" 200 POST /v1/admin/coupons "$OWNER" "$COUPON"
check "stored upper-case, running, by the owner" "$ONE running $OWNER_IDENTITY" "$(body_field '" ".join((d["coupon"]["code"], d["coupon"]["state"], d["coupon"]["createdBy"]))')"
expect "the same code again" 409 POST /v1/admin/coupons "$OWNER" "$COUPON"
expect "1000 off above 6000" 200 POST /v1/admin/coupons "$OWNER" "{\"code\":\"$PREFIX-MIN\",\"discountType\":\"DISCOUNT_TYPE_FIXED_AMOUNT\",\"discountValue\":\"1000\",\"minimumFareAmount\":\"6000\",\"validUntil\":\"$UNTIL\",\"perRiderLimit\":5}"
expect "the running ones" 200 GET "/v1/admin/coupons?state=running&query=$PREFIX" "$OWNER"
check "both" "$PREFIX-MIN $ONE" "$(body_field '" ".join(c["code"] for c in d["coupons"])')"

echo "==> [2/5] a rider enters a code"
quote "the rider's quotes with the code" "$RIDER" "$RIDER_ID" "$(echo "$ONE" | tr '[:upper:]' '[:lower:]')"
check "economy: 20% capped at 750" "APPLIED 750 4250" "$(class_field economy "$QUOTE_SUMMARY")"
check "comfort: not for this class" "NOT_FOR_CLASS 0 8000" "$(class_field comfort "$QUOTE_SUMMARY")"
ONE_QUOTE="$(class_field economy 'q["quoteId"]')"
quote "with the minimum-fare coupon" "$RIDER" "$RIDER_ID" "$PREFIX-MIN"
check "economy: below its minimum" "BELOW_MINIMUM 0 5000" "$(class_field economy "$QUOTE_SUMMARY")"
check "comfort: 1000 off" "APPLIED 1000 7000" "$(class_field comfort "$QUOTE_SUMMARY")"
quote "with a code nobody made" "$RIDER" "$RIDER_ID" "$PREFIX-NOPE"
check "unknown, full price" "NOT_FOUND 0 5000" "$(class_field economy "$QUOTE_SUMMARY")"

echo "==> [3/5] a trip holds the coupon's use"
TRIP_BODY="\"pickup\":$PICKUP,\"dropoff\":$DROPOFF"
expect "the rider's trip with the quote" 200 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",$TRIP_BODY,\"quoteId\":\"$ONE_QUOTE\"}"
FIRST_TRIP="$(body_field 'd["trip"]["id"]')"
check "at the discounted price" 4250 "$(body_field 'd["trip"]["quotedFare"]')"
check "the use is reserved" reserved "$(redemption_status "$ONE" "$FIRST_TRIP")"
expect "the coupon" 200 GET "/v1/admin/coupons/$(echo "$ONE" | tr '[:upper:]' '[:lower:]')" "$OWNER"
check "its one use is held" "1 used_up" "$(body_field 'str(d["coupon"]["redemptionCount"]) + " " + d["coupon"]["state"]')"
quote "the second rider's quotes" "$SECOND" "$SECOND_ID" "$ONE"
check "nothing left for them" "USED_UP 0 5000" "$(class_field economy "$QUOTE_SUMMARY")"
expect "the rider cancels" 200 POST "/v1/trips/$FIRST_TRIP:cancel" "$RIDER" '{"reason":"e2e"}'
check "the use is freed" released "$(wait_for_redemption "$ONE" "$FIRST_TRIP" released)"
quote "the second rider again" "$SECOND" "$SECOND_ID" "$ONE"
check "now it applies" "APPLIED 750 4250" "$(class_field economy "$QUOTE_SUMMARY")"
SECOND_QUOTE="$(class_field economy 'q["quoteId"]')"
expect "their trip" 200 POST /v1/trips "$SECOND" "{\"riderId\":\"$SECOND_ID\",$TRIP_BODY,\"quoteId\":\"$SECOND_QUOTE\"}"
SECOND_TRIP="$(body_field 'd["trip"]["id"]')"
# Dispatch may already have given the trip to this driver (the only one).
grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" \
  -d "{\"trip_id\":\"$SECOND_TRIP\",\"driver_id\":\"$DRIVER_ID\"}" "$TRIP_ADDR" ride.trip.v1.TripService/AcceptTrip > /dev/null 2>&1 || true
expect "the driver starts" 200 POST "/v1/trips/$SECOND_TRIP:start" "$DRIVER" '{}'
expect "the driver completes" 200 POST "/v1/trips/$SECOND_TRIP:complete" "$DRIVER" '{}'
check "the use is redeemed" redeemed "$(wait_for_redemption "$ONE" "$SECOND_TRIP" redeemed)"
check "the fare is the discounted one" "4250 750 Coupon: $ONE" "$(sql ride-pricing-postgres "select trim(trailing '.' from trim(trailing '0' from total::text)) || ' ' || trim(trailing '.' from trim(trailing '0' from discount_amount::text)) || ' ' || applied_discount_label from fares where trip_id = '$SECOND_TRIP';")"
expect "the coupon" 200 GET "/v1/admin/coupons/$ONE" "$OWNER"
check "one completed trip used it" "1 1 750" "$(body_field '" ".join(str(x) for x in (d["coupon"]["redemptionCount"], d["coupon"]["redeemedCount"], d["coupon"]["discountGiven"]))')"
expect "its use" 200 GET "/v1/admin/coupons/$ONE/redemptions" "$OWNER"
check "redeemed, then released" "redeemed released" "$(body_field '" ".join(r["status"] for r in d["redemptions"])')"
quote "the first rider again" "$RIDER" "$RIDER_ID" "$ONE"
check "every use is taken" "USED_UP 0 5000" "$(class_field economy "$QUOTE_SUMMARY")"

echo "==> [4/5] staff change a coupon"
expect "no limit on uses" 200 PATCH "/v1/admin/coupons/$ONE" "$OWNER" '{"maxRedemptions":0}'
check "running again" "0 running" "$(body_field 'str(d["coupon"].get("maxRedemptions", 0)) + " " + d["coupon"]["state"]')"
quote "the first rider" "$RIDER" "$RIDER_ID" "$ONE"
check "it applies (their use was freed)" "APPLIED 750 4250" "$(class_field economy "$QUOTE_SUMMARY")"
quote "the second rider" "$SECOND" "$SECOND_ID" "$ONE"
check "once per rider" "ALREADY_USED 0 5000" "$(class_field economy "$QUOTE_SUMMARY")"
expect "an end before its start" 400 PATCH "/v1/admin/coupons/$ONE" "$OWNER" "{\"validUntil\":\"$PAST\"}"
expect "a coupon nobody made" 404 PATCH "/v1/admin/coupons/$PREFIX-NOPE" "$OWNER" '{"active":false}'
expect "turned off" 200 PATCH "/v1/admin/coupons/$ONE" "$OWNER" '{"active":false}'
check "ended, by the owner" "ended $OWNER_IDENTITY" "$(body_field 'd["coupon"]["state"] + " " + d["coupon"]["updatedBy"]')"
quote "the first rider" "$RIDER" "$RIDER_ID" "$ONE"
check "it no longer applies" "ENDED 0 5000" "$(class_field economy "$QUOTE_SUMMARY")"
expect "500 off for new riders" 200 POST /v1/admin/coupons "$OWNER" "{\"code\":\"$PREFIX-NEW\",\"discountType\":\"DISCOUNT_TYPE_FIXED_AMOUNT\",\"discountValue\":\"500\",\"validUntil\":\"$UNTIL\",\"newRidersOnly\":true}"
quote "a rider who never completed a trip" "$RIDER" "$RIDER_ID" "$PREFIX-NEW"
check "gets it" "APPLIED 500 4500" "$(class_field economy "$QUOTE_SUMMARY")"
quote "a rider who did" "$SECOND" "$SECOND_ID" "$PREFIX-NEW"
check "does not" "NEW_RIDERS_ONLY 0 5000" "$(class_field economy "$QUOTE_SUMMARY")"
expect "the finished ones" 200 GET "/v1/admin/coupons?state=finished&query=$PREFIX" "$OWNER"
check "the one turned off" "$ONE" "$(body_field '" ".join(c["code"] for c in d["coupons"])')"

echo "==> [5/5] the first-ride and loyalty discounts"
expect "the operations role reads them" 403 GET /v1/admin/promotion-settings "$OPERATOR"
expect "every trip a loyalty one" 400 PUT /v1/admin/promotion-settings "$OWNER" "$(settings 50 "" 1 20 "")"
expect "50% off a first ride (at most 1000), 20% off every 2nd" 200 PUT /v1/admin/promotion-settings "$OWNER" "$(settings 50 1000 2 20 "")"
check "saved, by the owner" "50 1000 2 $OWNER_IDENTITY" "$(body_field '" ".join(str(x) for x in (d["settings"]["firstRidePercent"], d["settings"]["firstRideMaxAmount"], d["settings"]["loyaltyEvery"], d["settings"]["updatedBy"]))')"
quote "a first ride" "$RIDER" "$RIDER_ID"
check "half off, capped" "1000 4000 First ride discount" "$(class_field economy 'q["fare"]["discountAmount"] + " " + q["fare"]["total"] + " " + q["fare"]["appliedDiscountLabel"]')"
quote "a first ride with a smaller coupon" "$RIDER" "$RIDER_ID" "$PREFIX-NEW"
check "the larger discount wins, never both" "BETTER_DISCOUNT 1000 4000" "$(class_field economy "$QUOTE_SUMMARY")"
quote "the second rider's second trip" "$SECOND" "$SECOND_ID"
check "20% off" "1000 4000 Loyalty discount" "$(class_field economy 'q["fare"]["discountAmount"] + " " + q["fare"]["total"] + " " + q["fare"]["appliedDiscountLabel"]')"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: staff run coupons and discounts, a quote says what became of a code, and a trip holds a coupon's use until it completes or is cancelled"
