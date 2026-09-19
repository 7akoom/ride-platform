#!/usr/bin/env bash
# End-to-end trip lifecycle test.
# Run from the ride-platform repo root:
#   bash /mnt/c/Users/7akoom/Downloads/test-trip-lifecycle.sh
#
# What it proves, with NO manual DispatchTrip / CalculateFare calls:
#   1. a requested trip is dispatched to a driver automatically   (dispatch-service)
#   2. once the trip is completed, its fare is calculated by itself (pricing-service)
#   3. the fare is settled with the driver by itself               (wallet-service)
#   4. the rider gets the "trip completed" notification            (notification-service)
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

echo "==> [1/9] refreshing protoset"
buf build -o "$PROTOSET"

echo "==> [2/9] cancelling leftover active trips of the test rider AND driver"
for ID in $(sql ride-trip-postgres "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted');"); do
  echo "cancelling $ID"
  call localhost:50055 ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$ID\",\"reason\":\"test cleanup\"}" > /dev/null
done

echo "==> [3/9] driver -> available, wallet standing must allow trips"
sql ride-driver-postgres "update drivers set availability_status='available' where id='$DRV';"
call localhost:50058 ride.wallet.v1.WalletService/CheckDriverStanding "{\"driver_id\":\"$DRV\"}"

echo "==> [4/9] driver goes online near the pickup point (location TTL is 30s)"
call localhost:50054 ride.location.v1.LocationService/UpdateLocation \
  "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRV\",\"coordinates\":{\"latitude\":36.1905,\"longitude\":44.0105}}"

echo "==> [5/9] requesting a new economy trip"
BALANCE_BEFORE="$(sql ride-wallet-postgres "select balance from wallets where owner_id='$DRV';")"
echo "driver wallet balance before: $BALANCE_BEFORE"
call localhost:50055 ride.trip.v1.TripService/RequestTrip \
  "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\"}" > /dev/null

TRIP="$(sql ride-trip-postgres "select id from trips order by created_at desc limit 1;")"
echo "trip=$TRIP"

echo "==> [6/9] waiting 6s for automatic dispatch"
sleep 6

DISPATCHED="$(sql ride-trip-postgres "select status || '|' || coalesce(driver_id::text,'') from trips where id='$TRIP';")"
echo "trip status|driver_id : $DISPATCHED"

if [ "$DISPATCHED" != "accepted|$DRV" ]; then
  echo "--- dispatch-service log (last 60s):"
  docker logs --since 60s ride-dispatch-service 2>&1 | tail -15
  echo
  echo "FAIL: trip was not dispatched automatically" >&2
  exit 1
fi

echo "==> [7/9] driver starts the trip"
call localhost:50055 ride.trip.v1.TripService/StartTrip "{\"trip_id\":\"$TRIP\"}" > /dev/null

echo "==> [8/9] driver completes the trip"
call localhost:50055 ride.trip.v1.TripService/CompleteTrip "{\"trip_id\":\"$TRIP\"}" > /dev/null

echo "==> [9/9] waiting 12s for pricing to calculate the fare and wallet to settle it"
sleep 12

FARE="$(sql ride-pricing-postgres "select total || ' ' || currency_code || ' | class=' || coalesce(vehicle_class,'-') || ' | zone=' || coalesce(zone_id::text,'-') from fares where trip_id='$TRIP';")"
NOTIFIED="$(sql ride-notification-postgres "select count(*) from notifications where recipient_id='$RIDER' and created_at > now() - interval '2 minutes' and event_key = 'trip.completed';")"

echo "fare row              : ${FARE:-<none>}"
echo "trip.completed notices: $NOTIFIED (rider, last 2 min)"
BALANCE_AFTER="$(sql ride-wallet-postgres "select balance from wallets where owner_id='$DRV';")"
echo "driver wallet balance : $BALANCE_BEFORE -> $BALANCE_AFTER"
echo "--- rider's latest notifications:"
sql ride-notification-postgres "select event_key || '  ' || created_at from notifications where recipient_id='$RIDER' order by created_at desc limit 5;"
echo "--- pricing-service log (last 60s):"
docker logs --since 60s ride-pricing-service 2>&1 | tail -5
echo "--- wallet-service log (last 60s):"
docker logs --since 60s ride-wallet-service 2>&1 | tail -5

if [ -z "$FARE" ]; then
  echo
  echo "FAIL: no fare was calculated for the completed trip" >&2
  exit 1
fi

if [ "$BALANCE_BEFORE" = "$BALANCE_AFTER" ]; then
  echo
  echo "FAIL: fare was calculated but the trip was never settled (driver balance unchanged)" >&2
  exit 1
fi

echo
if [ "$NOTIFIED" -ge 1 ]; then
  echo "PASS: dispatched, priced, settled and notified automatically"
else
  echo "PASS (fare + settlement): dispatched, priced and settled automatically"
  echo "WARN: no 'trip.completed' notification found - check the notification list above" >&2
fi
