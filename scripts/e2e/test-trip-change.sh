#!/usr/bin/env bash
# End-to-end test of POST /v1/drivers/{driver_id}/trips/{trip_id}/change, through the
# gateway, with real user tokens: the rider handed the driver more cash than the fare and
# the driver had no change, so the difference is credited to the rider's wallet.
# Run from the repo root:
#   bash scripts/e2e/test-trip-change.sh
#
# It runs one trip with an empty rider wallet (so the whole fare is cash) and checks:
#   1. only the trip's own driver can record it (no token 401, a rider 403, another driver 404)
#   2. bad amounts are refused: no change owed, above the limit, not a number (all 400)
#   3. a valid amount credits exactly the difference to the RIDER's wallet, writes one
#      change_credit ledger row, and takes NOTHING from the driver
#   4. recording the same amount again is harmless (no second credit); a different amount
#      is refused; the rider's wallet history and the settlement show the change
#
# It needs the local default DISPATCH_OFFER_TTL=0 (direct assignment) like the other trip
# scripts. It sets the test rider's wallet balance in the wallet database and puts the
# original balance back when it ends.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
DRV="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
DRIVER_B="4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
RIDER_IDENTITY="a0000000-0000-4000-8000-0000000000a1"
PROTOSET="/tmp/ride.binpb"
CHANGE=750

RIDER_ADDR="localhost:50052"
DRIVER_ADDR="localhost:50053"
LOCATION_ADDR="localhost:50054"
TRIP_ADDR="localhost:50055"
WALLET_ADDR="localhost:50058"

FAILURES=0
BODY_FILE="$(mktemp)"

call() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" \
    -H "authorization: Bearer $TOKEN" -d "$3" "$1" "$2"
}

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

wsql() { sql ride-wallet-postgres "$1"; }

json_field() { # <python expression over d> (reads stdin)
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

body_field() { # <python expression over d>: from the last HTTP response
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE" 2> /dev/null || true
}

mint() { go run scripts/tools/devtoken/main.go -sub "$1"; }

http() { # <method> <path> <token> [json body]
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$1" "$BASE$2")

  [ -n "$3" ] && args+=(-H "Authorization: Bearer $3")
  [ -n "${4:-}" ] && args+=(-H 'Content-Type: application/json' -d "$4")

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

same_number() { # <label> <expected number> <actual number>
  local equal
  equal="$(wsql "select '${3:-x}'::numeric = '$2'::numeric;" 2> /dev/null || true)"

  if [ "$equal" = "t" ]; then
    printf '  ok    %s -> %s\n' "$1" "$3"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "${3:-(absent)}"
    FAILURES=$((FAILURES + 1))
  fi
}

balance_of() { # <owner id>
  wsql "select coalesce(sum(balance),0) from wallets where owner_id='$1';"
}

fail() { echo; echo "FAIL: $*" >&2; exit 1; }

json_body() { # <cash received>
  printf '{"cashReceived":"%s"}' "$1"
}

echo "==> [1/5] preparing: protoset, identities, tokens, the rider's wallet"
buf build -o "$PROTOSET"

rider_id_for_identity() { # <identity id>
  call "$RIDER_ADDR" ride.rider.v1.RiderService/GetRiderByIdentity "{\"identity_id\":\"$1\"}" 2> /dev/null \
    | json_field 'd["rider"]["id"]' 2> /dev/null || true
}

RIDER="$(rider_id_for_identity "$RIDER_IDENTITY")"
if [ -z "$RIDER" ]; then
  call "$RIDER_ADDR" ride.rider.v1.RiderService/CreateRider \
    "{\"identity_id\":\"$RIDER_IDENTITY\",\"display_name\":\"Settlement Test Rider\"}" > /dev/null
  RIDER="$(rider_id_for_identity "$RIDER_IDENTITY")"
fi
[ -n "$RIDER" ] || fail "could not find or create the test rider"

DRIVER_IDENTITY="$(call "$DRIVER_ADDR" ride.driver.v1.DriverService/GetDriver "{\"driver_id\":\"$DRV\"}" | json_field 'd["driver"]["identityId"]')"
DRIVER_B_IDENTITY="$(call "$DRIVER_ADDR" ride.driver.v1.DriverService/GetDriver "{\"driver_id\":\"$DRIVER_B\"}" | json_field 'd["driver"]["identityId"]')"
[ -n "$DRIVER_IDENTITY" ] && [ -n "$DRIVER_B_IDENTITY" ] || fail "a test driver has no identity"

RIDER_TOKEN="$(mint "$RIDER_IDENTITY")"
DRIVER_TOKEN="$(mint "$DRIVER_IDENTITY")"
DRIVER_B_TOKEN="$(mint "$DRIVER_B_IDENTITY")"

call "$WALLET_ADDR" ride.wallet.v1.WalletService/GetWallet \
  "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER\"}" > /dev/null

ORIGINAL_RIDER_BALANCE="$(balance_of "$RIDER")"

cleanup() {
  rm -f "$BODY_FILE"
  wsql "update wallets set balance=$ORIGINAL_RIDER_BALANCE where owner_type='rider' and owner_id='$RIDER';" > /dev/null 2>&1 || true
}
trap cleanup EXIT

# An empty wallet: the whole fare is cash, and the change credit is the only money that reaches it.
wsql "update wallets set balance=0 where owner_type='rider' and owner_id='$RIDER';" > /dev/null

# A suspended driver receives no trips; make sure the test driver is in good standing.
if [ "$(wsql "select coalesce(sum(balance),0) < 50000 from wallets where owner_id='$DRV' and owner_type='driver';")" = "t" ]; then
  call "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp \
    "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRV\",\"amount\":\"100000\",\"idempotency_key\":\"e2e-change-$(date +%s)\",\"description\":\"e2e test deposit\"}" > /dev/null
fi

echo "==> [2/5] one cash trip"
for id in $(sql ride-trip-postgres "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted','in_progress');"); do
  call "$TRIP_ADDR" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
done

sql ride-driver-postgres "update drivers set availability_status='available' where id='$DRV';" > /dev/null

call "$LOCATION_ADDR" ride.location.v1.LocationService/UpdateLocation \
  "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRV\",\"coordinates\":{\"latitude\":36.1905,\"longitude\":44.0105}}" > /dev/null

call "$TRIP_ADDR" ride.trip.v1.TripService/RequestTrip \
  "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\",\"payment_method\":\"cash\"}" > /dev/null

TRIP="$(sql ride-trip-postgres "select id from trips where rider_id='$RIDER' order by created_at desc limit 1;")"
echo "    trip=$TRIP"

state=""
for _ in $(seq 1 30); do
  state="$(sql ride-trip-postgres "select status || '|' || coalesce(driver_id::text,'') from trips where id='$TRIP';")"
  [ "$state" = "accepted|$DRV" ] && break
  sleep 1
done
[ "$state" = "accepted|$DRV" ] || fail "trip was not dispatched (got: $state)"

call "$TRIP_ADDR" ride.trip.v1.TripService/StartTrip "{\"trip_id\":\"$TRIP\"}" > /dev/null
call "$TRIP_ADDR" ride.trip.v1.TripService/CompleteTrip "{\"trip_id\":\"$TRIP\"}" > /dev/null

FARE=""
for _ in $(seq 1 45); do
  FARE="$(wsql "select fare_amount from trip_settlements where trip_id='$TRIP';")"
  [ -n "$FARE" ] && break
  sleep 1
done
[ -n "$FARE" ] || fail "the trip was never settled"

MAX="$(wsql "select max_change_credit from wallet_configs order by created_at desc limit 1;")"
[ -n "$MAX" ] || fail "no change limit is configured"
[ "$(wsql "select $CHANGE::numeric <= $MAX::numeric;")" = "t" ] || fail "this test credits $CHANGE but the limit is $MAX"
echo "    fare=$FARE (all cash), change limit=$MAX"

CHANGE_URL="/v1/drivers/$DRV/trips/$TRIP/change"
RECEIVED="$(wsql "select $FARE::numeric + $CHANGE::numeric;")"
OVER_LIMIT="$(wsql "select $FARE::numeric + $MAX::numeric + 1;")"
DIFFERENT="$(wsql "select $FARE::numeric + 500;")"

echo "==> [3/5] who may record it, and which amounts are refused"
expect "no token" 401 POST "$CHANGE_URL" "" "$(json_body "$RECEIVED")"
expect "the rider, on the driver's route" 403 POST "$CHANGE_URL" "$RIDER_TOKEN" "$(json_body "$RECEIVED")"
expect "another driver, through their own route" 404 POST "/v1/drivers/$DRIVER_B/trips/$TRIP/change" "$DRIVER_B_TOKEN" "$(json_body "$RECEIVED")"
expect "another driver, through this driver's route" 403 POST "$CHANGE_URL" "$DRIVER_B_TOKEN" "$(json_body "$RECEIVED")"
expect "exactly the cash due: no change is owed" 400 POST "$CHANGE_URL" "$DRIVER_TOKEN" "$(json_body "$FARE")"
expect "less than the cash due" 400 POST "$CHANGE_URL" "$DRIVER_TOKEN" "$(json_body "1")"
expect "more change than the limit allows" 400 POST "$CHANGE_URL" "$DRIVER_TOKEN" "$(json_body "$OVER_LIMIT")"
expect "not a number" 400 POST "$CHANGE_URL" "$DRIVER_TOKEN" "$(json_body "abc")"
expect "a trip that does not exist" 404 POST "/v1/drivers/$DRV/trips/00000000-0000-4000-8000-00000000dead/change" "$DRIVER_TOKEN" "$(json_body "$RECEIVED")"
same_number "nothing was credited by the refused attempts" 0 "$(balance_of "$RIDER")"

echo "==> [4/5] the driver records $CHANGE of change"
DRIVER_BEFORE="$(balance_of "$DRV")"

expect "the driver records the change" 200 POST "$CHANGE_URL" "$DRIVER_TOKEN" "$(json_body "$RECEIVED")"
same_number "the change" "$CHANGE" "$(body_field 'd["changeAmount"]')"
same_number "the cash due" "$FARE" "$(body_field 'd["cashDue"]')"
same_number "the cash received" "$RECEIVED" "$(body_field 'd["cashReceived"]')"
same_number "the rider's wallet received exactly the change" "$CHANGE" "$(balance_of "$RIDER")"
same_number "nothing was taken from the driver" "$DRIVER_BEFORE" "$(balance_of "$DRV")"
same_number "one change_credit ledger row for the rider" "$CHANGE" "$(wsql "select coalesce(sum(t.amount),0) from wallet_transactions t join wallets w on w.id = t.wallet_id where t.type = 'change_credit' and t.trip_id = '$TRIP' and w.owner_type = 'rider';")"
same_number "and only one of them" 1 "$(wsql "select count(*) from wallet_transactions where type = 'change_credit' and trip_id = '$TRIP';")"
same_number "none on any driver's wallet" 0 "$(wsql "select count(*) from wallet_transactions t join wallets w on w.id = t.wallet_id where t.type = 'change_credit' and t.trip_id = '$TRIP' and w.owner_type = 'driver';")"

echo "==> [5/5] repeating it, and what the rider sees"
expect "the same amount again is harmless" 200 POST "$CHANGE_URL" "$DRIVER_TOKEN" "$(json_body "$RECEIVED")"
same_number "no second credit" "$CHANGE" "$(balance_of "$RIDER")"
same_number "still one ledger row" 1 "$(wsql "select count(*) from wallet_transactions where type = 'change_credit' and trip_id = '$TRIP';")"
expect "a different amount is refused" 400 POST "$CHANGE_URL" "$DRIVER_TOKEN" "$(json_body "$DIFFERENT")"
same_number "and credits nothing" "$CHANGE" "$(balance_of "$RIDER")"

expect "the rider reads the trip settlement" 200 GET "/v1/wallets/$RIDER/trips/$TRIP/settlement?owner_type=OWNER_TYPE_RIDER" "$RIDER_TOKEN"
same_number "the settlement shows the change to the rider" "$CHANGE" "$(body_field 'd["changeAmount"]')"
expect "the driver reads the trip settlement" 200 GET "/v1/wallets/$DRV/trips/$TRIP/settlement?owner_type=OWNER_TYPE_DRIVER" "$DRIVER_TOKEN"
same_number "the settlement shows the change to the driver" "$CHANGE" "$(body_field 'd["changeAmount"]')"

expect "the rider lists their transactions" 200 GET "/v1/wallets/$RIDER/transactions?owner_type=OWNER_TYPE_RIDER&limit=10" "$RIDER_TOKEN"
if grep -q "TRANSACTION_TYPE_CHANGE_CREDIT" "$BODY_FILE"; then
  echo "  ok    the wallet history names the change credit"
else
  echo "  FAIL  the wallet history does not show TRANSACTION_TYPE_CHANGE_CREDIT: $(head -c 300 "$BODY_FILE")"
  FAILURES=$((FAILURES + 1))
fi

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: the driver records the change, the rider's wallet gets it, the driver pays nothing, and it happens once"
