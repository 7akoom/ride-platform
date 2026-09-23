#!/usr/bin/env bash
# End-to-end test of saved addresses, a trip requested from one, the pickup
# photo for the captain, and recent destinations, on the real services,
# through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-saved-addresses.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken) and the object store on 127.0.0.1:8333.
#
# What it proves:
#   1. a rider saves addresses (one home, one work, others with a label) with
#      a photo of the entrance; the photo is then kept by the address and the
#      rider cannot delete it on its own; someone else's photo is refused
#   2. nobody else reads or changes the rider's addresses
#   3. a trip requested from the saved home takes its point, address,
#      details, note for the captain and photo; the offer-free parts show in
#      the trip
#   4. the pickup photo is only for the rider and the driver of the trip, and
#      only while it is accepted or in progress
#   5. a completed trip's destination is in the rider's recent destinations
#   6. replacing or removing the photo, or deleting the address, deletes the
#      old photo
#
# It creates a throw-away city and zone (far from any real one), two riders,
# a driver and one staff member (to make the zone), and removes them again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
TRIP_ADDR="localhost:50055"
PROTOSET="/tmp/ride.binpb"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
DRIVER_IDENTITY="$(uuid)"
STAFF_IDENTITY="$(uuid)"
STAFF_ID="$(uuid)"
PLATE="E2E-ADDR-$RUN"
CITY="E2E Address City $RUN"

INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' services/trip-service/.env | tr -d '"')"
[ -n "$INTERNAL_TOKEN" ] || { echo "ABORT: INTERNAL_SERVICE_TOKEN is missing from services/trip-service/.env" >&2; exit 2; }

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
OTHER_ID=""
TRIP_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  sql ride-staff-postgres "delete from staff_members where id = '$STAFF_ID';" > /dev/null 2>&1 || true
  sql ride-location-postgres "
    delete from zones where city_id in (select id from cities where name like 'E2E Address City%');
    delete from cities where name like 'E2E Address City%';" > /dev/null 2>&1 || true
  sql ride-driver-postgres "delete from drivers where vehicle_plate_number = '$PLATE';" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID$OTHER_ID" ]; then
    sql ride-rider-postgres "delete from riders where id in ('${RIDER_ID:-00000000-0000-0000-0000-000000000000}', '${OTHER_ID:-00000000-0000-0000-0000-000000000000}');" > /dev/null 2>&1 || true
  fi
  sql ride-media-postgres "delete from media_objects where owner_identity_id in ('$RIDER_IDENTITY', '$OTHER_IDENTITY');" > /dev/null 2>&1 || true
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

# upload_photo <token> <file>: uploads an address photo, prints its media id.
upload_photo() {
  local size status
  size="$(stat -c %s "$2")"
  status="$(http POST /v1/media:upload "$1" "{\"purpose\":\"MEDIA_PURPOSE_ADDRESS_PHOTO\",\"contentType\":\"image/png\",\"sizeBytes\":$size}")"
  [ "$status" = 200 ] || { echo "upload reservation answered $status: $(head -c 200 "$BODY_FILE")" >&2; return 1; }

  local id url
  id="$(body_field 'd["media"]["id"]')"
  url="$(body_field 'd["uploadUrl"]')"

  status="$(curl -sS -o /dev/null -w '%{http_code}' -X PUT -H 'Content-Type: image/png' --data-binary "@$2" "$url")"
  [ "$status" = 200 ] || { echo "PUT answered $status" >&2; return 1; }

  status="$(http POST "/v1/media/$id:complete" "$1" '{}')"
  [ "$status" = 200 ] && [ "$(body_field 'd["media"]["status"]')" = MEDIA_STATUS_READY ] \
    || { echo "complete answered $status: $(head -c 200 "$BODY_FILE")" >&2; return 1; }

  echo "$id"
}

python3 - "$WORK" <<'PY'
import struct, sys, zlib

def png(width, height, shade):
    raw = b"".join(
        b"\x00" + b"".join(bytes((shade, (x * 7) % 256, (y * 5) % 256)) for x in range(width))
        for y in range(height))
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) +
            chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))

for name, shade in (("gate1", 40), ("gate2", 160), ("other", 90)):
    open(f"{sys.argv[1]}/{name}.png", "wb").write(png(32, 24, shade))
PY

buf build -o "$PROTOSET"
RIDER="$(mint "$RIDER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"

echo "==> [0/6] a served zone, two riders and a driver"
sql ride-staff-postgres "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
  values ('$STAFF_ID', '$STAFF_IDENTITY', 'e2e-addr-$RUN@ride.test', 'E2E Address', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values ('$STAFF_ID', '$OPERATIONS_ROLE');" > /dev/null
STAFF="$(mint "$STAFF_IDENTITY")"
expect "a city" 200 POST /v1/admin/cities "$STAFF" "{\"name\":\"$CITY\",\"timeZone\":\"Asia/Baghdad\",\"center\":{\"latitude\":10.05,\"longitude\":10.05}}"
CITY_ID="$(body_field 'd["city"]["id"]')"
expect "a zone" 200 POST /v1/admin/zones "$STAFF" "{\"cityId\":\"$CITY_ID\",\"name\":\"E2E\",\"boundary\":[{\"latitude\":10,\"longitude\":10},{\"latitude\":10,\"longitude\":10.1},{\"latitude\":10.1,\"longitude\":10.1},{\"latitude\":10.1,\"longitude\":10}]}"
expect "the rider's profile" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Address Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "another rider's profile" 200 POST /v1/riders "$OTHER" "{\"identityId\":\"$OTHER_IDENTITY\",\"displayName\":\"E2E Other Rider\"}"
OTHER_ID="$(body_field 'd["rider"]["id"]')"
expect "a driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Address Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"

echo "==> [1/6] saving addresses, with a photo"
PHOTO1="$(upload_photo "$RIDER" "$WORK/gate1.png")"
OTHER_PHOTO="$(upload_photo "$OTHER" "$WORK/other.png")"
HOME_BODY="{\"kind\":\"SAVED_ADDRESS_KIND_HOME\",\"coordinates\":{\"latitude\":10.05,\"longitude\":10.05},\"address\":\"E2E Home, Gulan Street\",\"details\":\"Building 4, floor 2\",\"noteForDriver\":\"Blue gate, ring twice\""
expect "someone else's photo" 400 POST "/v1/riders/$RIDER_ID/addresses" "$RIDER" "$HOME_BODY,\"photoMediaId\":\"$OTHER_PHOTO\"}"
expect "save home with a photo" 200 POST "/v1/riders/$RIDER_ID/addresses" "$RIDER" "$HOME_BODY,\"photoMediaId\":\"$PHOTO1\"}"
HOME_ID="$(body_field 'd["address"]["id"]')"
check "the photo is the address's" "$PHOTO1" "$(body_field 'd["address"]["photoMediaId"]')"
expect "the rider deletes the kept photo" 400 DELETE "/v1/media/$PHOTO1" "$RIDER"
expect "a second home" 409 POST "/v1/riders/$RIDER_ID/addresses" "$RIDER" "$HOME_BODY}"
expect "another place without a label" 400 POST "/v1/riders/$RIDER_ID/addresses" "$RIDER" "{\"kind\":\"SAVED_ADDRESS_KIND_OTHER\",\"coordinates\":{\"latitude\":10.06,\"longitude\":10.06}}"
expect "the gym" 200 POST "/v1/riders/$RIDER_ID/addresses" "$RIDER" "{\"kind\":\"SAVED_ADDRESS_KIND_OTHER\",\"label\":\"Gym\",\"coordinates\":{\"latitude\":10.06,\"longitude\":10.06},\"address\":\"E2E Gym\"}"
expect "work" 200 POST "/v1/riders/$RIDER_ID/addresses" "$RIDER" "{\"kind\":\"SAVED_ADDRESS_KIND_WORK\",\"coordinates\":{\"latitude\":10.07,\"longitude\":10.07},\"address\":\"E2E Work, 100m Street\"}"
WORK_ID="$(body_field 'd["address"]["id"]')"
expect "list" 200 GET "/v1/riders/$RIDER_ID/addresses" "$RIDER"
check "home, work, then the gym" "SAVED_ADDRESS_KIND_HOME,SAVED_ADDRESS_KIND_WORK,SAVED_ADDRESS_KIND_OTHER" "$(body_field '",".join(a["kind"] for a in d["addresses"])')"

echo "==> [2/6] nobody else"
expect "another rider lists them" 403 GET "/v1/riders/$RIDER_ID/addresses" "$OTHER"
expect "another rider reads home" 403 GET "/v1/riders/$RIDER_ID/addresses/$HOME_ID" "$OTHER"
expect "another rider deletes home" 403 DELETE "/v1/riders/$RIDER_ID/addresses/$HOME_ID" "$OTHER"
expect "home through the other rider's profile" 404 GET "/v1/riders/$OTHER_ID/addresses/$HOME_ID" "$OTHER"
expect "no token" 401 GET "/v1/riders/$RIDER_ID/addresses" ""

echo "==> [3/6] a trip from the saved home"
expect "someone else's saved address" 404 POST /v1/trips "$OTHER" "{\"riderId\":\"$OTHER_ID\",\"pickupSavedAddressId\":\"$HOME_ID\",\"dropoff\":{\"latitude\":10.08,\"longitude\":10.08}}"
expect "request from home to work" 200 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickupSavedAddressId\":\"$HOME_ID\",\"dropoffSavedAddressId\":\"$WORK_ID\",\"pickup\":{\"latitude\":1,\"longitude\":1}}"
TRIP_ID="$(body_field 'd["trip"]["id"]')"
check "the saved point, not the one sent" "10.05 10.05" "$(body_field 'str(d["trip"]["pickup"]["latitude"]) + " " + str(d["trip"]["pickup"]["longitude"])')"
check "the addresses" "E2E Home, Gulan Street | E2E Work, 100m Street" "$(body_field 'd["trip"]["pickupAddress"] + " | " + d["trip"]["dropoffAddress"]')"
check "what helps the captain" "Building 4, floor 2 | Blue gate, ring twice | True" "$(body_field 'd["trip"]["pickupDetails"] + " | " + d["trip"]["pickupNote"] + " | " + str(d["trip"]["hasPickupPhoto"])')"

echo "==> [4/6] the pickup photo"
expect "while the trip is only requested" 400 GET "/v1/trips/$TRIP_ID/pickup-photo" "$RIDER"
grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" \
  -d "{\"trip_id\":\"$TRIP_ID\",\"driver_id\":\"$DRIVER_ID\"}" "$TRIP_ADDR" ride.trip.v1.TripService/AcceptTrip > /dev/null
expect "the driver, once accepted" 200 GET "/v1/trips/$TRIP_ID/pickup-photo" "$DRIVER"
check "the photo downloads" 200 "$(curl -sS -o "$WORK/seen.png" -w '%{http_code}' "$(body_field 'd["url"]')")"
check "and is an image" True "$(python3 -c "import sys; print(open(sys.argv[1],'rb').read(8) == b'\x89PNG\r\n\x1a\n')" "$WORK/seen.png")"
expect "the rider" 200 GET "/v1/trips/$TRIP_ID/pickup-photo" "$RIDER"
expect "another rider" 403 GET "/v1/trips/$TRIP_ID/pickup-photo" "$OTHER"
expect "the driver starts" 200 POST "/v1/trips/$TRIP_ID:start" "$DRIVER" '{}'
expect "the driver completes" 200 POST "/v1/trips/$TRIP_ID:complete" "$DRIVER" '{}'
expect "after the trip" 400 GET "/v1/trips/$TRIP_ID/pickup-photo" "$DRIVER"

echo "==> [5/6] recent destinations"
expect "the rider's recent destinations" 200 GET "/v1/riders/$RIDER_ID/recent-destinations" "$RIDER"
check "work is the latest" "E2E Work, 100m Street" "$(body_field 'd["destinations"][0]["address"] if d.get("destinations") else ""')"
expect "another rider's" 403 GET "/v1/riders/$RIDER_ID/recent-destinations" "$OTHER"

echo "==> [6/6] changing and deleting the photo"
PHOTO2="$(upload_photo "$RIDER" "$WORK/gate2.png")"
expect "a new photo" 200 PATCH "/v1/riders/$RIDER_ID/addresses/$HOME_ID" "$RIDER" "$HOME_BODY,\"photoMediaId\":\"$PHOTO2\"}"
check "the new one" "$PHOTO2" "$(body_field 'd["address"]["photoMediaId"]')"
expect "the old photo" 200 GET "/v1/media/$PHOTO1" "$RIDER"
check "is deleted" MEDIA_STATUS_DELETED "$(body_field 'd["media"]["status"]')"
expect "photo and removal at once" 400 PATCH "/v1/riders/$RIDER_ID/addresses/$HOME_ID" "$RIDER" "$HOME_BODY,\"photoMediaId\":\"$PHOTO2\",\"removePhoto\":true}"
expect "delete home" 200 DELETE "/v1/riders/$RIDER_ID/addresses/$HOME_ID" "$RIDER"
expect "its photo" 200 GET "/v1/media/$PHOTO2" "$RIDER"
check "is deleted too" MEDIA_STATUS_DELETED "$(body_field 'd["media"]["status"]')"
expect "home is gone" 404 GET "/v1/riders/$RIDER_ID/addresses/$HOME_ID" "$RIDER"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: saved addresses carry the captain's note and photo into a trip, and only to the people on it"
