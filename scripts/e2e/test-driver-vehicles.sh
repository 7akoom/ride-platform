#!/usr/bin/env bash
# End-to-end test of drivers' cars (driver-service + media-service +
# notification-service), on the real services, through the gateway. Run from
# the ride-platform repo root:
#   bash scripts/e2e/test-driver-vehicles.sh
#
# Needs the local identity signing key, the object store on 127.0.0.1:8333 and
# grpcurl, like test-driver-documents.sh.
#
# What it proves:
#   1. a driver registers with the year of their car; that car is their first,
#      active one, and approving the driver approves it
#   2. the driver adds another car: it needs its year, a plate nobody else
#      has, and waits for review; it cannot be made active, and the active car
#      cannot be retired
#   3. the new car's own documents are handed in for it (not for the active
#      car), and staff cannot approve the car until they are approved; staff
#      set the class; the driver is told
#   4. a rejected car is shown with its reason, and goes back to review once
#      the driver corrects it; staff work the car queue, the driver cannot
#   5. the driver switches cars only while offline: riders, dispatch and
#      pricing then see the new car (plate, class, year); the documents that
#      count are the new car's
#   6. the old car is retired with its documents, and its plate is free again
#
# It creates throw-away identities, one driver and two staff members, and
# removes them again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
STORE="${S3_URL:-http://127.0.0.1:8333}"
DRIVER_ADDR="localhost:50053"
PROTOSET="/tmp/ride.binpb"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
DRIVER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
OPS_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OPS_ID="$(uuid)"
OWNER_ID="$(uuid)"
PLATE_1="E2E-V1-$RUN"
PLATE_2="E2E-V2-$RUN"
PLATE_3="E2E-V3-$RUN"
DRIVER_ID=""

INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' services/driver-service/.env | tr -d '"')"
if [ -z "$INTERNAL_TOKEN" ]; then
  echo "ABORT: INTERNAL_SERVICE_TOKEN is missing from services/driver-service/.env" >&2
  exit 2
fi

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  [ -n "$DRIVER_ID" ] && sql ride-driver-postgres "delete from drivers where id = '$DRIVER_ID';" > /dev/null 2>&1 || true
  sql ride-staff-postgres "delete from staff_members where id in ('$OPS_ID', '$OWNER_ID');" > /dev/null 2>&1 || true
  sql ride-media-postgres "delete from media_objects where owner_identity_id in ('$DRIVER_IDENTITY', '$OTHER_IDENTITY');" > /dev/null 2>&1 || true
  [ -n "$DRIVER_ID" ] && sql ride-notification-postgres "delete from notifications where recipient_id = '$DRIVER_ID';" > /dev/null 2>&1 || true
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

must() { # <label> <expected status> <method> <path> <token> [json body]: stops on failure
  local actual
  actual="$(http "$3" "$4" "$5" "${6:-}")"

  if [ "$actual" != "$2" ]; then
    echo "FAIL: $1 answered $actual: $(head -c 300 "$BODY_FILE")" >&2
    exit 1
  fi
}

grpc_code() { # <token> <method> <json>
  local out
  out="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$3" "$DRIVER_ADDR" "ride.driver.v1.DriverService/$2" 2>&1)" && {
    echo OK
    return 0
  }

  echo "$out" | sed -n 's/^ *Code: *//p' | head -1
}

# upload <token> <purpose> <file>: reserves, sends and completes an upload;
# prints the READY media id.
upload() {
  local size id url args=()
  size="$(stat -c %s "$3")"

  must "reserving an upload" 200 POST /v1/media:upload "$1" "{\"purpose\":\"$2\",\"contentType\":\"image/png\",\"sizeBytes\":$size}"
  cp "$BODY_FILE" "$WORK/upload.json"
  id="$(body_field 'd["media"]["id"]')"
  url="$(body_field 'd["uploadUrl"]')"

  while IFS= read -r header; do
    [ -n "$header" ] && args+=(-H "$header")
  done < <(python3 -c '
import json, sys
for name, value in json.load(open(sys.argv[1])).get("uploadHeaders", {}).items():
    if name.lower() not in ("content-type", "content-length"):
        print(f"{name}: {value}")
' "$WORK/upload.json")

  local status
  status="$(curl -sS -o /dev/null -w '%{http_code}' -X PUT "${args[@]}" -H "Content-Type: image/png" --data-binary "@$3" "$url")"
  [ "$status" = 200 ] || { echo "FAIL: sending the file to $STORE answered $status" >&2; exit 1; }

  must "completing an upload" 200 POST "/v1/media/$id:complete" "$1" '{}'
  [ "$(body_field 'd["media"]["status"]')" = MEDIA_STATUS_READY ] || { echo "FAIL: the upload is not READY" >&2; exit 1; }

  echo "$id"
}

# picture <name>: a small distinct PNG, standard library only.
picture() {
  python3 - "$WORK/$1.png" "$1" <<'PY'
import struct, sys, zlib, hashlib
seed = hashlib.sha256(sys.argv[2].encode()).digest()
width, height = 48, 32
raw = b"".join(b"\x00" + b"".join(bytes((seed[(x + y) % 32], x * 5 % 256, y * 7 % 256)) for x in range(width)) for y in range(height))
def chunk(kind, data):
    return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
header = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
open(sys.argv[1], "wb").write(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))
PY
  echo "$WORK/$1.png"
}

submit() { # <type> <media id> <vehicle id> [number] [expires on]
  echo "{\"typeCode\":\"$1\",\"mediaId\":\"$2\",\"vehicleId\":\"$3\",\"documentNumber\":\"${4:-}\",\"expiresOn\":\"${5:-}\"}"
}

availability() {
  must "reading the driver" 200 GET "/v1/drivers/$DRIVER_ID" "$DRIVER"
  body_field 'd["driver"]["availabilityStatus"]'
}

# notified <event key>: True once the driver has that notification.
notified() {
  for _ in $(seq 1 20); do
    if [ "$(http GET "/v1/notifications?recipient_type=RECIPIENT_TYPE_DRIVER&recipient_id=$DRIVER_ID&limit=100" "$DRIVER")" = 200 ] \
      && [ "$(body_field "any(n['eventKey'] == '$1' for n in d.get('notifications', []))")" = True ]; then
      echo True
      return
    fi
    sleep 1
  done
  echo False
}

online() { echo '{"availabilityStatus":"AVAILABILITY_STATUS_AVAILABLE"}'; }
offline() { echo '{"availabilityStatus":"AVAILABILITY_STATUS_OFFLINE"}'; }

day() { python3 -c "import datetime,sys; print(datetime.date.today() + datetime.timedelta(days=int(sys.argv[1])))" "$1"; }


approve_documents() { # <driver id>: the driver's and the active car's, as a reviewer would leave them
  sql ride-driver-postgres "insert into driver_documents
      (id, driver_id, type_code, media_id, document_number, expires_on, status, reviewed_at, vehicle_id)
    select gen_random_uuid(), '$1', code, gen_random_uuid(),
           case when requires_number then 'E2E-' || left(md5('$1' || code), 12) else '' end,
           case when requires_expiry then current_date + 365 end,
           'approved', now(),
           case when scope = 'vehicle' then (select id from vehicles where driver_id = '$1' and active) end
    from driver_document_types
    where active and required
    on conflict do nothing;" > /dev/null
}

car() { # <plate> <year> [class]
  echo "{\"make\":\"Toyota\",\"model\":\"Camry\",\"color\":\"White\",\"plateNumber\":\"$1\",\"year\":$2,\"vehicleClass\":\"${3:-}\"}"
}

# car_field <vehicle id> <python expression over v>: from the driver's car list.
car_field() {
  must "listing the cars" 200 GET "/v1/drivers/$DRIVER_ID/vehicles" "$DRIVER"
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); v=next(x for x in d['vehicles'] if x['id'] == sys.argv[2]); print($2)" "$BODY_FILE" "$1"
}

echo "==> [1/6] the first car comes with the driver"
[ "$(sql ride-driver-postgres "select to_regclass('public.vehicles') is not null;")" = t ] \
  || { echo "ABORT: the vehicles table does not exist: apply driver-service migration 00009 (goose up)" >&2; exit 2; }
buf build -o "$PROTOSET"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
OPS="$(mint "$OPS_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"

sql ride-staff-postgres "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at) values
    ('$OPS_ID', '$OPS_IDENTITY', 'e2e-cars-ops-$RUN@ride.test', 'E2E Cars Ops', 'active', now(), now()),
    ('$OWNER_ID', '$OWNER_IDENTITY', 'e2e-cars-owner-$RUN@ride.test', 'E2E Cars Owner', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values
    ('$OPS_ID', '$OPERATIONS_ROLE'), ('$OWNER_ID', '$OWNER_ROLE');" > /dev/null

expect "a car from before 1980" 400 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Cars Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Red\",\"plateNumber\":\"$PLATE_1\",\"year\":1975}}"
expect "the driver registers with a 2019 car" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Cars Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Red\",\"plateNumber\":\"$PLATE_1\",\"year\":2019}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
FIRST_CAR="$(body_field 'd["driver"]["vehicle"]["id"]')"
check "the driver shows the car's year" 2019 "$(body_field 'd["driver"]["vehicle"]["year"]')"
check "the first car is active and waits with the driver" "VEHICLE_STATUS_PENDING True" "$(car_field "$FIRST_CAR" "v['status'] + ' ' + str(v.get('active', False))")"

approve_documents "$DRIVER_ID"
expect "operations approves the driver" 200 POST "/v1/admin/drivers/$DRIVER_ID:approve" "$OPS" '{}'
check "which approves the first car" VEHICLE_STATUS_APPROVED "$(car_field "$FIRST_CAR" "v['status']")"

echo "==> [2/6] another car"
expect "without its year" 400 POST "/v1/drivers/$DRIVER_ID/vehicles" "$DRIVER" "$(car "$PLATE_2" 0)"
expect "an unknown class" 400 POST "/v1/drivers/$DRIVER_ID/vehicles" "$DRIVER" "$(car "$PLATE_2" 2022 limo)"
expect "the plate of the first car" 409 POST "/v1/drivers/$DRIVER_ID/vehicles" "$DRIVER" "$(car "$PLATE_1" 2022)"
expect "someone else adds a car for the driver" 403 POST "/v1/drivers/$DRIVER_ID/vehicles" "$OTHER" "$(car "$PLATE_2" 2022)"
expect "the driver adds a 2022 car" 200 POST "/v1/drivers/$DRIVER_ID/vehicles" "$DRIVER" "$(car "$PLATE_2" 2022)"
NEW_CAR="$(body_field 'd["vehicle"]["id"]')"
check "it waits for review, not active" "VEHICLE_STATUS_PENDING False" "$(body_field 'd["vehicle"]["status"] + " " + str(d["vehicle"].get("active", False))')"
expect "making it active before review" 400 POST "/v1/drivers/$DRIVER_ID/vehicles/$NEW_CAR:activate" "$DRIVER" '{}'
expect "retiring the active car" 400 POST "/v1/drivers/$DRIVER_ID/vehicles/$FIRST_CAR:retire" "$DRIVER" '{}'
expect "operations reads the driver's cars" 200 GET "/v1/drivers/$DRIVER_ID/vehicles" "$OPS"
expect "another user reads them" 403 GET "/v1/drivers/$DRIVER_ID/vehicles" "$OTHER"

echo "==> [3/6] the new car's documents, and its review"
expect "the new car's requirements" 200 GET "/v1/drivers/$DRIVER_ID/documents?vehicle_id=$NEW_CAR" "$DRIVER"
check "only car documents, all missing" True "$(body_field "len(d['requirements']) > 0 and all(r['type'].get('scope') == 'vehicle' and r.get('vehicleId') == '$NEW_CAR' and r['state'] == 'DOCUMENT_REQUIREMENT_STATE_MISSING' for r in d['requirements'])")"
check "for that car" "$NEW_CAR" "$(body_field 'd["vehicle"]["id"]')"
body_field "'\\n'.join(r['type']['code'] + ' ' + ('MEDIA_PURPOSE_PROFILE_PHOTO' if r['type']['mediaPurpose'] == 'profile_photo' else 'MEDIA_PURPOSE_DRIVER_DOCUMENT') + ' ' + str(int(r['type'].get('requiresNumber', False))) + ' ' + str(int(r['type'].get('requiresExpiry', False))) for r in d['requirements'])" > "$WORK/car-types.txt"
expect "operations approves the car without its documents" 400 POST "/v1/admin/vehicles/$NEW_CAR:approve" "$OPS" '{}'

declare -A DOC
index=0
while read -r code purpose needs_number needs_expiry; do
  index=$((index + 1))
  file="$(upload "$DRIVER" "$purpose" "$(picture "car-$code")")"
  number=""
  expires=""
  [ "$needs_number" = 1 ] && number="CAR2-$RUN-$index"
  [ "$needs_expiry" = 1 ] && expires="$(day 400)"
  must "handing in $code for the new car" 200 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit "$code" "$file" "$NEW_CAR" "$number" "$expires")"
  [ "$(body_field 'd["document"]["vehicleId"]')" = "$NEW_CAR" ] || { echo "FAIL: $code was not handed in for the new car" >&2; exit 1; }
  DOC[$code]="$(body_field 'd["document"]["id"]')"
done < "$WORK/car-types.txt"
printf '  ok    handed in %d documents for the new car\n' "$index"

expect "the driver's own requirements" 200 GET "/v1/drivers/$DRIVER_ID/documents" "$DRIVER"
check "still count the first car" True "$(body_field "all(r.get('vehicleId') == '$FIRST_CAR' for r in d['requirements'] if r['type'].get('scope') == 'vehicle')")"
check "and the driver still may work" True "$(body_field 'd.get("compliant", False)')"

for code in "${!DOC[@]}"; do
  must "approving $code" 200 POST "/v1/admin/driver-documents/${DOC[$code]}:approve" "$OPS" '{}'
done
expect "a bad year from the reviewer" 400 POST "/v1/admin/vehicles/$NEW_CAR:approve" "$OPS" '{"year":1900}'
expect "the driver approves their own car" 403 POST "/v1/admin/vehicles/$NEW_CAR:approve" "$DRIVER" '{}'
expect "operations approves the car as comfort" 200 POST "/v1/admin/vehicles/$NEW_CAR:approve" "$OPS" '{"vehicleClass":"comfort"}'
check "approved, comfort" "VEHICLE_STATUS_APPROVED comfort" "$(body_field 'd["vehicle"]["status"] + " " + d["vehicle"]["vehicleClass"]')"
expect "approving it again" 400 POST "/v1/admin/vehicles/$NEW_CAR:approve" "$OPS" '{}'
check "the driver is told" True "$(notified driver.vehicle_approved)"

echo "==> [4/6] a rejected car, and the queue"
expect "the driver adds a third car" 200 POST "/v1/drivers/$DRIVER_ID/vehicles" "$DRIVER" "$(car "$PLATE_3" 2021)"
THIRD_CAR="$(body_field 'd["vehicle"]["id"]')"
expect "operations reads the car queue" 200 GET "/v1/admin/vehicles?page_size=100" "$OPS"
check "the third car is in it, with the driver's name" True "$(body_field "any(x['vehicle']['id'] == '$THIRD_CAR' and x['driverDisplayName'] == 'E2E Cars Driver' for x in d.get('vehicles', []))")"
expect "the driver reads the queue" 403 GET /v1/admin/vehicles "$DRIVER"
expect "a rejection without a reason" 400 POST "/v1/admin/vehicles/$THIRD_CAR:reject" "$OPS" '{}'
expect "operations rejects it" 200 POST "/v1/admin/vehicles/$THIRD_CAR:reject" "$OPS" '{"reason":"The plate does not match the registration"}'
check "rejected, with the reason" "VEHICLE_STATUS_REJECTED The plate does not match the registration" "$(car_field "$THIRD_CAR" "v['status'] + ' ' + v.get('rejectionReason', '')")"
check "the driver is told" True "$(notified driver.vehicle_rejected)"
expect "the driver corrects it" 200 PATCH "/v1/drivers/$DRIVER_ID/vehicles/$THIRD_CAR" "$DRIVER" "$(car "$PLATE_3-X" 2021)"
check "it waits for review again" "VEHICLE_STATUS_PENDING $PLATE_3-X" "$(body_field 'd["vehicle"]["status"] + " " + d["vehicle"]["plateNumber"]')"
expect "the driver changes an approved car" 400 PATCH "/v1/drivers/$DRIVER_ID/vehicles/$NEW_CAR" "$DRIVER" "$(car "$PLATE_2" 2020)"

echo "==> [5/6] switching cars"
expect "the driver goes online" 200 PUT "/v1/drivers/$DRIVER_ID/availability" "$DRIVER" "$(online)"
expect "switching cars while online" 400 POST "/v1/drivers/$DRIVER_ID/vehicles/$NEW_CAR:activate" "$DRIVER" '{}'
expect "the driver goes offline" 200 PUT "/v1/drivers/$DRIVER_ID/availability" "$DRIVER" "$(offline)"
expect "switching to the new car" 200 POST "/v1/drivers/$DRIVER_ID/vehicles/$NEW_CAR:activate" "$DRIVER" '{}'
check "it is active" True "$(body_field 'str(d["vehicle"].get("active", False))')"
expect "the driver's profile" 200 GET "/v1/drivers/$DRIVER_ID" "$DRIVER"
check "shows the new car to riders, dispatch and pricing" "$NEW_CAR $PLATE_2 comfort 2022" "$(body_field 'd["driver"]["vehicle"]["id"] + " " + d["driver"]["vehicle"]["plateNumber"] + " " + d["driver"]["vehicle"]["vehicleClass"] + " " + str(d["driver"]["vehicle"]["year"])')"
check "the first car is no longer active" False "$(car_field "$FIRST_CAR" "str(v.get('active', False))")"
expect "the driver's requirements" 200 GET "/v1/drivers/$DRIVER_ID/documents" "$DRIVER"
check "now count the new car" True "$(body_field "all(r.get('vehicleId') == '$NEW_CAR' for r in d['requirements'] if r['type'].get('scope') == 'vehicle')")"
check "and the driver may work" True "$(body_field 'd.get("compliant", False)')"
expect "the driver goes online with it" 200 PUT "/v1/drivers/$DRIVER_ID/availability" "$DRIVER" "$(online)"
expect "and offline" 200 PUT "/v1/drivers/$DRIVER_ID/availability" "$DRIVER" "$(offline)"

echo "==> [6/6] retiring the old car"
expect "the driver retires the first car" 200 POST "/v1/drivers/$DRIVER_ID/vehicles/$FIRST_CAR:retire" "$DRIVER" '{}'
check "retired" VEHICLE_STATUS_RETIRED "$(body_field 'd["vehicle"]["status"]')"
check "its documents went with it" 0 "$(sql ride-driver-postgres "select count(*) from driver_documents where vehicle_id = '$FIRST_CAR' and status <> 'superseded';")"
expect "retiring it again" 400 POST "/v1/drivers/$DRIVER_ID/vehicles/$FIRST_CAR:retire" "$DRIVER" '{}'
expect "its plate is free for a new car" 200 POST "/v1/drivers/$DRIVER_ID/vehicles" "$DRIVER" "$(car "$PLATE_1" 2019)"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: drivers add cars, staff review them with their documents, and drivers switch and retire them"
