#!/usr/bin/env bash
# End-to-end SOS test: an SOS reaches a HUMAN, not only the person who pressed it.
# Run from the ride-platform repo root:
#   SOS_TEST_PHONE=9647XXXXXXXXX bash scripts/e2e/test-sos-alert.sh
#
# This sends ONE REAL SMS (two segments) through the BulkSMSIraq gateway, to
# YOUR phone only. Before it does anything it checks that the running
# notification-service is configured to text exactly SOS_TEST_PHONE and no
# other number, and refuses to continue otherwise.
#
# Prerequisites in services/notification-service/.env (then recreate the
# container: docker compose up -d --force-recreate notification-service):
#   SOS_OPERATOR_PHONES=<the same number as SOS_TEST_PHONE>
#   BULKSMSIRAQ_ENDPOINT=...  BULKSMSIRAQ_API_KEY=...  BULKSMSIRAQ_SENDER_ID=...
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

fail() { echo; echo "FAIL: $*" >&2; exit 1; }

if [ -z "${SOS_TEST_PHONE:-}" ]; then
  echo "Set SOS_TEST_PHONE to YOUR number (digits only, e.g. 9647701234567)." >&2
  exit 2
fi

PHONE="${SOS_TEST_PHONE#+}"
case "$PHONE" in
  *[!0-9]*|"") fail "SOS_TEST_PHONE must contain only digits" ;;
esac

echo "==> [1/8] safety check: the service must text ONLY $PHONE"
CONFIGURED="$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' ride-notification-service | grep '^SOS_OPERATOR_PHONES=' | cut -d= -f2- || true)"
CONFIGURED="${CONFIGURED//+/}"
echo "notification-service SOS_OPERATOR_PHONES=${CONFIGURED:-<empty>}"

[ -n "$CONFIGURED" ] || fail "SOS_OPERATOR_PHONES is not set on the running container (see the prerequisites at the top of this script)"
[ "$CONFIGURED" = "$PHONE" ] || fail "the service is configured to text '$CONFIGURED', not only '$PHONE'; refusing to send real SMS to anyone else"

echo "==> [2/8] refreshing protoset and finding the SOS enum value for a rider"
buf build -o "$PROTOSET"
RIDER_ENUM="$(grpcurl -plaintext -protoset "$PROTOSET" describe ride.trip.v1.SosTriggeredBy | grep -i rider | awk '{print $1}' | head -1)"
echo "rider enum value: ${RIDER_ENUM:-<not found>}"
[ -n "$RIDER_ENUM" ] || fail "could not find a rider value in ride.trip.v1.SosTriggeredBy"

echo "==> [3/8] preparing a trip that is accepted by a driver"
for ID in $(sql ride-trip-postgres "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted');"); do
  call localhost:50055 ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$ID\",\"reason\":\"test cleanup\"}" > /dev/null
done

sql ride-driver-postgres "update drivers set availability_status='available' where id='$DRV';" > /dev/null

call localhost:50054 ride.location.v1.LocationService/UpdateLocation \
  "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRV\",\"coordinates\":{\"latitude\":36.1905,\"longitude\":44.0105}}" > /dev/null

call localhost:50055 ride.trip.v1.TripService/RequestTrip \
  "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\"}" > /dev/null

TRIP="$(sql ride-trip-postgres "select id from trips order by created_at desc limit 1;")"
echo "trip=$TRIP"
sleep 6

STATE="$(sql ride-trip-postgres "select status || '|' || coalesce(driver_id::text,'') from trips where id='$TRIP';")"
[ "$STATE" = "accepted|$DRV" ] || fail "trip was not dispatched (got: $STATE)"

echo "==> [4/8] the rider presses SOS (this sends the real SMS)"
STARTED="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
call localhost:50055 ride.trip.v1.TripService/TriggerSOS \
  "{\"trip_id\":\"$TRIP\",\"triggered_by\":\"$RIDER_ENUM\",\"location\":{\"latitude\":36.1955,\"longitude\":44.0155}}"

echo "==> [5/8] waiting 10s for the alert"
sleep 10

echo "==> [6/8] what notification-service did"
docker logs --since 60s ride-notification-service 2>&1 | grep -E "SOS|sos" | tail -8

DELIVERED="$(docker logs --since 60s ride-notification-service 2>&1 | grep -c "SOS operator alert delivered" || true)"
CONFIRMED="$(sql ride-notification-postgres "select count(*) from notifications where recipient_id='$RIDER' and event_key='trip.sos_confirmed' and created_at > '$STARTED';")"
echo "operator alerts delivered : $DELIVERED"
echo "rider confirmations       : $CONFIRMED"

echo "==> [7/8] cleaning up the trip"
call localhost:50055 ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$TRIP\",\"reason\":\"test cleanup\"}" > /dev/null

[ "$DELIVERED" -ge 1 ] || fail "notification-service did not report delivering an operator alert"
[ "$CONFIRMED" -ge 1 ] || fail "the rider's own SOS confirmation was not created"

echo "==> [8/8] LOOK AT YOUR PHONE ($PHONE)"
echo "You should have received an SMS starting with 'SOS ALERT: the rider pressed SOS.'"
echo "with the trip id, the driver's name and an OpenStreetMap link near 36.1955, 44.0155."
read -r -p "Did the SMS arrive and look right? [y/N] " ANSWER

case "$ANSWER" in
  y|Y|yes|YES) echo; echo "PASS: an SOS reached a human out of band, and the rider still got their confirmation" ;;
  *) fail "the SMS did not arrive or was wrong; check the gateway values and the log lines above" ;;
esac
