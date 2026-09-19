#!/usr/bin/env bash
# End-to-end SOS test through the WEBHOOK channel, with a local receiver.
# Needs no SMS account and sends no real message to anyone.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-sos-webhook.sh
#
# The receiver (sos-webhook-sink.py) runs as a container ON THE COMPOSE
# NETWORK, so the notification-service container reaches it by name. That
# works with any Docker setup, unlike host.docker.internal, which does not
# resolve on a plain Docker Engine.
#
# Prerequisites in services/notification-service/.env, then recreate the
# container (docker compose up -d --force-recreate notification-service):
#   SOS_OPERATOR_PHONES=                      # EMPTY: no SMS to anyone
#   SOS_WEBHOOK_URL=http://ride-sos-sink:8099/sos
#   SOS_WEBHOOK_SECRET=sos-test-secret
#
# What it proves:
#   1. an SOS produces a signed webhook alert with the trip, the driver's name
#      and the exact place SOS was pressed
#   2. the person who pressed SOS still gets their confirmation
set -euo pipefail

TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
DRV="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
RIDER="d586ce00-5c1c-46f1-81b5-ed7e0977d075"
PROTOSET="/tmp/ride.binpb"
SINK_NAME="ride-sos-sink"
SINK_PORT=8099
SECRET="sos-test-secret"
EXPECTED_URL="http://${SINK_NAME}:${SINK_PORT}/sos"
LOCAL_LOG="/tmp/sos-sink.log"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

call() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" \
    -H "authorization: Bearer $TOKEN" -d "$3" "$1" "$2"
}

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

container_env() { # <VAR>
  docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' ride-notification-service \
    | grep "^$1=" | cut -d= -f2- || true
}

fetch_sink_log() {
  docker exec "$SINK_NAME" cat /tmp/sos-sink.log > "$LOCAL_LOG" 2>/dev/null || : > "$LOCAL_LOG"
}

fail() { echo; echo "FAIL: $*" >&2; exit 1; }

echo "==> [1/8] safety check: no SMS to anyone, webhook pointing at the sink container"
PHONES="$(container_env SOS_OPERATOR_PHONES)"
URL="$(container_env SOS_WEBHOOK_URL)"
CONTAINER_SECRET="$(container_env SOS_WEBHOOK_SECRET)"
echo "SOS_OPERATOR_PHONES=${PHONES:-<empty>}"
echo "SOS_WEBHOOK_URL=${URL:-<empty>}"

[ -z "$PHONES" ] || fail "SOS_OPERATOR_PHONES must be EMPTY for this test (it must not text anyone); see the prerequisites at the top"
[ "$URL" = "$EXPECTED_URL" ] || fail "SOS_WEBHOOK_URL must be $EXPECTED_URL on the running container (recreate it after editing .env)"
[ "$CONTAINER_SECRET" = "$SECRET" ] || fail "SOS_WEBHOOK_SECRET must be $SECRET on the running container"

echo "==> [2/8] starting the webhook sink as a container on the compose network"
NETWORK="$(docker inspect -f '{{range $name, $conf := .NetworkSettings.Networks}}{{println $name}}{{end}}' ride-notification-service | head -1)"
[ -n "$NETWORK" ] || fail "could not find the compose network of ride-notification-service"
echo "network: $NETWORK"

docker rm -f "$SINK_NAME" > /dev/null 2>&1 || true
trap 'docker rm -f "$SINK_NAME" > /dev/null 2>&1 || true' EXIT

docker pull -q python:3-alpine > /dev/null
docker run -d --rm --name "$SINK_NAME" --network "$NETWORK" \
  -v "$HERE:/sink:ro" \
  -e SOS_WEBHOOK_SECRET="$SECRET" -e SINK_LOG=/tmp/sos-sink.log -e SINK_PORT="$SINK_PORT" \
  python:3-alpine python /sink/sos-webhook-sink.py > /dev/null
sleep 2
[ "$(docker inspect -f '{{.State.Running}}' "$SINK_NAME" 2>/dev/null)" = "true" ] \
  || fail "the sink container did not start: $(docker logs "$SINK_NAME" 2>&1 | tail -3)"

echo "==> [3/8] finding the SOS enum value for a rider"
buf build -o "$PROTOSET"
RIDER_ENUM="$(grpcurl -plaintext -protoset "$PROTOSET" describe ride.trip.v1.SosTriggeredBy | grep -i rider | awk '{print $1}' | head -1)"
[ -n "$RIDER_ENUM" ] || fail "could not find a rider value in ride.trip.v1.SosTriggeredBy"
echo "rider enum value: $RIDER_ENUM"

echo "==> [4/8] preparing a trip that is accepted by a driver"
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

echo "==> [5/8] the rider presses SOS"
STARTED="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
call localhost:50055 ride.trip.v1.TripService/TriggerSOS \
  "{\"trip_id\":\"$TRIP\",\"triggered_by\":\"$RIDER_ENUM\",\"location\":{\"latitude\":36.1955,\"longitude\":44.0155}}" > /dev/null

echo "==> [6/8] waiting up to 30s for the alert to reach the sink"
FOUND=""
for _ in $(seq 1 30); do
  fetch_sink_log
  if grep -q "$TRIP" "$LOCAL_LOG"; then
    FOUND=1
    break
  fi
  sleep 1
done

echo "==> [7/8] what the sink received"
if [ -n "$FOUND" ]; then
  python3 - "$TRIP" "$LOCAL_LOG" <<'PY'
import json, sys

trip, path = sys.argv[1], sys.argv[2]
for line in open(path):
    record = json.loads(line)
    body = record.get("body") or {}
    if body.get("trip_id") == trip:
        print("signature valid :", record["signature_valid"])
        print("triggered_by    :", body.get("triggered_by"))
        print("driver_name     :", body.get("driver_name"))
        print("location        :", body.get("latitude"), body.get("longitude"), "(" + str(body.get("location_source")) + ")")
        print("map_url         :", body.get("map_url"))
        print("text            :", body.get("text"))
PY
fi

CONFIRMED="$(sql ride-notification-postgres "select count(*) from notifications where recipient_id='$RIDER' and event_key='trip.sos_confirmed' and created_at > '$STARTED';")"
echo "rider confirmations: $CONFIRMED"

echo "==> [8/8] cleaning up the trip"
call localhost:50055 ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$TRIP\",\"reason\":\"test cleanup\"}" > /dev/null

[ -n "$FOUND" ] || fail "no alert reached the sink; check: docker logs --since 2m ride-notification-service | grep -i sos"

CHECK="$(python3 - "$TRIP" "$LOCAL_LOG" <<'PY'
import json, sys

trip, path = sys.argv[1], sys.argv[2]
problems = []
for line in open(path):
    record = json.loads(line)
    body = record.get("body") or {}
    if body.get("trip_id") != trip:
        continue
    if not record["signature_valid"]:
        problems.append("the signature did not verify")
    if body.get("triggered_by") != "rider":
        problems.append("triggered_by is not rider")
    if body.get("location_source") != "sos_press":
        problems.append("location is not the place SOS was pressed")
    if abs(float(body.get("latitude", 0)) - 36.1955) > 1e-6:
        problems.append("wrong latitude")
    if not body.get("driver_name"):
        problems.append("driver name missing")
    if "openstreetmap.org" not in str(body.get("map_url", "")):
        problems.append("map link missing")
print("; ".join(problems))
PY
)"

[ -z "$CHECK" ] || fail "alert content is wrong: $CHECK"
[ "$CONFIRMED" -ge 1 ] || fail "the rider's own SOS confirmation was not created"

echo
echo "PASS: an SOS produced a signed operator alert with the right trip, driver and location, and the rider still got their confirmation"
