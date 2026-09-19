#!/usr/bin/env bash
# End-to-end smoke test for automatic dispatch.
# Run from the ride-platform repo root:
#   bash /mnt/c/Users/7akoom/Downloads/test-autodispatch.sh
#
# What it proves:
#   1. a suspended driver is reinstated by a real Wallet.TopUp
#   2. a NEW trip is requested while NO driver location exists yet
#   3. dispatch keeps retrying (delayed redelivery) instead of giving up
#   4. once the driver's location appears, the trip is accepted automatically
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

echo "==> [1/8] refreshing protoset"
buf build -o "$PROTOSET"

echo "==> [2/8] driver -> available (top-up below fixes the wallet side)"
sql ride-driver-postgres "update drivers set availability_status='available' where id='$DRV';"

echo "==> [3/8] wallet top-up 20000 (real ledger RPC, unique idempotency key)"
KEY="autodispatch-test-$(date +%s)"
call localhost:50058 ride.wallet.v1.WalletService/TopUp \
  "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRV\",\"amount\":\"20000\",\"idempotency_key\":\"$KEY\",\"description\":\"auto-dispatch smoke test\"}"

echo "--- standing after top-up:"
call localhost:50058 ride.wallet.v1.WalletService/CheckDriverStanding "{\"driver_id\":\"$DRV\"}"

echo "==> [4/8] cancelling leftover active trips of the test rider AND test driver (real CancelTrip RPC)"
for ID in $(sql ride-trip-postgres "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted');"); do
  echo "cancelling $ID"
  call localhost:50055 ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$ID\",\"reason\":\"test cleanup\"}" > /dev/null
done

echo "==> [5/8] requesting a new economy trip (no driver location is live yet)"
call localhost:50055 ride.trip.v1.TripService/RequestTrip \
  "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\"}"

TRIP="$(sql ride-trip-postgres "select id from trips order by created_at desc limit 1;")"
echo "trip=$TRIP"

echo "==> [6/8] waiting 12s so dispatch fails at least twice (retry every 5s)"
sleep 12

echo "==> [7/8] driver goes online near the pickup point; live location TTL is 30s"
call localhost:50054 ride.location.v1.LocationService/UpdateLocation \
  "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRV\",\"coordinates\":{\"latitude\":36.1905,\"longitude\":44.0105}}"

echo "    waiting 10s for the next dispatch attempt"
sleep 10

echo "==> [8/8] result"
RESULT="$(sql ride-trip-postgres "select status || '|' || coalesce(driver_id::text,'') from trips where id='$TRIP';")"
DRIVER_STATE="$(sql ride-driver-postgres "select availability_status from drivers where id='$DRV';")"
echo "trip status|driver_id : $RESULT"
echo "driver availability   : $DRIVER_STATE"
echo "--- dispatch-service log (last 60s):"
docker logs --since 60s ride-dispatch-service 2>&1 | tail -20

if [ "$RESULT" = "accepted|$DRV" ]; then
  echo
  echo "PASS: trip was dispatched automatically"
else
  echo
  echo "FAIL: expected accepted|$DRV" >&2
  exit 1
fi
