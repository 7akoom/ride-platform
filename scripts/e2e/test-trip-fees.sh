#!/usr/bin/env bash
# End-to-end test of the driver's arrival, the waiting fee, and the fees of
# cancelled trips (a late cancellation, a rider no-show) down to the wallet,
# on the real services, through the gateway. Run from the ride-platform repo
# root:
#   bash scripts/e2e/test-trip-fees.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken), grpcurl, and the default TRIP_NO_SHOW_WAIT (5m).
#
# What it proves:
#   1. only the trip's driver marks arrival, and only at the pickup; the
#      rider is told; marking again changes nothing
#   2. the wait beyond the card's free minutes is added to the quoted fare
#   3. a rider who cancels after the grace minutes pays the cancellation fee;
#      within them, nothing; the fee comes out of the wallet and what it
#      cannot cover stays owed; the driver earns the fee minus commission
#   4. only the driver reports a no-show, only after arriving and waiting;
#      the rider pays the no-show fee; a driver who just cancels costs the
#      rider nothing
#
# It creates a throw-away city and zone (far from any real one), two riders,
# a driver and a staff member, and removes them again. Waiting is simulated
# by moving the trip's timestamps back.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
TRIP_ADDR="localhost:50055"
WALLET_ADDR="localhost:50058"
PROTOSET="/tmp/ride.binpb"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
DRIVER_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
PLATE="E2E-FEES-$RUN"
CITY="E2E Fees City $RUN"
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
  if [ -n "$CITY_ID" ]; then
    sql ride-pricing-postgres "
      delete from fares where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');
      delete from rider_trip_stats where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');
      delete from fare_quotes where zone_id = '${ZONE_ID:-$UNKNOWN}';
      delete from pricing_configs where city_id = '$CITY_ID';" > /dev/null 2>&1 || true
  fi
  sql ride-location-postgres "
    delete from zones where city_id in (select id from cities where name like 'E2E Fees City%');
    delete from cities where name like 'E2E Fees City%';" > /dev/null 2>&1 || true
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

PICKUP='{"latitude":30.05,"longitude":30.05}'
DROPOFF='{"latitude":30.07,"longitude":30.07}'

# new_trip <token> <rider id> [quote id]: requests a trip and gives it to the
# driver; prints its id.
new_trip() {
  local quote="" status id
  [ -n "${3:-}" ] && quote=",\"quoteId\":\"$3\""
  status="$(http POST /v1/trips "$1" "{\"riderId\":\"$2\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF$quote}")"
  [ "$status" = 200 ] || { echo "FAIL: could not request a trip ($status): $(head -c 200 "$BODY_FILE")" >&2; exit 1; }
  id="$(body_field 'd["trip"]["id"]')"
  # Dispatch may already have given the trip to this driver (the only one near).
  internal_call "$TRIP_ADDR" ride.trip.v1.TripService/AcceptTrip "{\"trip_id\":\"$id\",\"driver_id\":\"$DRIVER_ID\"}" > /dev/null 2>&1 || true
  echo "$id"
}

# fare_of <trip id>: "<kind> <total> <waiting minutes>" once pricing recorded it.
fare_of() {
  local fare=""
  for _ in $(seq 1 30); do
    fare="$(sql ride-pricing-postgres "select kind || ' ' || trim(trailing '.' from trim(trailing '0' from total::text)) || ' ' || waiting_minutes from fares where trip_id = '$1';" 2> /dev/null || true)"
    [ -n "$fare" ] && break
    sleep 1
  done
  echo "$fare"
}

# settlement_of <owner id> <OWNER_TYPE_...> <trip id> <token>: waits for it.
settlement_of() {
  for _ in $(seq 1 30); do
    [ "$(http GET "/v1/wallets/$1/trips/$3/settlement?owner_type=$2" "$4")" = 200 ] && return 0
    sleep 1
  done
  return 1
}

# notified <rider id> <event key>: True once the rider has that notification.
notified() {
  for _ in $(seq 1 20); do
    if [ "$(http GET "/v1/notifications?recipient_type=RECIPIENT_TYPE_RIDER&recipient_id=$1&limit=50" "$2")" = 200 ] \
      && [ "$(body_field "any(n['eventKey'] == '$3' for n in d.get('notifications', []))")" = True ]; then
      echo True
      return
    fi
    sleep 1
  done
  echo False
}

buf build -o "$PROTOSET"
RIDER="$(mint "$RIDER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"

echo "==> [0/4] a served zone with fees, two riders, a free driver"
sql ride-staff-postgres "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
  values ('$OWNER_STAFF_ID', '$OWNER_IDENTITY', 'e2e-fees-$RUN@ride.test', 'E2E Fees', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values ('$OWNER_STAFF_ID', '$OWNER_ROLE');" > /dev/null
expect "a city" 200 POST /v1/admin/cities "$OWNER" "{\"name\":\"$CITY\",\"timeZone\":\"Asia/Baghdad\",\"center\":$PICKUP}"
CITY_ID="$(body_field 'd["city"]["id"]')"
expect "a zone" 200 POST /v1/admin/zones "$OWNER" "{\"cityId\":\"$CITY_ID\",\"name\":\"E2E\",\"boundary\":[{\"latitude\":30,\"longitude\":30},{\"latitude\":30,\"longitude\":30.1},{\"latitude\":30.1,\"longitude\":30.1},{\"latitude\":30.1,\"longitude\":30}]}"
ZONE_ID="$(body_field 'd["zone"]["id"]')"
# Rates are zero so a fare is the minimum; surge is off so it does not move.
expect "the city's card with fees" 200 POST /v1/admin/rate-cards "$OWNER" "{\"cityId\":\"$CITY_ID\",\"baseFare\":\"1000\",\"perKmRate\":\"0\",\"perMinuteRate\":\"0\",\"minimumFare\":\"3000\",\"freeWaitingMinutes\":3,\"waitingPerMinute\":\"100\",\"cancellationFee\":\"1000\",\"cancellationGraceMinutes\":2,\"noShowFee\":\"2000\",\"maxSurgePercent\":\"0\"}"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Fees Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "another rider" 200 POST /v1/riders "$OTHER" "{\"identityId\":\"$OTHER_IDENTITY\",\"displayName\":\"E2E Other Rider\"}"
OTHER_ID="$(body_field 'd["rider"]["id"]')"
expect "a driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Fees Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
sql ride-driver-postgres "update drivers set status = 'active', availability_status = 'available' where id = '$DRIVER_ID';" > /dev/null

echo "==> [1/4] arriving, and paying for the wait"
expect "a quote" 200 POST /v1/fare-quotes "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF}"
QUOTE="$(body_field 'next(q["quoteId"] for q in d["quotes"] if q["vehicleClass"] == "economy")')"
QUOTED="$(body_field 'next(q["fare"]["total"] for q in d["quotes"] if q["vehicleClass"] == "economy")')"
TRIP="$(new_trip "$RIDER" "$RIDER_ID" "$QUOTE")"
report_position 30.09 30.09
expect "the driver, far from the pickup" 400 POST "/v1/trips/$TRIP:arrived" "$DRIVER" '{}'
report_position 30.0505 30.0505
expect "the rider marks the driver's arrival" 403 POST "/v1/trips/$TRIP:arrived" "$RIDER" '{}'
expect "the driver, at the pickup" 200 POST "/v1/trips/$TRIP:arrived" "$DRIVER" '{}'
ARRIVED="$(body_field 'd["trip"]["arrivedAt"]')"
expect "again" 200 POST "/v1/trips/$TRIP:arrived" "$DRIVER" '{}'
check "changes nothing" "$ARRIVED" "$(body_field 'd["trip"]["arrivedAt"]')"
check "the rider is told" True "$(notified "$RIDER_ID" "$RIDER" trip.driver_arrived)"
# Eight minutes at the pickup: five beyond the three free ones.
sql ride-trip-postgres "update trips set arrived_at = now() - interval '8 minutes' where id = '$TRIP';" > /dev/null
expect "the driver starts" 200 POST "/v1/trips/$TRIP:start" "$DRIVER" '{}'
expect "the driver completes" 200 POST "/v1/trips/$TRIP:complete" "$DRIVER" '{}'
check "the quote plus five minutes of waiting" "trip $(python3 -c "print(int(float('$QUOTED')) + 500)") 5" "$(fare_of "$TRIP")"

echo "==> [2/4] a rider cancels late, and in time"
TRIP="$(new_trip "$RIDER" "$RIDER_ID")"
sql ride-trip-postgres "update trips set accepted_at = now() - interval '5 minutes' where id = '$TRIP';" > /dev/null
expect "the rider cancels five minutes after acceptance" 200 POST "/v1/trips/$TRIP:cancel" "$RIDER" '{"reason":"changed plans"}'
check "cancelled by the rider" "rider False" "$(body_field 'd["trip"]["cancelledBy"] + " " + str(d["trip"].get("riderNoShow", False))')"
check "the cancellation fee" "cancellation 1000 0" "$(fare_of "$TRIP")"
settlement_of "$RIDER_ID" OWNER_TYPE_RIDER "$TRIP" "$RIDER" || true
check "from an empty wallet: all of it owed" "cancellation 0 1000" "$(body_field 'd.get("kind", "") + " " + d.get("walletAmount", "") + " " + d.get("dueAmount", "")')"
expect "the driver's view" 200 GET "/v1/wallets/$DRIVER_ID/trips/$TRIP/settlement?owner_type=OWNER_TYPE_DRIVER" "$DRIVER"
check "the driver earns the fee minus commission" 800 "$(body_field 'd["driverEarning"]')"
check "the rider is told" True "$(notified "$RIDER_ID" "$RIDER" trip.cancellation_fee)"

TRIP="$(new_trip "$RIDER" "$RIDER_ID")"
expect "the rider cancels right away" 200 POST "/v1/trips/$TRIP:cancel" "$RIDER" '{}'
sleep 3
check "no fee within the grace minutes" "" "$(sql ride-pricing-postgres "select kind from fares where trip_id = '$TRIP';")"

echo "==> [3/4] the rider does not come"
internal_call "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$OTHER_ID\",\"amount\":\"1500\",\"idempotency_key\":\"e2e-fees-$RUN\"}" > /dev/null
TRIP="$(new_trip "$OTHER" "$OTHER_ID")"
expect "the rider reports their own no-show" 403 POST "/v1/trips/$TRIP:cancel" "$OTHER" '{"riderNoShow":true}'
expect "the driver, before arriving" 400 POST "/v1/trips/$TRIP:cancel" "$DRIVER" '{"riderNoShow":true}'
report_position 30.0505 30.0505
expect "the driver arrives" 200 POST "/v1/trips/$TRIP:arrived" "$DRIVER" '{}'
expect "the driver, right after arriving" 400 POST "/v1/trips/$TRIP:cancel" "$DRIVER" '{"riderNoShow":true}'
sql ride-trip-postgres "update trips set arrived_at = now() - interval '6 minutes' where id = '$TRIP';" > /dev/null
expect "the driver, after waiting" 200 POST "/v1/trips/$TRIP:cancel" "$DRIVER" '{"riderNoShow":true,"reason":"rider did not come"}'
check "a no-show, by the driver" "driver True" "$(body_field 'd["trip"]["cancelledBy"] + " " + str(d["trip"]["riderNoShow"])')"
check "the no-show fee" "no_show 2000 0" "$(fare_of "$TRIP")"
settlement_of "$OTHER_ID" OWNER_TYPE_RIDER "$TRIP" "$OTHER" || true
check "the wallet pays what it holds, the rest is owed" "no_show 1500 500" "$(body_field 'd.get("kind", "") + " " + d.get("walletAmount", "") + " " + d.get("dueAmount", "")')"
expect "the rider's wallet" 200 GET "/v1/wallets/$OTHER_ID?owner_type=OWNER_TYPE_RIDER" "$OTHER"
check "is empty, never negative" 0 "$(body_field 'd["wallet"]["balance"]')"
check "the rider is told" True "$(notified "$OTHER_ID" "$OTHER" trip.no_show_fee)"

echo "==> [4/4] a driver who just cancels"
TRIP="$(new_trip "$OTHER" "$OTHER_ID")"
sql ride-trip-postgres "update trips set accepted_at = now() - interval '5 minutes' where id = '$TRIP';" > /dev/null
expect "the driver cancels" 200 POST "/v1/trips/$TRIP:cancel" "$DRIVER" '{"reason":"car trouble"}'
check "cancelled by the driver" "driver False" "$(body_field 'd["trip"]["cancelledBy"] + " " + str(d["trip"].get("riderNoShow", False))')"
sleep 3
check "costs the rider nothing" "" "$(sql ride-pricing-postgres "select kind from fares where trip_id = '$TRIP';")"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: arrival is checked, waiting is charged, and late cancellations and no-shows cost the rider a fee"
