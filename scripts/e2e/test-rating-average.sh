#!/usr/bin/env bash
# End-to-end test of the rating averages: trip-service publishes trip.rated, and
# driver-service and rider-service fold each rating into the profile's average.
# Run from the repo root:
#   bash scripts/e2e/test-rating-average.sh
#
# It resets the test driver and the test rider to the starting 5.00 with a count of 0, runs
# two trips, and checks:
#   1. the first real rating REPLACES the starting 5.00 (a 3-star rating gives 3.00, not 4.00)
#   2. the second is a running average: (3 + 5) / 2 = 4.00 for the driver, (4 + 2) / 2 = 3.00
#      for the rider
#   3. a rating changes only the profile it is about: the rider's rating of the driver does
#      not touch the rider's own average, and the other way round
#   4. an event delivered twice counts once
#   5. the new average is what the driver's app reads over HTTP
#
# It needs the local default DISPATCH_OFFER_TTL=0 (direct assignment) like the other trip
# scripts. It puts both profiles back to 5.00 with a count of 0 when it ends.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
DRV="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
RIDER_IDENTITY="a0000000-0000-4000-8000-0000000000a1"
PROTOSET="/tmp/ride.binpb"

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

http() { # <method> <path> <token> [json body]
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$1" "$BASE$2")

  [ -n "$3" ] && args+=(-H "Authorization: Bearer $3")
  [ -n "${4:-}" ] && args+=(-H 'Content-Type: application/json' -d "$4")

  curl "${args[@]}"
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

driver_rating() { sql ride-driver-postgres "select rating_average || '|' || rating_count from drivers where id='$DRV';"; }
rider_rating() { sql ride-rider-postgres "select rating_average || '|' || rating_count from riders where id='$RIDER';"; }

# wait_for <label> <expected> <command that prints the value>: the consumers work asynchronously
wait_for() {
  local label="$1" expected="$2" got=""
  shift 2

  for _ in $(seq 1 40); do
    got="$("$@")"
    [ "$got" = "$expected" ] && break
    sleep 1
  done

  same "$label" "$expected" "$got"
}

reset_profiles() {
  sql ride-driver-postgres "update drivers set rating_average = 5.00, rating_count = 0 where id='$DRV';" > /dev/null
  sql ride-rider-postgres "update riders set rating_average = 5.00, rating_count = 0 where id='${RIDER:-none}';" > /dev/null 2>&1 || true
}

cleanup() {
  rm -f "$BODY_FILE"
  reset_profiles
}
trap cleanup EXIT

rating_body() { # <rated_by> <stars>
  printf '{"ratedBy":"%s","stars":%s}' "$1" "$2"
}

echo "==> [1/4] preparing: protoset, identities, tokens, the driver's standing"
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
[ -n "$DRIVER_IDENTITY" ] || fail "the test driver has no identity"

RIDER_TOKEN="$(mint "$RIDER_IDENTITY")"
DRIVER_TOKEN="$(mint "$DRIVER_IDENTITY")"

# A suspended driver receives no trips; make sure the test driver is in good standing.
if [ "$(sql ride-wallet-postgres "select coalesce(sum(balance),0) < 50000 from wallets where owner_id='$DRV' and owner_type='driver';")" = "t" ]; then
  call "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp \
    "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRV\",\"amount\":\"100000\",\"idempotency_key\":\"e2e-average-$(date +%s)\",\"description\":\"e2e test deposit\"}" > /dev/null
fi

reset_profiles
same "the driver starts at 5.00 with no ratings" "5.00|0" "$(driver_rating)"
same "the rider starts at 5.00 with no ratings" "5.00|0" "$(rider_rating)"

rated_trip() { # <stars the rider gives the driver> <stars the driver gives the rider>
  local trip state

  for id in $(sql ride-trip-postgres "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted','in_progress');"); do
    call "$TRIP_ADDR" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
  done

  sql ride-driver-postgres "update drivers set availability_status='available' where id='$DRV';" > /dev/null

  call "$LOCATION_ADDR" ride.location.v1.LocationService/UpdateLocation \
    "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRV\",\"coordinates\":{\"latitude\":36.1905,\"longitude\":44.0105}}" > /dev/null

  call "$TRIP_ADDR" ride.trip.v1.TripService/RequestTrip \
    "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\",\"payment_method\":\"cash\"}" > /dev/null

  trip="$(sql ride-trip-postgres "select id from trips where rider_id='$RIDER' order by created_at desc limit 1;")"

  state=""
  for _ in $(seq 1 30); do
    state="$(sql ride-trip-postgres "select status || '|' || coalesce(driver_id::text,'') from trips where id='$trip';")"
    [ "$state" = "accepted|$DRV" ] && break
    sleep 1
  done
  [ "$state" = "accepted|$DRV" ] || fail "trip was not dispatched (got: $state)"

  call "$TRIP_ADDR" ride.trip.v1.TripService/StartTrip "{\"trip_id\":\"$trip\"}" > /dev/null
  call "$TRIP_ADDR" ride.trip.v1.TripService/CompleteTrip "{\"trip_id\":\"$trip\"}" > /dev/null

  [ "$(http POST "/v1/trips/$trip:rate" "$RIDER_TOKEN" "$(rating_body RATED_BY_RIDER "$1")")" = 200 ] || fail "the rider could not rate the driver: $(head -c 200 "$BODY_FILE")"
  [ "$(http POST "/v1/trips/$trip:rate" "$DRIVER_TOKEN" "$(rating_body RATED_BY_DRIVER "$2")")" = 200 ] || fail "the driver could not rate the rider: $(head -c 200 "$BODY_FILE")"

  TRIP="$trip"
}

echo "==> [2/4] first trip: the rider gives the driver 3 stars, the driver gives the rider 4"
rated_trip 3 4
wait_for "the driver's first rating replaces the starting 5.00" "3.00|1" driver_rating
wait_for "the rider's first rating replaces the starting 5.00" "4.00|1" rider_rating

echo "==> [3/4] second trip: 5 stars for the driver, 2 for the rider (a running average)"
rated_trip 5 2
wait_for "the driver's average: (3 + 5) / 2" "4.00|2" driver_rating
wait_for "the rider's average: (4 + 2) / 2" "3.00|2" rider_rating

expect_status="$(http GET "/v1/drivers/$DRV" "$DRIVER_TOKEN")"
same "the driver's app reads its profile" 200 "$expect_status"
same "and sees the rating count" 2 "$(body_field 'd["driver"]["ratingCount"]')"
same "and the average" 4.0 "$(body_field 'float(d["driver"]["ratingAverage"])')"

echo "==> [4/4] the same event delivered twice counts once"
RATING_ID="$(sql ride-trip-postgres "select id from trip_ratings where trip_id='$TRIP' and rated_by='rider';")"
[ -n "$RATING_ID" ] || fail "the second trip's rating was not stored"

sql ride-trip-postgres "insert into outbox_events (aggregate_type, aggregate_id, event_type, schema_version, payload, occurred_at, available_at) select aggregate_type, aggregate_id, event_type, schema_version, payload, now(), now() from outbox_events where event_type='trip.rated' and aggregate_id='$TRIP' and payload->>'rated_by'='rider' limit 1;" > /dev/null

for _ in $(seq 1 30); do
  [ "$(sql ride-trip-postgres "select count(*) from outbox_events where event_type='trip.rated' and aggregate_id='$TRIP' and published_at is null;")" = 0 ] && break
  sleep 1
done
sleep 5

same "the repeated event was published" 2 "$(sql ride-trip-postgres "select count(*) from outbox_events where event_type='trip.rated' and aggregate_id='$TRIP' and payload->>'rated_by'='rider';")"
same "but the driver's average did not move" "4.00|2" "$(driver_rating)"
same "the rating was recorded as counted once" 1 "$(sql ride-driver-postgres "select count(*) from processed_rating_events where rating_id='$RATING_ID';")"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: ratings become running averages on the rated profile, once each, and only there"
