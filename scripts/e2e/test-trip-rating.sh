#!/usr/bin/env bash
# End-to-end test of POST /v1/trips/{trip_id}:rate, through the gateway, with real user tokens.
# Run from the repo root:
#   bash scripts/e2e/test-trip-rating.sh
#
# It runs one cash trip and checks:
#   1. a trip that is not completed cannot be rated, and neither can one completed more than
#      24 hours ago
#   2. only a side of the trip can rate, and only as its own side (no token 401; a rider
#      speaking as the driver, the driver speaking as the rider, another driver, or no side
#      named: all 403)
#   3. bad input is refused: 0 stars, 6 stars, a comment of 501 characters (400)
#   4. the rider rates the driver and the driver rates the rider (200), each once (a second
#      rating by the same side is 409 and changes nothing)
#   5. the ratings are stored for the right people, an Arabic comment survives intact, and
#      each rating produced one trip.rated event that carries the stars but never the comment
#
# It needs the local default DISPATCH_OFFER_TTL=0 (direct assignment) like the other trip
# scripts. It changes the trip's completed_at in the trip database to test the 24-hour window.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
DRV="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
DRIVER_B="4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
RIDER_IDENTITY="a0000000-0000-4000-8000-0000000000a1"
PROTOSET="/tmp/ride.binpb"
ARABIC_COMMENT="سائق ممتاز وسيارة نظيفة"

RIDER_ADDR="localhost:50052"
DRIVER_ADDR="localhost:50053"
LOCATION_ADDR="localhost:50054"
TRIP_ADDR="localhost:50055"
WALLET_ADDR="localhost:50058"

FAILURES=0
BODY_FILE="$(mktemp)"
trap 'rm -f "$BODY_FILE"' EXIT

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

mint() { go run scripts/tools/devtoken/main.go -sub "$1"; }

http() { # <method> <path> <token> [json body]
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$1" "$BASE$2")

  [ -n "$3" ] && args+=(-H "Authorization: Bearer $3")
  [ -n "${4:-}" ] && args+=(-H 'Content-Type: application/json' -d "$4")

  curl "${args[@]}"
}

expect() { # <label> <expected status> <method> <path> <token> [json body]
  local actual
  actual="$(http "$3" "$4" "$5" "${6:-}")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s: %s\n' "$1" "$2" "$actual" "$(head -c 200 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
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

rating_body() { # <rated_by> <stars> [comment]
  python3 -c 'import json,sys; print(json.dumps({"ratedBy": sys.argv[1], "stars": int(sys.argv[2]), "comment": sys.argv[3]}, ensure_ascii=False))' "$1" "$2" "${3:-}"
}

echo "==> [1/5] preparing: protoset, identities, tokens, the driver's standing"
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
DRIVER_B_IDENTITY="$(call "$DRIVER_ADDR" ride.driver.v1.DriverService/GetDriver "{\"driver_id\":\"$DRIVER_B\"}" | json_field 'd["driver"]["identityId"]')"
[ -n "$DRIVER_IDENTITY" ] && [ -n "$DRIVER_B_IDENTITY" ] || fail "a test driver has no identity"

RIDER_TOKEN="$(mint "$RIDER_IDENTITY")"
DRIVER_TOKEN="$(mint "$DRIVER_IDENTITY")"
DRIVER_B_TOKEN="$(mint "$DRIVER_B_IDENTITY")"

# A suspended driver receives no trips; make sure the test driver is in good standing.
if [ "$(sql ride-wallet-postgres "select coalesce(sum(balance),0) < 50000 from wallets where owner_id='$DRV' and owner_type='driver';")" = "t" ]; then
  call "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp \
    "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRV\",\"amount\":\"100000\",\"idempotency_key\":\"e2e-rating-$(date +%s)\",\"description\":\"e2e test deposit\"}" > /dev/null
fi

echo "==> [2/5] one cash trip; before it is completed nobody can rate it"
for id in $(sql ride-trip-postgres "select id from trips where (rider_id='$RIDER' or driver_id='$DRV') and status in ('requested','accepted','in_progress');"); do
  call "$TRIP_ADDR" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null
done

sql ride-driver-postgres "update drivers set availability_status='available' where id='$DRV';" > /dev/null

call "$LOCATION_ADDR" ride.location.v1.LocationService/UpdateLocation \
  "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRV\",\"coordinates\":{\"latitude\":36.1905,\"longitude\":44.0105}}" > /dev/null

call "$TRIP_ADDR" ride.trip.v1.TripService/RequestTrip \
  "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":36.19,\"longitude\":44.01},\"dropoff\":{\"latitude\":36.2,\"longitude\":44.02},\"vehicle_class\":\"economy\",\"payment_method\":\"cash\"}" > /dev/null

TRIP="$(tsql "select id from trips where rider_id='$RIDER' order by created_at desc limit 1;")"
echo "    trip=$TRIP"

state=""
for _ in $(seq 1 30); do
  state="$(tsql "select status || '|' || coalesce(driver_id::text,'') from trips where id='$TRIP';")"
  [ "$state" = "accepted|$DRV" ] && break
  sleep 1
done
[ "$state" = "accepted|$DRV" ] || fail "trip was not dispatched (got: $state)"

RATE_URL="/v1/trips/$TRIP:rate"

expect "the rider rates a trip that has not started" 400 POST "$RATE_URL" "$RIDER_TOKEN" "$(rating_body RATED_BY_RIDER 5)"

call "$TRIP_ADDR" ride.trip.v1.TripService/StartTrip "{\"trip_id\":\"$TRIP\"}" > /dev/null
expect "the rider rates a trip in progress" 400 POST "$RATE_URL" "$RIDER_TOKEN" "$(rating_body RATED_BY_RIDER 5)"
expect "the driver rates a trip in progress" 400 POST "$RATE_URL" "$DRIVER_TOKEN" "$(rating_body RATED_BY_DRIVER 5)"

call "$TRIP_ADDR" ride.trip.v1.TripService/CompleteTrip "{\"trip_id\":\"$TRIP\"}" > /dev/null

echo "==> [3/5] the 24-hour window, who may rate, and bad input"
tsql "update trips set completed_at = now() - interval '25 hours' where id='$TRIP';" > /dev/null
expect "a trip completed 25 hours ago" 400 POST "$RATE_URL" "$RIDER_TOKEN" "$(rating_body RATED_BY_RIDER 5)"
tsql "update trips set completed_at = now() where id='$TRIP';" > /dev/null

expect "no token" 401 POST "$RATE_URL" "" "$(rating_body RATED_BY_RIDER 5)"
expect "the rider, speaking as the driver" 403 POST "$RATE_URL" "$RIDER_TOKEN" "$(rating_body RATED_BY_DRIVER 5)"
expect "the driver, speaking as the rider" 403 POST "$RATE_URL" "$DRIVER_TOKEN" "$(rating_body RATED_BY_RIDER 5)"
expect "another driver, speaking as the driver" 403 POST "$RATE_URL" "$DRIVER_B_TOKEN" "$(rating_body RATED_BY_DRIVER 5)"
expect "no side named" 403 POST "$RATE_URL" "$RIDER_TOKEN" '{"stars":5}'
expect "zero stars" 400 POST "$RATE_URL" "$RIDER_TOKEN" "$(rating_body RATED_BY_RIDER 0)"
expect "six stars" 400 POST "$RATE_URL" "$RIDER_TOKEN" "$(rating_body RATED_BY_RIDER 6)"
expect "a comment of 501 characters" 400 POST "$RATE_URL" "$RIDER_TOKEN" "$(rating_body RATED_BY_RIDER 5 "$(python3 -c 'print("a" * 501)')")"
same "nothing was stored by the refused attempts" 0 "$(tsql "select count(*) from trip_ratings where trip_id='$TRIP';")"

echo "==> [4/5] each side rates once"
expect "the rider rates the driver" 200 POST "$RATE_URL" "$RIDER_TOKEN" "$(rating_body RATED_BY_RIDER 5 "$ARABIC_COMMENT")"
expect "the rider rates again" 409 POST "$RATE_URL" "$RIDER_TOKEN" "$(rating_body RATED_BY_RIDER 1 "changed my mind")"
expect "the driver rates the rider" 200 POST "$RATE_URL" "$DRIVER_TOKEN" "$(rating_body RATED_BY_DRIVER 4)"
expect "the driver rates again" 409 POST "$RATE_URL" "$DRIVER_TOKEN" "$(rating_body RATED_BY_DRIVER 1)"

echo "==> [5/5] what was stored, and what was published"
same "two ratings, one per side" 2 "$(tsql "select count(*) from trip_ratings where trip_id='$TRIP';")"
same "the rider's rating is for the driver" "rider|$RIDER|$DRV|5" "$(tsql "select rated_by || '|' || rater_id || '|' || ratee_id || '|' || stars from trip_ratings where trip_id='$TRIP' and rated_by='rider';")"
same "the driver's rating is for the rider" "driver|$DRV|$RIDER|4" "$(tsql "select rated_by || '|' || rater_id || '|' || ratee_id || '|' || stars from trip_ratings where trip_id='$TRIP' and rated_by='driver';")"
same "the refused second rating changed nothing" 5 "$(tsql "select stars from trip_ratings where trip_id='$TRIP' and rated_by='rider';")"
same "the Arabic comment is stored intact" "$ARABIC_COMMENT" "$(tsql "select comment from trip_ratings where trip_id='$TRIP' and rated_by='rider';")"
same "no comment is stored as nothing" "" "$(tsql "select coalesce(comment, '') from trip_ratings where trip_id='$TRIP' and rated_by='driver';")"

same "one trip.rated event per rating" 2 "$(tsql "select count(*) from outbox_events where event_type='trip.rated' and aggregate_id='$TRIP';")"
same "the event carries the stars and who was rated" "5|$DRV|rider" "$(tsql "select payload->>'stars' || '|' || (payload->>'ratee_id') || '|' || (payload->>'rated_by') from outbox_events where event_type='trip.rated' and aggregate_id='$TRIP' and payload->>'rated_by'='rider';")"
same "no event carries a comment" 0 "$(tsql "select count(*) from outbox_events where event_type='trip.rated' and aggregate_id='$TRIP' and payload ? 'comment';")"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: each side rates the other once, only after a completed trip, and the comment stays private"
