#!/usr/bin/env bash
# End-to-end test of the "new trip offer" push for drivers.
# Run from the repo root:
#   bash scripts/e2e/test-offer-push.sh
#
# When a trip is offered to a driver (dispatch does it with DISPATCH_OFFER_TTL=15 in
# production; here the script offers it by hand), trip-service writes a trip.offered event in
# the same transaction, and notification-service turns it into a notification for that driver.
#   1. the event exists once, names the trip, the driver and when the offer ends, and says
#      nothing about the rider
#   2. a refused second offer of the same trip to the same driver writes no event
#   3. the driver gets a trip.offer_received notification, and only one
#   4. the driver's app can then read the offer itself over HTTP
#
# It sets every driver offline so dispatch cannot take the trip first, and cancels the trip
# when it ends. It needs the local default DISPATCH_OFFER_TTL=0 like the other trip scripts.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
DRV="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
RIDER_IDENTITY="a0000000-0000-4000-8000-0000000000a1"
PROTOSET="/tmp/ride.binpb"

RIDER_ADDR="localhost:50052"
DRIVER_ADDR="localhost:50053"
TRIP_ADDR="localhost:50055"

FAILURES=0
BODY_FILE="$(mktemp)"
TRIP=""

call() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" \
    -H "authorization: Bearer $TOKEN" -d "$3" "$1" "$2"
}

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

tsql() { sql ride-trip-postgres "$1"; }

json_field() { # <python expression over d> (reads stdin)
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

body_field() { # <python expression over d>: from the last HTTP response
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE" 2> /dev/null || true
}

mint() { go run scripts/tools/devtoken/main.go -sub "$1"; }

http() { # <method> <path> <token>
  curl -sS -o "$BODY_FILE" -w '%{http_code}' -X "$1" "$BASE$2" -H "Authorization: Bearer $3"
}

same() { # <label> <expected> <actual>
  if [ "$2" = "$3" ]; then
    printf '  ok    %s -> %s\n' "$1" "${3:-(none)}"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "${2:-(none)}" "${3:-(none)}"
    FAILURES=$((FAILURES + 1))
  fi
}

fail() { echo; echo "FAIL: $*" >&2; exit 1; }

cleanup() {
  rm -f "$BODY_FILE"

  if [ -n "$TRIP" ]; then
    call "$TRIP_ADDR" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$TRIP\",\"reason\":\"test cleanup\"}" > /dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

# The notifications addressed to the driver for this kind of push. The column names of the
# notification tables are not assumed: each row is searched as text.
offer_notifications() {
  sql ride-notification-postgres "select count(*) from notifications n where to_jsonb(n)::text like '%trip.offer_received%' and to_jsonb(n)::text like '%$DRV%';"
}

echo "==> [1/4] preparing: protoset, the rider, the driver's token, no driver free for dispatch"
buf build -o "$PROTOSET"

[ -n "$(sql ride-notification-postgres "select to_regclass('public.notifications');")" ] \
  || fail "the notification database has no 'notifications' table: $(sql ride-notification-postgres "select string_agg(tablename, ', ') from pg_tables where schemaname='public';")"

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
[ -n "$DRIVER_IDENTITY" ] || fail "the test driver has no identity"
DRIVER_TOKEN="$(mint "$DRIVER_IDENTITY")"

for id in $(tsql "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted','in_progress');"); do
  call "$TRIP_ADDR" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
done

# With every driver offline, dispatch has nobody to give the trip to, so it stays REQUESTED
# and the offer below is the only thing that happens to it.
sql ride-driver-postgres "update drivers set availability_status='offline';" > /dev/null

echo "==> [2/4] a trip nobody has taken, offered to the driver"
call "$TRIP_ADDR" ride.trip.v1.TripService/RequestTrip \
  "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\",\"payment_method\":\"cash\"}" > /dev/null

TRIP="$(tsql "select id from trips where rider_id='$RIDER' order by created_at desc limit 1;")"
echo "    trip=$TRIP"
same "the trip is waiting" "requested|" "$(tsql "select status || '|' || coalesce(driver_id::text,'') from trips where id='$TRIP';")"

BEFORE="$(offer_notifications)"

call "$TRIP_ADDR" ride.trip.v1.TripService/OfferTrip "{\"trip_id\":\"$TRIP\",\"driver_id\":\"$DRV\",\"ttl_seconds\":30}" > /dev/null

same "one trip.offered event" 1 "$(tsql "select count(*) from outbox_events where event_type='trip.offered' and aggregate_id='$TRIP';")"
same "it names the driver" "$DRV" "$(tsql "select payload->>'driver_id' from outbox_events where event_type='trip.offered' and aggregate_id='$TRIP';")"
same "it names the trip" "$TRIP" "$(tsql "select payload->>'trip_id' from outbox_events where event_type='trip.offered' and aggregate_id='$TRIP';")"
same "it says when the offer ends" t "$(tsql "select (payload->>'expires_at')::timestamptz > now() from outbox_events where event_type='trip.offered' and aggregate_id='$TRIP';")"
same "it does not name the rider" f "$(tsql "select payload::text like '%$RIDER%' or payload ? 'rider_id' from outbox_events where event_type='trip.offered' and aggregate_id='$TRIP';")"

if call "$TRIP_ADDR" ride.trip.v1.TripService/OfferTrip "{\"trip_id\":\"$TRIP\",\"driver_id\":\"$DRV\",\"ttl_seconds\":30}" > /dev/null 2>&1; then
  echo "  FAIL  offering the same trip to the same driver twice was accepted"
  FAILURES=$((FAILURES + 1))
else
  echo "  ok    offering it to the same driver again is refused"
fi
same "and the refused offer wrote no event" 1 "$(tsql "select count(*) from outbox_events where event_type='trip.offered' and aggregate_id='$TRIP';")"

echo "==> [3/4] the driver is notified"
AFTER="$BEFORE"
for _ in $(seq 1 30); do
  AFTER="$(offer_notifications)"
  [ "$AFTER" -gt "$BEFORE" ] && break
  sleep 1
done

same "one new trip.offer_received notification for the driver" "$((BEFORE + 1))" "$AFTER"

echo "==> [4/4] the driver's app reads the offer"
same "the driver reads their pending offer" 200 "$(http GET "/v1/drivers/$DRV/offer" "$DRIVER_TOKEN")"
same "it is the offered trip" "$TRIP" "$(body_field 'd["offer"]["tripId"]')"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: an offer writes one trip.offered event with no rider in it, and the driver is notified once"
