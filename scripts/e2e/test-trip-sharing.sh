#!/usr/bin/env bash
# End-to-end test of trip sharing links, on the real services, through the
# gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-trip-sharing.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken), grpcurl, and the default TRIP_SHARE_AFTER_END (30m).
#
# What it proves:
#   1. only the trip's rider makes a link, only while the trip is under way,
#      at most 5 live at once; the token is returned once and only its hash
#      is stored
#   2. anyone with the link, without an account, sees the trip, and once a
#      driver has it their name, car and position; never the rider, the
#      price or a phone number; a made-up token shows nothing
#   3. stopping sharing ends every link; a link still shows a trip for a
#      while after it ends, without the driver's position, then stops
#
# It creates a throw-away city and zone (far from any real one), a rider, a
# stranger, a driver and a staff member, and removes them again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
TRIP_ADDR="localhost:50055"
PROTOSET="/tmp/ride.binpb"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
DRIVER_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
PLATE="E2E-SHARE-$RUN"
CITY="E2E Share City $RUN"
UNKNOWN="$(uuid)"

INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' services/trip-service/.env | tr -d '"')"
[ -n "$INTERNAL_TOKEN" ] || { echo "ABORT: INTERNAL_SERVICE_TOKEN is missing from services/trip-service/.env" >&2; exit 2; }

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
OTHER_ID=""
CITY_ID=""
DRIVER_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  sql ride-staff-postgres "delete from staff_members where id = '$OWNER_STAFF_ID';" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID$OTHER_ID" ]; then
    sql ride-trip-postgres "
      delete from trip_shares where trip_id in (select id from trips where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}'));
      update trips set status = 'cancelled', cancelled_at = now()
       where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}') and status in ('requested', 'accepted', 'in_progress');" > /dev/null 2>&1 || true
    sql ride-pricing-postgres "
      delete from fares where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');
      delete from rider_trip_stats where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');" > /dev/null 2>&1 || true
  fi
  sql ride-location-postgres "
    delete from zones where city_id in (select id from cities where name like 'E2E Share City%');
    delete from cities where name like 'E2E Share City%';" > /dev/null 2>&1 || true
  sql ride-driver-postgres "delete from drivers where vehicle_plate_number = '$PLATE';" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID$OTHER_ID" ]; then
    sql ride-rider-postgres "delete from riders where id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');" > /dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1"; }

body_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE"
}

http() { # <method> <path> <token> [json body]
  local method="$1" path="$2" token="$3" body="${4:-}"
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$method" "$BASE$path")

  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")

  curl "${args[@]}"
}

expect() { # <label> <expected status> <method> <path> <token> [json body]
  local actual
  actual="$(http "$3" "$4" "$5" "${6:-}")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s: %s\n' "$1" "$2" "$actual" "$(head -c 300 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

check() { # <label> <expected> <actual>
  if [ "$2" = "$3" ]; then
    printf '  ok    %s -> %s\n' "$1" "$3"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "$3"
    FAILURES=$((FAILURES + 1))
  fi
}

internal_call() { # <address> <service/method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$3" "$1" "$2"
}

report_position() { # <latitude> <longitude>
  curl -sS -o /dev/null -X PUT "$BASE/v1/locations/$DRIVER_ID" -H "Authorization: Bearer $DRIVER" \
    -H 'Content-Type: application/json' \
    -d "{\"entityType\":\"ENTITY_TYPE_DRIVER\",\"coordinates\":{\"latitude\":$1,\"longitude\":$2}}" || true
}

# share <trip id>: makes a link as the rider; prints its token.
share() {
  local status
  status="$(http POST "/v1/trips/$1/shares" "$RIDER" '{}')"
  [ "$status" = 200 ] || { echo "FAIL: could not share the trip ($status): $(head -c 200 "$BODY_FILE")" >&2; exit 1; }
  body_field 'd["token"]'
}

PICKUP='{"latitude":26.05,"longitude":26.05}'
DROPOFF='{"latitude":26.07,"longitude":26.07}'
# What a link must never show, wherever it would sit in the answer.
PRIVATE='["riderId", "driverId", "passengerName", "passengerPhone", "quotedFare", "quoteId", "id"]'

buf build -o "$PROTOSET"
RIDER="$(mint "$RIDER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"

echo "==> [0/3] a served zone, a rider, a stranger, a driver"
sql ride-staff-postgres "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
  values ('$OWNER_STAFF_ID', '$OWNER_IDENTITY', 'e2e-share-$RUN@ride.test', 'E2E Share', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values ('$OWNER_STAFF_ID', '$OWNER_ROLE');" > /dev/null
expect "a city" 200 POST /v1/admin/cities "$OWNER" "{\"name\":\"$CITY\",\"timeZone\":\"Asia/Baghdad\",\"center\":$PICKUP}"
CITY_ID="$(body_field 'd["city"]["id"]')"
expect "a zone" 200 POST /v1/admin/zones "$OWNER" "{\"cityId\":\"$CITY_ID\",\"name\":\"E2E\",\"boundary\":[{\"latitude\":26,\"longitude\":26},{\"latitude\":26,\"longitude\":26.1},{\"latitude\":26.1,\"longitude\":26.1},{\"latitude\":26.1,\"longitude\":26}]}"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Share Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "a stranger" 200 POST /v1/riders "$OTHER" "{\"identityId\":\"$OTHER_IDENTITY\",\"displayName\":\"E2E Share Stranger\"}"
OTHER_ID="$(body_field 'd["rider"]["id"]')"
expect "a driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"Karwan\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
sql ride-driver-postgres "update drivers set status = 'active', availability_status = 'available' where id = '$DRIVER_ID';" > /dev/null

echo "==> [1/3] making links"
expect "a trip for Sara" 200 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"pickupAddress\":\"Home\",\"dropoffAddress\":\"Airport\",\"passengerName\":\"Sara\",\"passengerPhone\":\"+9647500000002\"}"
TRIP="$(body_field 'd["trip"]["id"]')"
expect "a stranger shares it" 403 POST "/v1/trips/$TRIP/shares" "$OTHER" '{}'
expect "the driver shares it" 403 POST "/v1/trips/$TRIP/shares" "$DRIVER" '{}'
expect "without an account" 401 POST "/v1/trips/$TRIP/shares" "" '{}'
expect "the rider shares it" 200 POST "/v1/trips/$TRIP/shares" "$RIDER" '{}'
TOKEN="$(body_field 'd["token"]')"
check "a 43-character token, the URL built from it or left to the app" True \
  "$(body_field 'len(d["token"]) == 43 and (d.get("url", "") == "" or d["url"].endswith(d["token"]))')"
HASH="$(python3 -c "import hashlib,sys; print(hashlib.sha256(sys.argv[1].encode()).hexdigest())" "$TOKEN")"
check "only its hash is stored" "1 0" \
  "$(sql ride-trip-postgres "select count(*) filter (where encode(token_hash, 'hex') = '$HASH') || ' ' || count(*) filter (where position('$TOKEN' in encode(token_hash, 'escape')) > 0) from trip_shares where trip_id = '$TRIP';")"
for n in 2 3 4 5; do share "$TRIP" > /dev/null; done
expect "a sixth live link" 400 POST "/v1/trips/$TRIP/shares" "$RIDER" '{}'

echo "==> [2/3] what a link shows"
expect "the link, without an account" 200 GET "/v1/shared-trips/$TOKEN" ""
check "the trip, not yet a driver" "TRIP_STATUS_REQUESTED Home Airport False" \
  "$(body_field '" ".join((d["trip"]["status"], d["trip"]["pickupAddress"], d["trip"]["dropoffAddress"], str(bool(d["trip"].get("driverName")))))')"
check "nothing private" "[]" "$(body_field "sorted(k for k in $PRIVATE if k in d['trip'])")"
expect "a made-up token" 404 GET "/v1/shared-trips/$(python3 -c 'import secrets; print(secrets.token_urlsafe(32))')" ""
expect "a malformed one" 404 GET "/v1/shared-trips/not-a-token" ""
internal_call "$TRIP_ADDR" ride.trip.v1.TripService/AcceptTrip "{\"trip_id\":\"$TRIP\",\"driver_id\":\"$DRIVER_ID\"}" > /dev/null 2>&1 || true
report_position 26.051 26.051
expect "the link, once a driver has the trip" 200 GET "/v1/shared-trips/$TOKEN" ""
check "who drives, in which car, where" "TRIP_STATUS_ACCEPTED Karwan Kia $PLATE 26.051" \
  "$(body_field '" ".join((d["trip"]["status"], d["trip"]["driverName"], d["trip"]["vehicle"]["make"], d["trip"]["vehicle"]["plateNumber"], str(d["trip"]["driverLocation"]["latitude"])))')"
check "and still nothing private" "[]" "$(body_field "sorted(k for k in $PRIVATE if k in d['trip'])")"
expect "the driver starts" 200 POST "/v1/trips/$TRIP:start" "$DRIVER" '{}'

echo "==> [3/3] links stop"
expect "a stranger stops the sharing" 403 POST "/v1/trips/$TRIP/shares:stop" "$OTHER" '{}'
expect "the rider stops it" 200 POST "/v1/trips/$TRIP/shares:stop" "$RIDER" '{}'
check "five links stopped" 5 "$(body_field 'd["stopped"]')"
expect "the first link" 404 GET "/v1/shared-trips/$TOKEN" ""
LATER="$(share "$TRIP")"
check "a new link after stopping" 43 "${#LATER}"
expect "the driver completes" 200 POST "/v1/trips/$TRIP:complete" "$DRIVER" '{}'
expect "the new link, the trip just over" 200 GET "/v1/shared-trips/$LATER" ""
check "completed, the driver still named, no position" "TRIP_STATUS_COMPLETED Karwan None" \
  "$(body_field '" ".join((d["trip"]["status"], d["trip"]["driverName"], str(d["trip"].get("driverLocation"))))')"
check "the link ends 30 minutes after the trip" True \
  "$(body_field '__import__("datetime").datetime.fromisoformat(d["trip"]["linkExpiresAt"].replace("Z", "+00:00")) - __import__("datetime").datetime.fromisoformat(d["trip"]["completedAt"].replace("Z", "+00:00")) == __import__("datetime").timedelta(minutes=30)')"
expect "sharing a trip that is over" 400 POST "/v1/trips/$TRIP/shares" "$RIDER" '{}'
sql ride-trip-postgres "update trips set completed_at = now() - interval '31 minutes' where id = '$TRIP';" > /dev/null
expect "the link, half an hour after" 404 GET "/v1/shared-trips/$LATER" ""

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: only the rider shares a trip under way; a link shows the trip, its driver and car and where they are, to anyone, and nothing private; links stop when told, and a while after the trip ends"
