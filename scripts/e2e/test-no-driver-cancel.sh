#!/usr/bin/env bash
# End-to-end test: a trip nobody can serve is cancelled automatically.
# Run from the ride-platform repo root (takes about 2.5 minutes):
#   bash /mnt/c/Users/7akoom/Downloads/test-no-driver-cancel.sh
#
# What it proves:
#   1. with no driver location live, dispatch keeps retrying for the whole
#      search window (2 minutes by default) and then cancels the trip
#   2. the trip carries the reason "no drivers available"
#   3. the rider is told (trip.cancelled notification)
#   4. the rider is FREE again: a new trip can be requested right away
set -euo pipefail

TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
DRV="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
RIDER="d586ce00-5c1c-46f1-81b5-ed7e0977d075"
PROTOSET="/tmp/ride.binpb"
MAX_WAIT_SECONDS=170

call() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" \
    -H "authorization: Bearer $TOKEN" -d "$3" "$1" "$2"
}

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

fail() { echo; echo "FAIL: $*" >&2; exit 1; }

echo "==> [1/6] refreshing protoset"
buf build -o "$PROTOSET"

echo "==> [2/6] cancelling leftover active trips of the test rider AND driver"
for ID in $(sql ride-trip-postgres "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted');"); do
  echo "cancelling $ID"
  call localhost:50055 ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$ID\",\"reason\":\"test cleanup\"}" > /dev/null
done

echo "==> [3/6] waiting 32s so no driver location is live (location TTL is 30s)"
sleep 32

echo "==> [4/6] requesting a trip that nobody can serve"
STARTED_AT="$(date +%s)"
call localhost:50055 ride.trip.v1.TripService/RequestTrip \
  "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\"}" > /dev/null

TRIP="$(sql ride-trip-postgres "select id from trips order by created_at desc limit 1;")"
echo "trip=$TRIP"

echo "==> [5/6] polling every 5s (up to ${MAX_WAIT_SECONDS}s) until dispatch gives up"
STATUS=""
while :; do
  ELAPSED=$(( $(date +%s) - STARTED_AT ))
  STATUS="$(sql ride-trip-postgres "select status from trips where id='$TRIP';")"
  printf "  t+%3ss  status=%s\n" "$ELAPSED" "$STATUS"

  [ "$STATUS" = "cancelled" ] && break
  [ "$ELAPSED" -ge "$MAX_WAIT_SECONDS" ] && break
  sleep 5
done

[ "$STATUS" = "cancelled" ] || fail "the trip was never cancelled (status: $STATUS)"
[ "$ELAPSED" -ge 110 ] || fail "cancelled too early (t+${ELAPSED}s): dispatch must keep trying for the whole search window"

REASON="$(sql ride-trip-postgres "select cancellation_reason from trips where id='$TRIP';")"
echo "cancellation_reason: $REASON"
[ "$REASON" = "no drivers available" ] || fail "unexpected cancellation reason: $REASON"

echo "==> [6/6] the rider must be told, and must be free to request again"
sleep 3
NOTIFIED="$(sql ride-notification-postgres "select count(*) from notifications where recipient_id='$RIDER' and event_key='trip.cancelled' and created_at > now() - interval '30 seconds';")"
echo "trip.cancelled notifications in the last 30s: $NOTIFIED"

echo "--- dispatch-service log (cancellation line):"
docker logs --since 5m ride-dispatch-service 2>&1 | grep -E "cancelled trip|could not close out" | tail -3

call localhost:50055 ride.trip.v1.TripService/RequestTrip \
  "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\"}" > /dev/null \
  || fail "the rider is still blocked after the cancellation"

NEXT="$(sql ride-trip-postgres "select id from trips order by created_at desc limit 1;")"
echo "rider can request again: new trip $NEXT"
call localhost:50055 ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$NEXT\",\"reason\":\"test cleanup\"}" > /dev/null

[ "$NOTIFIED" -ge 1 ] || fail "no trip.cancelled notification reached the rider"

echo
echo "PASS: an unserved trip is cancelled after the search window, the rider is told and is free again"
