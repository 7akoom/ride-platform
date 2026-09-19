#!/usr/bin/env bash
# End-to-end test for payment_method on trips.
# Run from the ride-platform repo root:
#   bash /mnt/c/Users/7akoom/Downloads/test-payment-method.sh
#
# What it proves:
#   1. card (and any unknown method) is rejected up front - no trip is created
#   2. a CASH trip is stored as cash and settled the cash way:
#        driver balance goes DOWN by the commission, the rider's wallet is untouched
#   3. a WALLET trip is stored as wallet and settled the wallet way:
#        rider balance goes DOWN by the fare, driver balance goes UP by the earning
set -euo pipefail

TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
DRV="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
RIDER="d586ce00-5c1c-46f1-81b5-ed7e0977d075"
PROTOSET="/tmp/ride.binpb"

call() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" \
    -H "authorization: Bearer $TOKEN" -d "$3" "$1" "$2"
}

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

driver_balance() { sql ride-wallet-postgres "select coalesce(sum(balance),0) from wallets where owner_id='$DRV';"; }
rider_balance()  { sql ride-wallet-postgres "select coalesce(sum(balance),0) from wallets where owner_id='$RIDER';"; }
delta() { sql ride-wallet-postgres "select $2 - $1;"; } # <before> <after>

fail() { echo; echo "FAIL: $*" >&2; exit 1; }

# run_trip <payment_method_json_fragment>  -> sets TRIP
run_trip() {
  local method_json="$1"

  for ID in $(sql ride-trip-postgres "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted');"); do
    call localhost:50055 ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$ID\",\"reason\":\"test cleanup\"}" > /dev/null
  done

  sql ride-driver-postgres "update drivers set availability_status='available' where id='$DRV';" > /dev/null

  call localhost:50054 ride.location.v1.LocationService/UpdateLocation \
    "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRV\",\"coordinates\":{\"latitude\":36.1905,\"longitude\":44.0105}}" > /dev/null

  call localhost:50055 ride.trip.v1.TripService/RequestTrip \
    "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\"$method_json}" > /dev/null

  TRIP="$(sql ride-trip-postgres "select id from trips order by created_at desc limit 1;")"
  echo "trip=$TRIP"

  sleep 6

  local dispatched
  dispatched="$(sql ride-trip-postgres "select status || '|' || coalesce(driver_id::text,'') from trips where id='$TRIP';")"
  [ "$dispatched" = "accepted|$DRV" ] || fail "trip was not dispatched (got: $dispatched)"

  call localhost:50055 ride.trip.v1.TripService/StartTrip "{\"trip_id\":\"$TRIP\"}" > /dev/null
  call localhost:50055 ride.trip.v1.TripService/CompleteTrip "{\"trip_id\":\"$TRIP\"}" > /dev/null

  sleep 12
}

echo "==> [1/9] refreshing protoset"
buf build -o "$PROTOSET"

echo "==> [2/9] card must be rejected up front (no trip may be created)"
TRIPS_BEFORE="$(sql ride-trip-postgres "select count(*) from trips;")"
REJECTED="$(call localhost:50055 ride.trip.v1.TripService/RequestTrip \
  "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"payment_method\":\"card\"}" 2>&1 || true)"
echo "$REJECTED" | head -4
echo "$REJECTED" | grep -qi "invalid payment method" || fail "card was not rejected with 'invalid payment method'"
[ "$(sql ride-trip-postgres "select count(*) from trips;")" = "$TRIPS_BEFORE" ] || fail "a trip was created for a rejected payment method"

echo "==> [3/9] a trip that names no method must default to cash"
run_trip ""
DEFAULTED="$(sql ride-trip-postgres "select payment_method from trips where id='$TRIP';")"
echo "payment_method stored: $DEFAULTED"
[ "$DEFAULTED" = "cash" ] || fail "expected cash by default, got $DEFAULTED"

echo "==> [4/9] CASH trip: explicit cash, balances before"
DRIVER_BEFORE="$(driver_balance)"
RIDER_BEFORE="$(rider_balance)"
echo "driver=$DRIVER_BEFORE rider=$RIDER_BEFORE"
run_trip ',"payment_method":"cash"'
CASH_TRIP="$TRIP"
FARE="$(sql ride-pricing-postgres "select total from fares where trip_id='$CASH_TRIP';")"
DRIVER_AFTER="$(driver_balance)"
RIDER_AFTER="$(rider_balance)"

echo "==> [5/9] CASH trip result"
echo "stored method : $(sql ride-trip-postgres "select payment_method from trips where id='$CASH_TRIP';")"
echo "fare          : $FARE"
echo "driver balance: $DRIVER_BEFORE -> $DRIVER_AFTER (delta $(delta "$DRIVER_BEFORE" "$DRIVER_AFTER"))"
echo "rider balance : $RIDER_BEFORE -> $RIDER_AFTER (delta $(delta "$RIDER_BEFORE" "$RIDER_AFTER"))"
[ "$(sql ride-trip-postgres "select payment_method from trips where id='$CASH_TRIP';")" = "cash" ] || fail "cash trip not stored as cash"
[ "$(delta "$DRIVER_BEFORE" "$DRIVER_AFTER" | cut -c1 )" = "-" ] || fail "cash: driver balance should go DOWN (commission)"
[ "$(sql ride-wallet-postgres "select $RIDER_BEFORE = $RIDER_AFTER;")" = "t" ] || fail "cash: the rider's wallet must not change"

echo "==> [6/9] top up the rider's wallet so a wallet-paid trip can be settled"
call localhost:50058 ride.wallet.v1.WalletService/TopUp \
  "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER\",\"amount\":\"50000\",\"idempotency_key\":\"payment-method-test-$(date +%s)\",\"description\":\"payment method test\"}" > /dev/null

echo "==> [7/9] WALLET trip, balances before"
DRIVER_BEFORE="$(driver_balance)"
RIDER_BEFORE="$(rider_balance)"
echo "driver=$DRIVER_BEFORE rider=$RIDER_BEFORE"
run_trip ',"payment_method":"wallet"'
WALLET_TRIP="$TRIP"
FARE="$(sql ride-pricing-postgres "select total from fares where trip_id='$WALLET_TRIP';")"
DRIVER_AFTER="$(driver_balance)"
RIDER_AFTER="$(rider_balance)"

echo "==> [8/9] WALLET trip result"
echo "stored method : $(sql ride-trip-postgres "select payment_method from trips where id='$WALLET_TRIP';")"
echo "fare          : $FARE"
echo "driver balance: $DRIVER_BEFORE -> $DRIVER_AFTER (delta $(delta "$DRIVER_BEFORE" "$DRIVER_AFTER"))"
echo "rider balance : $RIDER_BEFORE -> $RIDER_AFTER (delta $(delta "$RIDER_BEFORE" "$RIDER_AFTER"))"
[ "$(sql ride-trip-postgres "select payment_method from trips where id='$WALLET_TRIP';")" = "wallet" ] || fail "wallet trip not stored as wallet"
[ "$(sql ride-wallet-postgres "select ($RIDER_BEFORE - $RIDER_AFTER) = $FARE;")" = "t" ] || fail "wallet: the rider should have paid exactly the fare"
[ "$(sql ride-wallet-postgres "select ($DRIVER_AFTER - $DRIVER_BEFORE) > 0;")" = "t" ] || fail "wallet: the driver should have been credited the earning"

echo "--- wallet-service log (last 90s):"
docker logs --since 90s ride-wallet-service 2>&1 | grep -E "trip settled|cannot settle|settlement failed" | tail -6

echo "==> [9/9] done"
echo
echo "PASS: card rejected, cash and wallet trips stored and settled the right way"
