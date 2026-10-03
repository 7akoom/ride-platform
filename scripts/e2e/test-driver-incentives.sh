#!/usr/bin/env bash
# End-to-end test of drivers' incentive campaigns and earnings, on the real
# services, through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-driver-incentives.sh
#
# Needs the local identity signing key, grpcurl, buf, trip-service with
# migration 00017, wallet-service with migration 00017 (and its incentive
# worker running), notification-service with migration 00013 and
# staff-service knowing incentives.manage.
#
# What it proves:
#   1. only the owner creates campaigns (incentives.manage); bad ones are
#      refused; a retried key returns the same campaign, reused for another
#      is refused
#   2. a driver sees their progress (trips in the scope only, tier reached
#      and next, unmet conditions); nobody else sees it
#   3. once the campaign has ended (moved back in time here) the worker pays
#      each driver once: the bonus of the tier reached, or not_eligible with
#      the reason; the driver is told
#   4. a campaign that has not ended can be cancelled, with a reason
#   5. the driver's earnings for the day and the week: trips, commission,
#      cash and the incentive
#
# Trips and offers are written straight into trip-service's database (a
# completed trip needs the whole dispatch flow otherwise); everything it
# creates is removed again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
WALLET_ADDR="localhost:50058"
PROTOSET="/tmp/ride.binpb"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
A_IDENTITY="$(uuid)"
B_IDENTITY="$(uuid)"
C_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OPERATOR_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
OPERATOR_STAFF_ID="$(uuid)"
ZONE="$(uuid)"
OTHER_ZONE="$(uuid)"
SETTLED_TRIP="$(uuid)"
UNKNOWN="$(uuid)"
PLATE="E2E-INC-$RUN"

INTERNAL_TOKEN=""
for env_file in services/wallet-service/.env services/trip-service/.env; do
  [ -z "$INTERNAL_TOKEN" ] && [ -f "$env_file" ] && INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' "$env_file" | head -1 | tr -d '"')"
done
INTERNAL_TOKEN="${INTERNAL_TOKEN:-dev-internal-service-token-change-me}"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
A_ID=""
B_ID=""
C_ID=""
CAMPAIGN=""
FUTURE=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  local drivers="'${A_ID:-$UNKNOWN}', '${B_ID:-$UNKNOWN}', '${C_ID:-$UNKNOWN}'"
  local campaigns="'${CAMPAIGN:-$UNKNOWN}', '${FUTURE:-$UNKNOWN}'"
  sql ride-staff-postgres "delete from staff_members where id in ('$OWNER_STAFF_ID', '$OPERATOR_STAFF_ID');" > /dev/null 2>&1 || true
  sql ride-trip-postgres "
    delete from trip_offers where driver_id in ($drivers);
    delete from trips where driver_id in ($drivers);" > /dev/null 2>&1 || true
  sql ride-wallet-postgres "
    delete from incentive_payouts where campaign_id in ($campaigns);
    delete from incentive_campaigns where id in ($campaigns);
    delete from trip_settlements where trip_id = '$SETTLED_TRIP';
    delete from wallet_transactions where wallet_id in (select id from wallets where owner_id in ($drivers));
    delete from wallets where owner_id in ($drivers);" > /dev/null 2>&1 || true
  sql ride-notification-postgres "delete from notifications where recipient_id in ($drivers);" > /dev/null 2>&1 || true
  sql ride-driver-postgres "delete from drivers where id in ($drivers);" > /dev/null 2>&1 || true
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
    values ('$1', '$2', 'e2e-inc-$1@ride.test', 'E2E Incentives', 'active', now(), now());
    insert into staff_member_roles (staff_id, role_id) values ('$1', '$3');" > /dev/null
}

add_driver() { # <identity> <token> <plate suffix> -> driver id
  http POST /v1/drivers "$2" "{\"identityId\":\"$1\",\"displayName\":\"E2E Incentives $3\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE-$3\",\"vehicleClass\":\"economy\",\"year\":2020}}" > /dev/null
  body_field 'd["driver"]["id"]'
}

trips() { # <driver> <count> <zone> [status]
  local i
  for ((i = 0; i < $2; i++)); do
    sql ride-trip-postgres "
      insert into trips (id, rider_id, driver_id, status, pickup_latitude, pickup_longitude, dropoff_latitude,
                         dropoff_longitude, vehicle_class, pickup_zone_id, completed_at)
      values (gen_random_uuid(), gen_random_uuid(), '$1', 'completed', 36.19, 44.01, 36.2, 44.02, 'economy', '$3', now());" > /dev/null
  done
}

offers() { # <driver> <accepted> <rejected>
  local i status
  for ((i = 0; i < $2 + $3; i++)); do
    status=accepted
    [ "$i" -ge "$2" ] && status=rejected
    sql ride-trip-postgres "
      with t as (
        insert into trips (id, rider_id, status, pickup_latitude, pickup_longitude, dropoff_latitude, dropoff_longitude,
                           vehicle_class, pickup_zone_id)
        values (gen_random_uuid(), gen_random_uuid(), 'cancelled', 36.19, 44.01, 36.2, 44.02, 'economy', '$ZONE')
        returning id)
      insert into trip_offers (trip_id, driver_id, status, offered_at, expires_at)
      select id, '$1', '$status', now(), now() + interval '15 seconds' from t;" > /dev/null
  done
}

campaign() { # <name> <starts> <ends> <tiers json> <key> [extra json]
  echo "{\"name\":\"$1\",\"startsAt\":\"$2\",\"endsAt\":\"$3\",\"zoneIds\":[\"$ZONE\"],\"vehicleClass\":\"economy\",\"minAcceptanceRate\":\"70\",\"minRating\":\"4.5\",\"tiers\":$4,\"idempotencyKey\":\"$5\"${6:-}}"
}

at() { date -u -d "$1" +%Y-%m-%dT%H:%M:%SZ; }

progress() { # <driver id> <token> <python over the campaign's view i>
  http GET "/v1/drivers/$1/incentives" "$2" > /dev/null
  body_field "next(($3) for i in d.get('incentives', []) if i['campaign']['id'] == '$CAMPAIGN')"
}

[ "$(sql ride-wallet-postgres "select to_regclass('public.incentive_campaigns') is not null")" = t ] \
  || { echo "ABORT: the incentive tables do not exist: apply wallet-service migration 00017 (goose up)" >&2; exit 2; }
[ "$(sql ride-trip-postgres "select count(*) from information_schema.columns where table_name = 'trips' and column_name = 'pickup_zone_id'")" = 1 ] \
  || { echo "ABORT: trips have no pickup zone: apply trip-service migration 00017 (goose up)" >&2; exit 2; }

buf build -o "$PROTOSET"
A="$(mint "$A_IDENTITY")"
B="$(mint "$B_IDENTITY")"
C="$(mint "$C_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"
OPERATOR="$(mint "$OPERATOR_IDENTITY")"

echo "==> [0/5] three drivers, an owner and an operator"
add_staff "$OWNER_STAFF_ID" "$OWNER_IDENTITY" "$OWNER_ROLE"
add_staff "$OPERATOR_STAFF_ID" "$OPERATOR_IDENTITY" "$OPERATIONS_ROLE"
A_ID="$(add_driver "$A_IDENTITY" "$A" A)"
B_ID="$(add_driver "$B_IDENTITY" "$B" B)"
C_ID="$(add_driver "$C_IDENTITY" "$C" C)"
check "three drivers" 3 "$(sql ride-driver-postgres "select count(*) from drivers where id in ('$A_ID', '$B_ID', '$C_ID')")"
sql ride-driver-postgres "
  update drivers set status = 'active' where id in ('$A_ID', '$B_ID', '$C_ID');
  update drivers set rating_average = 4.9, rating_count = 10 where id = '$A_ID';
  update drivers set rating_average = 4.0, rating_count = 5 where id = '$C_ID';" > /dev/null

echo "==> [1/5] creating a campaign"
NOW="$(at now)"
LATER="$(at '+2 hours')"
TIERS='[{"trips":2,"amount":"5000"},{"trips":4,"amount":"12000"}]'
expect "by a driver" 403 POST /v1/admin/incentives "$A" "$(campaign "Night quest" "$NOW" "$LATER" "$TIERS" "inc-$RUN")"
expect "by the operations role" 403 POST /v1/admin/incentives "$OPERATOR" "$(campaign "Night quest" "$NOW" "$LATER" "$TIERS" "inc-$RUN")"
expect "tiers paying less for more" 400 POST /v1/admin/incentives "$OWNER" "$(campaign "Night quest" "$NOW" "$LATER" '[{"trips":2,"amount":"5000"},{"trips":4,"amount":"4000"}]' "inc-bad-$RUN")"
expect "ending before it starts" 400 POST /v1/admin/incentives "$OWNER" "$(campaign "Night quest" "$LATER" "$NOW" "$TIERS" "inc-bad2-$RUN")"
expect "hours with only a start" 400 POST /v1/admin/incentives "$OWNER" "$(campaign "Night quest" "$NOW" "$LATER" "$TIERS" "inc-bad3-$RUN" ',"dailyStart":"22:00"')"
expect "by the owner" 200 POST /v1/admin/incentives "$OWNER" "$(campaign "Night quest" "$NOW" "$LATER" "$TIERS" "inc-$RUN")"
CAMPAIGN="$(body_field 'd["campaign"]["id"]')"
check "running, in IQD, with two tiers" "INCENTIVE_CAMPAIGN_STATUS_RUNNING 2 70" "$(body_field '" ".join((d["campaign"]["status"], str(len(d["campaign"]["tiers"])), d["campaign"]["minAcceptanceRate"].split(".")[0]))')"
expect "the same key again" 200 POST /v1/admin/incentives "$OWNER" "$(campaign "Night quest" "$NOW" "$LATER" "$TIERS" "inc-$RUN")"
check "is the same campaign" "$CAMPAIGN" "$(body_field 'd["campaign"]["id"]')"
expect "the same key for another campaign" 409 POST /v1/admin/incentives "$OWNER" "$(campaign "Day quest" "$NOW" "$LATER" "$TIERS" "inc-$RUN")"
expect "the running campaigns" 200 GET "/v1/admin/incentives?status=INCENTIVE_CAMPAIGN_STATUS_RUNNING&page_size=100" "$OWNER"
check "list it" True "$(body_field "any(c['id'] == '$CAMPAIGN' for c in d.get('campaigns', []))")"
expect "read by the operations role" 403 GET "/v1/admin/incentives/$CAMPAIGN" "$OPERATOR"

echo "==> [2/5] drivers' progress"
trips "$A_ID" 3 "$ZONE"
trips "$A_ID" 2 "$OTHER_ZONE"
offers "$A_ID" 3 1
trips "$B_ID" 2 "$ZONE"
offers "$B_ID" 1 3
trips "$C_ID" 2 "$ZONE"
check "A: 3 trips in the zone, tier 2 reached, 4 next, 75% accepted, nothing unmet" "3 2 4 75.00 []" \
  "$(progress "$A_ID" "$A" '" ".join((str(i["completedTrips"]), str(i["reachedTierTrips"]), str(i["nextTierTrips"]), i["acceptanceRate"], str(i.get("unmet", []))))')"
check "B: 25% accepted, short of 70%" "25.00 ['acceptance_rate']" "$(progress "$B_ID" "$B" '" ".join((i["acceptanceRate"], str(i.get("unmet", []))))')"
check "C: rated 4.0, short of 4.5" "['rating']" "$(progress "$C_ID" "$C" 'str(i.get("unmet", []))')"
expect "A reading B's" 403 GET "/v1/drivers/$B_ID/incentives" "$A"

echo "==> [3/5] the campaign ends and is paid"
sql ride-trip-postgres "
  update trips set completed_at = now() - interval '1 hour' where driver_id in ('$A_ID', '$B_ID', '$C_ID');
  update trip_offers set offered_at = now() - interval '1 hour' where driver_id in ('$A_ID', '$B_ID', '$C_ID');" > /dev/null
sql ride-wallet-postgres "
  update incentive_campaigns set starts_at = now() - interval '3 hours', ends_at = now() - interval '31 minutes'
  where id = '$CAMPAIGN';" > /dev/null
STATUS=""
for _ in $(seq 1 45); do
  STATUS="$(sql ride-wallet-postgres "select status from incentive_campaigns where id = '$CAMPAIGN'")"
  [ "$STATUS" = settled ] && break
  sleep 4
done
check "the worker settled it" settled "$STATUS"
expect "the payouts" 200 GET "/v1/admin/incentives/$CAMPAIGN/payouts" "$OWNER"
check "A paid 5000, B and C not, with why" \
  "$A_ID:INCENTIVE_PAYOUT_STATUS_PAID:5000: $B_ID:INCENTIVE_PAYOUT_STATUS_NOT_ELIGIBLE:0:acceptance_rate $C_ID:INCENTIVE_PAYOUT_STATUS_NOT_ELIGIBLE:0:rating" \
  "$(body_field "' '.join(p['driverId'] + ':' + p['status'] + ':' + p['amount'].split('.')[0] + ':' + ','.join(p.get('unmet', [])) for p in sorted(d['payouts'], key=lambda p: ['$A_ID', '$B_ID', '$C_ID'].index(p['driverId'])))")"
expect "the campaign" 200 GET "/v1/admin/incentives/$CAMPAIGN" "$OWNER"
check "settled, one driver, 5000" "INCENTIVE_CAMPAIGN_STATUS_SETTLED 1 5000" "$(body_field '" ".join((d["campaign"]["status"], str(d["campaign"]["paidDrivers"]), d["campaign"]["paidTotal"].split(".")[0]))')"
expect "A's ledger" 200 GET "/v1/wallets/$A_ID/transactions?owner_type=OWNER_TYPE_DRIVER&limit=1" "$A"
check "an incentive of 5000" "TRANSACTION_TYPE_INCENTIVE 5000" "$(body_field '" ".join((d["transactions"][0]["type"], d["transactions"][0]["amount"].split(".")[0]))')"
check "A sees it paid" "INCENTIVE_PAYOUT_STATUS_PAID" "$(progress "$A_ID" "$A" 'i["payout"]["status"]')"
check "cancelling it now" 400 "$(http POST "/v1/admin/incentives/$CAMPAIGN:cancel" "$OWNER" '{"reason":"too late"}')"
TOLD=0
for _ in $(seq 1 15); do
  TOLD="$(sql ride-notification-postgres "select count(*) from notifications where recipient_id = '$A_ID' and event_key = 'driver.incentive_earned'")"
  [ "$TOLD" = 1 ] && break
  sleep 1
done
check "A was told once" 1 "$TOLD"
check "B was not" 0 "$(sql ride-notification-postgres "select count(*) from notifications where recipient_id = '$B_ID' and event_key = 'driver.incentive_earned'")"

echo "==> [4/5] cancelling a campaign that has not started"
expect "a weekend campaign" 200 POST /v1/admin/incentives "$OWNER" "$(campaign "Weekend" "$(at '+1 day')" "$(at '+3 days')" "$TIERS" "inc-future-$RUN")"
FUTURE="$(body_field 'd["campaign"]["id"]')"
check "scheduled" INCENTIVE_CAMPAIGN_STATUS_SCHEDULED "$(body_field 'd["campaign"]["status"]')"
expect "without a reason" 400 POST "/v1/admin/incentives/$FUTURE:cancel" "$OWNER" '{"reason":""}'
expect "by the operations role" 403 POST "/v1/admin/incentives/$FUTURE:cancel" "$OPERATOR" '{"reason":"budget"}'
expect "with a reason" 200 POST "/v1/admin/incentives/$FUTURE:cancel" "$OWNER" '{"reason":"budget"}'
check "cancelled" "INCENTIVE_CAMPAIGN_STATUS_CANCELLED budget" "$(body_field '" ".join((d["campaign"]["status"], d["campaign"]["cancelReason"]))')"

echo "==> [5/5] earnings"
grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" \
  -d "{\"trip_id\":\"$SETTLED_TRIP\",\"rider_id\":\"$(uuid)\",\"driver_id\":\"$A_ID\",\"fare_amount\":\"10000\",\"payment_method\":\"PAYMENT_METHOD_CASH\"}" \
  "$WALLET_ADDR" ride.wallet.v1.WalletService/SettleTrip > /dev/null
EARNING="$(sql ride-wallet-postgres "select driver_earning::numeric(16,0) from trip_settlements where trip_id = '$SETTLED_TRIP'")"
COMMISSION="$(sql ride-wallet-postgres "select commission_amount::numeric(16,0) from trip_settlements where trip_id = '$SETTLED_TRIP'")"
expect "A's earnings today" 200 GET "/v1/drivers/$A_ID/earnings" "$A"
check "one trip of 10000 in cash, its commission, the 5000 bonus" "1 10000 10000 $COMMISSION 5000 $((EARNING + 5000))" \
  "$(body_field '" ".join([str(d["totals"].get("trips", 0))] + [d["totals"][k].split(".")[0] for k in ("fares", "cashCollected", "commission", "incentives", "netEarnings")])')"
expect "A's week" 200 GET "/v1/drivers/$A_ID/earnings?period=EARNINGS_PERIOD_WEEK" "$A"
check "seven days, Monday first, same totals" "7 0 5000" \
  "$(body_field '" ".join((str(len(d["days"])), str(__import__("datetime").date.fromisoformat(d["fromDate"]).weekday()), d["totals"]["incentives"].split(".")[0]))')"
expect "a bad date" 400 GET "/v1/drivers/$A_ID/earnings?date=04-10-2026" "$A"
expect "B reading A's" 403 GET "/v1/drivers/$A_ID/earnings" "$B"

echo
if [ "$FAILURES" -ne 0 ]; then
  echo "FAIL: $FAILURES check(s) failed"
  exit 1
fi

echo "PASS: the owner runs incentive campaigns; drivers see their progress, are paid once when it ends, and see their earnings"
