#!/usr/bin/env bash
# End-to-end test: a trip paid "from the wallet" when the wallet cannot cover the fare.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-wallet-split.sh
#
# Rule under test: the wallet pays what it holds, the rest is cash in the driver's hand,
# and the driver's balance moves by (wallet part - commission) so that the driver always
# ends with the fare minus the commission.
#
#   A. wallet smaller than the fare   -> rider wallet drops to 0, driver gets wallet part - commission
#   B. wallet EMPTY                   -> behaves like a cash trip: the commission is drawn from the driver
#                                        (before this rule the trip was never settled at all)
#   C. wallet larger than the fare    -> unchanged: rider pays the fare, driver gets fare - commission
#
# It needs the local default DISPATCH_OFFER_TTL=0 (direct assignment) like the other trip
# scripts. It sets the test rider's wallet balance directly in the wallet database and puts
# the original balance back when it ends.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
DRV="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
RIDER="d586ce00-5c1c-46f1-81b5-ed7e0977d075"
PROTOSET="/tmp/ride.binpb"

WALLET_ADDR="localhost:50058"
TRIP_ADDR="localhost:50055"
LOCATION_ADDR="localhost:50054"

FAILURES=0

call() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" \
    -H "authorization: Bearer $TOKEN" -d "$3" "$1" "$2"
}

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

fail() { echo; echo "FAIL: $*" >&2; exit 1; }

balance_of() { # <owner id>
  sql ride-wallet-postgres "select coalesce(sum(balance),0) from wallets where owner_id='$1';"
}

num() { # <sql numeric expression>
  sql ride-wallet-postgres "select round(($1)::numeric, 3);"
}

check() { # <label> <expected sql expression> <actual sql expression>
  local expected actual
  expected="$(num "$2")"
  actual="$(num "$3")"

  if [ "$expected" = "$actual" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$expected" "$actual"
    FAILURES=$((FAILURES + 1))
  fi
}

echo "==> [1/4] preparing: protoset, the rider's wallet, the driver's standing"
buf build -o "$PROTOSET"

call "$WALLET_ADDR" ride.wallet.v1.WalletService/GetWallet \
  "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER\"}" > /dev/null

ORIGINAL_RIDER_BALANCE="$(balance_of "$RIDER")"

restore_rider_balance() {
  sql ride-wallet-postgres "update wallets set balance=$ORIGINAL_RIDER_BALANCE where owner_type='rider' and owner_id='$RIDER';" > /dev/null 2>&1 || true
}
trap restore_rider_balance EXIT

set_rider_balance() { # <amount>
  sql ride-wallet-postgres "update wallets set balance=$1 where owner_type='rider' and owner_id='$RIDER';" > /dev/null
}

# A suspended driver receives no trips; make sure the test driver is in good standing.
if [ "$(sql ride-wallet-postgres "select coalesce(sum(balance),0) < 50000 from wallets where owner_id='$DRV' and owner_type='driver';")" = "t" ]; then
  call "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp \
    "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRV\",\"amount\":\"100000\",\"idempotency_key\":\"e2e-split-$(date +%s)\",\"description\":\"e2e test deposit\"}" > /dev/null
fi

run_scenario() { # <label> <rider wallet balance> <expect: partial|empty|full>
  local label="$1" balance="$2" kind="$3"
  local trip state row fare commission wallet_part driver_before

  echo "==> $label (rider wallet = $balance)"
  set_rider_balance "$balance"

  for id in $(sql ride-trip-postgres "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted','in_progress');"); do
    call "$TRIP_ADDR" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
  done

  sql ride-driver-postgres "update drivers set availability_status='available' where id='$DRV';" > /dev/null

  call "$LOCATION_ADDR" ride.location.v1.LocationService/UpdateLocation \
    "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRV\",\"coordinates\":{\"latitude\":36.1905,\"longitude\":44.0105}}" > /dev/null

  call "$TRIP_ADDR" ride.trip.v1.TripService/RequestTrip \
    "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\",\"payment_method\":\"wallet\"}" > /dev/null

  trip="$(sql ride-trip-postgres "select id from trips where rider_id='$RIDER' order by created_at desc limit 1;")"
  echo "    trip=$trip"

  state=""
  for _ in $(seq 1 30); do
    state="$(sql ride-trip-postgres "select status || '|' || coalesce(driver_id::text,'') from trips where id='$trip';")"
    [ "$state" = "accepted|$DRV" ] && break
    sleep 1
  done
  [ "$state" = "accepted|$DRV" ] || fail "trip was not dispatched (got: $state)"

  driver_before="$(balance_of "$DRV")"

  call "$TRIP_ADDR" ride.trip.v1.TripService/StartTrip "{\"trip_id\":\"$trip\"}" > /dev/null
  call "$TRIP_ADDR" ride.trip.v1.TripService/CompleteTrip "{\"trip_id\":\"$trip\"}" > /dev/null

  row=""
  for _ in $(seq 1 45); do
    row="$(sql ride-wallet-postgres "select fare_amount || '|' || commission_amount from trip_settlements where trip_id='$trip';")"
    [ -n "$row" ] && break
    sleep 1
  done
  [ -n "$row" ] || fail "the trip was never settled (a wallet-paid trip must settle even when the wallet is short)"

  fare="${row%%|*}"
  commission="${row##*|}"
  wallet_part="$(num "least($balance::numeric, $fare::numeric)")"
  echo "    fare=$fare commission=$commission wallet part=$wallet_part"

  case "$kind" in
    partial) [ "$(sql ride-wallet-postgres "select $fare::numeric > $balance::numeric;")" = "t" ] || fail "the fare ($fare) is not larger than the wallet ($balance): this scenario needs a bigger fare" ;;
    full) [ "$(sql ride-wallet-postgres "select $balance::numeric >= $fare::numeric;")" = "t" ] || fail "the wallet ($balance) does not cover the fare ($fare)" ;;
  esac

  check "the rider's wallet lost exactly the wallet part" "$balance - $wallet_part" "$(balance_of "$RIDER")"
  check "the driver's balance moved by wallet part - commission" "$driver_before + $wallet_part - $commission" "$(balance_of "$DRV")"
}

echo "==> [2/4] A: the wallet holds less than the fare"
run_scenario "A" 500 partial

echo "==> [3/4] B: the wallet is empty"
run_scenario "B" 0 empty

echo "==> [4/4] C: the wallet covers the fare"
run_scenario "C" 1000000 full

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: a wallet-paid trip settles whatever the wallet holds; the rest is cash and the driver is made whole"
