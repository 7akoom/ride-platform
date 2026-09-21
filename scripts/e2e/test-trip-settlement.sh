#!/usr/bin/env bash
# End-to-end test of GET /v1/wallets/{owner_id}/trips/{trip_id}/settlement, through the
# gateway, with real user tokens.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-trip-settlement.sh
#
# It runs one trip paid "from the wallet" with only 500 in the rider's wallet, so the fare
# is split between the wallet and cash, and then checks:
#   1. the rider sees the fare, what left the wallet and the cash to hand over, and NOT the
#      commission
#   2. the driver sees the same split plus the commission and their earning
#   3. nobody else can read it: a rider through the driver's wallet is 403, another driver
#      through their own wallet is 404 (the trip is not theirs), no token is 401
#   4. an unknown trip, and an id that is not a UUID, are 404
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
MISSING_ID="00000000-0000-4000-8000-00000000dead"
PROTOSET="/tmp/ride.binpb"
WALLET_BALANCE=500

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

json_field() { # <python expression over d> (reads stdin)
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

body_field() { # <python expression over d>: from the last HTTP response
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE" 2> /dev/null || true
}

mint() { go run scripts/tools/devtoken/main.go -sub "$1"; }

http() { # <method> <path> <token>
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$1" "$BASE$2")

  [ -n "$3" ] && args+=(-H "Authorization: Bearer $3")

  curl "${args[@]}"
}

expect() { # <label> <expected status> <method> <path> <token>
  local actual
  actual="$(http "$3" "$4" "$5")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s: %s\n' "$1" "$2" "$actual" "$(head -c 200 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

same_text() { # <label> <expected> <actual>
  if [ "$2" = "$3" ]; then
    printf '  ok    %s -> %s\n' "$1" "${3:-(absent)}"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "${2:-(absent)}" "${3:-(absent)}"
    FAILURES=$((FAILURES + 1))
  fi
}

same_number() { # <label> <expected number> <actual number>
  local equal
  equal="$(sql ride-wallet-postgres "select '${3:-x}'::numeric = '$2'::numeric;" 2> /dev/null || true)"

  if [ "$equal" = "t" ]; then
    printf '  ok    %s -> %s\n' "$1" "$3"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "${3:-(absent)}"
    FAILURES=$((FAILURES + 1))
  fi
}

fail() { echo; echo "FAIL: $*" >&2; exit 1; }

echo "==> [1/4] preparing: protoset, identities, tokens, the rider's wallet"
buf build -o "$PROTOSET"

rider_id_for_identity() { # <identity id>
  call "$RIDER_ADDR" ride.rider.v1.RiderService/GetRiderByIdentity "{\"identity_id\":\"$1\"}" 2> /dev/null \
    | json_field 'd["rider"]["id"]' 2> /dev/null || true
}

# The rider must be a real profile (its wallet is reached through its identity): find it or create it.
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

ORIGINAL_RIDER_BALANCE="$(sql ride-wallet-postgres "select coalesce(sum(balance),0) from wallets where owner_id='$RIDER';")"

cleanup() {
  rm -f "$BODY_FILE"
  sql ride-wallet-postgres "update wallets set balance=$ORIGINAL_RIDER_BALANCE where owner_type='rider' and owner_id='$RIDER';" > /dev/null 2>&1 || true
}
trap cleanup EXIT

sql ride-wallet-postgres "update wallets set balance=$WALLET_BALANCE where owner_type='rider' and owner_id='$RIDER';" > /dev/null

# A suspended driver receives no trips; make sure the test driver is in good standing.
if [ "$(sql ride-wallet-postgres "select coalesce(sum(balance),0) < 50000 from wallets where owner_id='$DRV' and owner_type='driver';")" = "t" ]; then
  call "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp \
    "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRV\",\"amount\":\"100000\",\"idempotency_key\":\"e2e-settlement-$(date +%s)\",\"description\":\"e2e test deposit\"}" > /dev/null
fi

echo "==> [2/4] one trip paid from a wallet that holds only $WALLET_BALANCE"
for id in $(sql ride-trip-postgres "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted','in_progress');"); do
  call "$TRIP_ADDR" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
done

sql ride-driver-postgres "update drivers set availability_status='available' where id='$DRV';" > /dev/null

call "$LOCATION_ADDR" ride.location.v1.LocationService/UpdateLocation \
  "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRV\",\"coordinates\":{\"latitude\":36.1905,\"longitude\":44.0105}}" > /dev/null

call "$TRIP_ADDR" ride.trip.v1.TripService/RequestTrip \
  "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\",\"payment_method\":\"wallet\"}" > /dev/null

TRIP="$(sql ride-trip-postgres "select id from trips where rider_id='$RIDER' order by created_at desc limit 1;")"
echo "    trip=$TRIP"

state=""
for _ in $(seq 1 30); do
  state="$(sql ride-trip-postgres "select status || '|' || coalesce(driver_id::text,'') from trips where id='$TRIP';")"
  [ "$state" = "accepted|$DRV" ] && break
  sleep 1
done
[ "$state" = "accepted|$DRV" ] || fail "trip was not dispatched (got: $state)"

SETTLEMENT_URL_RIDER="/v1/wallets/$RIDER/trips/$TRIP/settlement?owner_type=OWNER_TYPE_RIDER"
SETTLEMENT_URL_DRIVER="/v1/wallets/$DRV/trips/$TRIP/settlement?owner_type=OWNER_TYPE_DRIVER"

expect "before the trip is settled, the rider gets nothing" 404 GET "$SETTLEMENT_URL_RIDER" "$RIDER_TOKEN"

call "$TRIP_ADDR" ride.trip.v1.TripService/StartTrip "{\"trip_id\":\"$TRIP\"}" > /dev/null
call "$TRIP_ADDR" ride.trip.v1.TripService/CompleteTrip "{\"trip_id\":\"$TRIP\"}" > /dev/null

row=""
for _ in $(seq 1 45); do
  row="$(sql ride-wallet-postgres "select fare_amount || '|' || commission_amount from trip_settlements where trip_id='$TRIP';")"
  [ -n "$row" ] && break
  sleep 1
done
[ -n "$row" ] || fail "the trip was never settled"

FARE="${row%%|*}"
COMMISSION="${row##*|}"
CASH="$(sql ride-wallet-postgres "select $FARE::numeric - $WALLET_BALANCE::numeric;")"
[ "$(sql ride-wallet-postgres "select $FARE::numeric > $WALLET_BALANCE::numeric;")" = "t" ] || fail "the fare ($FARE) is not larger than the wallet ($WALLET_BALANCE)"
echo "    fare=$FARE commission=$COMMISSION wallet=$WALLET_BALANCE cash=$CASH"

echo "==> [3/4] the two people on the trip read the split"
expect "the rider reads it" 200 GET "$SETTLEMENT_URL_RIDER" "$RIDER_TOKEN"
same_text "the payment method" PAYMENT_METHOD_WALLET "$(body_field 'd["paymentMethod"]')"
same_number "the fare" "$FARE" "$(body_field 'd["fareAmount"]')"
same_number "what left the wallet" "$WALLET_BALANCE" "$(body_field 'd["walletAmount"]')"
same_number "the cash to hand over" "$CASH" "$(body_field 'd["cashAmount"]')"
same_text "the rider does not see the commission" "" "$(body_field 'd.get("commissionAmount","")')"
same_text "the rider does not see the driver's earning" "" "$(body_field 'd.get("driverEarning","")')"

expect "the driver reads it" 200 GET "$SETTLEMENT_URL_DRIVER" "$DRIVER_TOKEN"
same_number "the driver sees the cash to collect" "$CASH" "$(body_field 'd["cashAmount"]')"
same_number "the driver sees the commission" "$COMMISSION" "$(body_field 'd["commissionAmount"]')"
same_number "the driver sees their earning" "$(sql ride-wallet-postgres "select $FARE::numeric - $COMMISSION::numeric;")" "$(body_field 'd["driverEarning"]')"

echo "==> [4/4] nobody else can read it"
expect "a rider reads it through the driver's wallet" 403 GET "$SETTLEMENT_URL_DRIVER" "$RIDER_TOKEN"
expect "a driver reads it through the rider's wallet" 403 GET "$SETTLEMENT_URL_RIDER" "$DRIVER_TOKEN"
expect "another driver, through their own wallet" 404 GET "/v1/wallets/$DRIVER_B/trips/$TRIP/settlement?owner_type=OWNER_TYPE_DRIVER" "$DRIVER_B_TOKEN"
expect "no token" 401 GET "$SETTLEMENT_URL_RIDER" ""
expect "an unknown trip" 404 GET "/v1/wallets/$RIDER/trips/$MISSING_ID/settlement?owner_type=OWNER_TYPE_RIDER" "$RIDER_TOKEN"
expect "an id that is not a UUID" 404 GET "/v1/wallets/$RIDER/trips/not-a-uuid/settlement?owner_type=OWNER_TYPE_RIDER" "$RIDER_TOKEN"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: the rider and the driver read how the trip was paid, and nobody else can"
