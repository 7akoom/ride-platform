#!/usr/bin/env bash
# End-to-end test of the account details of riders and drivers, the
# notification badge and inbox pages, and a driver's name change, on the real
# services, through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-profile-details.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken), the object store on 127.0.0.1:8333 (what upload URLs
# point at in development), rider-service migration 00006, driver-service
# migration 00010 and notification-service migration 00015.
#
# What it proves:
#   1. a rider gives gender, date of birth and nationality; bad ones (unknown
#      gender, under 18, unknown country) are refused; nobody else reads them
#   2. a rider sets a profile photo from their own upload (someone else's file
#      is refused), reads its link, replaces it (the old file is deleted) and
#      removes it
#   3. a driver fills the details before approval; once approved, a given one
#      is locked and an empty one can still be filled
#   4. a driver's photo is the approved profile photo document
#   5. a name change: not before approval, one open at a time, not the same
#      name; operations approves (the name changes, the driver is told) or
#      rejects with a reason (the name stays); the driver cannot decide
#   6. the notification badge counts unread ones, the inbox pages with a
#      token, "mark all read" clears the badge, and nobody else reads them
#
# It creates throw-away identities, one rider, one driver and one staff member,
# and removes them again. Audit entries stay: the log is append-only by design.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
STORE="${S3_URL:-http://127.0.0.1:8333}"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
DRIVER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
OPS_IDENTITY="$(uuid)"
OPS_ID="$(uuid)"
PLATE="E2E-PROF-$RUN"
RIDER_ID=""
DRIVER_ID=""

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  sql ride-driver-postgres "delete from outbox_events where aggregate_id = '${DRIVER_ID:-00000000-0000-0000-0000-000000000000}'; delete from drivers where vehicle_plate_number = '$PLATE';" > /dev/null 2>&1 || true
  sql ride-rider-postgres "delete from riders where identity_id = '$RIDER_IDENTITY';" > /dev/null 2>&1 || true
  sql ride-staff-postgres "delete from staff_members where id = '$OPS_ID';" > /dev/null 2>&1 || true
  sql ride-media-postgres "delete from media_objects where owner_identity_id in ('$RIDER_IDENTITY', '$DRIVER_IDENTITY', '$OTHER_IDENTITY');" > /dev/null 2>&1 || true
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

years_ago() { # <years> [days added]
  python3 -c "import datetime,sys; t=datetime.date.today(); y=t.year-int(sys.argv[1]); d=t.replace(year=y) if not (t.month==2 and t.day==29) else t.replace(year=y, day=28); print(d + datetime.timedelta(days=int(sys.argv[2])))" "$1" "${2:-0}"
}

INBOX="recipient_type=RECIPIENT_TYPE_DRIVER&recipient_id"

# notified <event key>: True once the driver has that notification.
notified() {
  for _ in $(seq 1 20); do
    if [ "$(http GET "/v1/notifications?$INBOX=$DRIVER_ID&limit=100" "$DRIVER")" = 200 ] \
      && [ "$(body_field "any(n['eventKey'] == '$1' for n in d.get('notifications', []))")" = True ]; then
      echo True
      return
    fi
    sleep 1
  done
  echo False
}

echo "==> [0/6] preparing"
for check_table in "ride-rider-postgres rider_details 00006 rider-service" \
                   "ride-driver-postgres driver_name_changes 00010 driver-service"; do
  read -r container table migration service <<< "$check_table"
  [ "$(sql "$container" "select to_regclass('public.$table') is not null;")" = t ] \
    || { echo "ABORT: the $table table does not exist: apply $service migration $migration (goose up)" >&2; exit 2; }
done
[ "$(sql ride-notification-postgres "select count(*) from notification_templates where event_key like 'driver.name_change_%';")" = 2 ] \
  || { echo "ABORT: the name change templates are missing: apply notification-service migration 00015 (goose up)" >&2; exit 2; }

RIDER="$(mint "$RIDER_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
OPS="$(mint "$OPS_IDENTITY")"

sql ride-staff-postgres "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at) values
    ('$OPS_ID', '$OPS_IDENTITY', 'e2e-profile-ops-$RUN@ride.test', 'E2E Profile Ops', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values ('$OPS_ID', '$OPERATIONS_ROLE');" > /dev/null

must "creating the rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Profile Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
must "registering the driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Profile Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"

echo "==> [1/6] rider details"
R="/v1/riders/$RIDER_ID"
expect "empty details" 200 GET "$R/details" "$RIDER"
check "nothing given yet" "GENDER_UNSPECIFIED||False" "$(body_field 'd["details"].get("gender","GENDER_UNSPECIFIED") + "|" + d["details"].get("dateOfBirth","") + "|" + str(d["details"].get("hasPhoto", False))')"
expect "an unknown gender" 400 PATCH "$R/details" "$RIDER" '{"gender":7}'
expect "under 18" 400 PATCH "$R/details" "$RIDER" "{\"dateOfBirth\":\"$(years_ago 18 1)\"}"
expect "an unknown country" 400 PATCH "$R/details" "$RIDER" '{"nationality":"XX"}'
DOB="$(years_ago 30)"
expect "the rider gives the details" 200 PATCH "$R/details" "$RIDER" "{\"gender\":\"GENDER_FEMALE\",\"dateOfBirth\":\"$DOB\",\"nationality\":\"iq\"}"
check "stored as given, country in capitals" "GENDER_FEMALE|$DOB|IQ" "$(body_field 'd["details"]["gender"] + "|" + d["details"]["dateOfBirth"] + "|" + d["details"]["nationality"]')"
expect "only the nationality changes" 200 PATCH "$R/details" "$RIDER" '{"nationality":"TR"}'
check "the rest stays" "GENDER_FEMALE|$DOB|TR" "$(body_field 'd["details"]["gender"] + "|" + d["details"]["dateOfBirth"] + "|" + d["details"]["nationality"]')"
expect "someone else reads them" 403 GET "$R/details" "$OTHER"
expect "someone else changes them" 403 PATCH "$R/details" "$OTHER" '{"nationality":"SY"}'
expect "without a token" 401 GET "$R/details" ""

echo "==> [2/6] rider photo"
PHOTO1="$(upload "$RIDER" MEDIA_PURPOSE_PROFILE_PHOTO "$(picture rider-1)")"
PHOTO2="$(upload "$RIDER" MEDIA_PURPOSE_PROFILE_PHOTO "$(picture rider-2)")"
OTHERS_PHOTO="$(upload "$OTHER" MEDIA_PURPOSE_PROFILE_PHOTO "$(picture other)")"
expect "no photo yet" 404 GET "$R/photo" "$RIDER"
expect "someone else's file" 400 PUT "$R/photo" "$RIDER" "{\"mediaId\":\"$OTHERS_PHOTO\"}"
expect "the rider sets a photo" 200 PUT "$R/photo" "$RIDER" "{\"mediaId\":\"$PHOTO1\"}"
check "the details show it" True "$(body_field 'd["details"]["hasPhoto"]')"
expect "the photo link" 200 GET "$R/photo" "$RIDER"
check "a link that expires" True "$(body_field 'bool(d["url"]) and bool(d["expiresAt"])')"
expect "someone else reads the photo" 403 GET "$R/photo" "$OTHER"
expect "the rider replaces it" 200 PUT "$R/photo" "$RIDER" "{\"mediaId\":\"$PHOTO2\"}"
check "the old file is deleted" deleted "$(sql ride-media-postgres "select status from media_objects where id = '$PHOTO1';")"
expect "the rider removes it" 200 DELETE "$R/photo" "$RIDER"
check "no photo any more" False "$(body_field 'd["details"].get("hasPhoto", False)')"
check "that file is deleted too" deleted "$(sql ride-media-postgres "select status from media_objects where id = '$PHOTO2';")"
expect "no photo link" 404 GET "$R/photo" "$RIDER"

echo "==> [3/6] driver details"
D="/v1/drivers/$DRIVER_ID"
expect "before approval the driver gives a gender" 200 PATCH "$D/details" "$DRIVER" '{"gender":"GENDER_MALE"}'
expect "and changes it" 200 PATCH "$D/details" "$DRIVER" '{"gender":"GENDER_FEMALE"}'
expect "someone else reads them" 403 GET "$D/details" "$OTHER"
expect "operations reads them" 200 GET "$D/details" "$OPS"
sql ride-driver-postgres "update drivers set status = 'active' where id = '$DRIVER_ID';" > /dev/null
expect "once approved a given detail is locked" 400 PATCH "$D/details" "$DRIVER" '{"gender":"GENDER_MALE"}'
expect "an empty one can still be filled" 200 PATCH "$D/details" "$DRIVER" "{\"dateOfBirth\":\"$DOB\",\"nationality\":\"IQ\"}"
check "both stored" "GENDER_FEMALE|$DOB|IQ" "$(body_field 'd["details"]["gender"] + "|" + d["details"]["dateOfBirth"] + "|" + d["details"]["nationality"]')"

echo "==> [4/6] driver photo"
expect "no approved photo" 404 GET "$D/photo" "$DRIVER"
DRIVER_PHOTO="$(upload "$DRIVER" MEDIA_PURPOSE_PROFILE_PHOTO "$(picture driver)")"
must "handing in the photo" 200 POST "$D/documents" "$DRIVER" "{\"typeCode\":\"profile_photo\",\"mediaId\":\"$DRIVER_PHOTO\"}"
PHOTO_DOC="$(body_field 'd["document"]["id"]')"
expect "a photo waiting for review is not shown" 404 GET "$D/photo" "$DRIVER"
must "operations approves the photo" 200 POST "/v1/admin/driver-documents/$PHOTO_DOC:approve" "$OPS" '{}'
expect "the photo link" 200 GET "$D/photo" "$DRIVER"
check "a link that expires" True "$(body_field 'bool(d["url"]) and bool(d["expiresAt"])')"
expect "the details show it" 200 GET "$D/details" "$DRIVER"
check "has a photo" True "$(body_field 'd["details"]["hasPhoto"]')"

echo "==> [5/6] driver name change"
sql ride-driver-postgres "update drivers set status = 'pending' where id = '$DRIVER_ID';" > /dev/null
expect "not before approval" 400 POST "$D/name-changes" "$DRIVER" '{"requestedName":"Ali Hasan"}'
sql ride-driver-postgres "update drivers set status = 'active' where id = '$DRIVER_ID';" > /dev/null
expect "the same name" 400 POST "$D/name-changes" "$DRIVER" '{"requestedName":"  E2E   Profile Driver "}'
expect "an empty name" 400 POST "$D/name-changes" "$DRIVER" '{"requestedName":"   "}'
expect "someone else asks for the driver" 403 POST "$D/name-changes" "$OTHER" '{"requestedName":"Ali Hasan"}'
expect "the driver asks" 200 POST "$D/name-changes" "$DRIVER" '{"requestedName":" Ali  Hasan ","reason":"my name on the ID"}'
FIRST="$(body_field 'd["nameChange"]["id"]')"
check "waiting, tidied" "NAME_CHANGE_STATUS_PENDING|Ali Hasan|E2E Profile Driver" "$(body_field 'd["nameChange"]["status"] + "|" + d["nameChange"]["requestedName"] + "|" + d["nameChange"]["currentName"]')"
expect "a second one while it waits" 409 POST "$D/name-changes" "$DRIVER" '{"requestedName":"Someone Else"}'
expect "the driver lists the pending queue" 403 GET /v1/admin/driver-name-changes "$DRIVER"
expect "the driver approves it" 403 POST "/v1/admin/driver-name-changes/$FIRST:approve" "$DRIVER" '{}'
expect "operations lists the queue" 200 GET "/v1/admin/driver-name-changes?page_size=100" "$OPS"
check "it is there" True "$(body_field "any(c['id'] == '$FIRST' for c in d.get('nameChanges', []))")"
expect "operations approves it" 200 POST "/v1/admin/driver-name-changes/$FIRST:approve" "$OPS" '{}'
expect "approving twice" 400 POST "/v1/admin/driver-name-changes/$FIRST:approve" "$OPS" '{}'
expect "the driver" 200 GET "$D" "$DRIVER"
check "is renamed" "Ali Hasan" "$(body_field 'd["driver"]["displayName"]')"
check "the driver is told" True "$(notified driver.name_change_approved)"

expect "another request" 200 POST "$D/name-changes" "$DRIVER" '{"requestedName":"Someone Else"}'
SECOND="$(body_field 'd["nameChange"]["id"]')"
expect "a rejection without a reason" 400 POST "/v1/admin/driver-name-changes/$SECOND:reject" "$OPS" '{"reason":" "}'
expect "operations rejects it" 200 POST "/v1/admin/driver-name-changes/$SECOND:reject" "$OPS" '{"reason":"does not match the ID"}'
expect "an unknown request" 404 POST "/v1/admin/driver-name-changes/$(uuid):approve" "$OPS" '{}'
expect "the driver's history" 200 GET "$D/name-changes" "$DRIVER"
check "both, the rejection with its reason" "2|does not match the ID" "$(body_field "str(len(d['nameChanges'])) + '|' + next(c.get('rejectionReason','') for c in d['nameChanges'] if c['id'] == '$SECOND')")"
expect "the driver" 200 GET "$D" "$DRIVER"
check "keeps the approved name" "Ali Hasan" "$(body_field 'd["driver"]["displayName"]')"
check "the driver is told" True "$(notified driver.name_change_rejected)"

echo "==> [6/6] notification badge and pages"
expect "the badge" 200 GET "/v1/notifications:unread-count?$INBOX=$DRIVER_ID" "$DRIVER"
UNREAD="$(body_field 'd.get("unreadCount", 0)')"
check "counts at least the three notices" True "$( [ "$UNREAD" -ge 3 ] && echo True || echo False)"
expect "someone else's badge" 403 GET "/v1/notifications:unread-count?$INBOX=$DRIVER_ID" "$OTHER"
expect "page one" 200 GET "/v1/notifications?$INBOX=$DRIVER_ID&limit=2" "$DRIVER"
PAGE1="$(body_field '",".join(n["id"] for n in d["notifications"])')"
TOKEN="$(body_field 'd.get("nextPageToken", "")')"
check "two and a token" True "$( [ "$(echo "$PAGE1" | tr ',' '\n' | wc -l)" = 2 ] && [ -n "$TOKEN" ] && echo True || echo False)"
expect "page two" 200 GET "/v1/notifications?$INBOX=$DRIVER_ID&limit=2&page_token=$TOKEN" "$DRIVER"
check "other notifications than page one" True "$(body_field "len(d['notifications']) > 0 and not any(n['id'] in '$PAGE1'.split(',') for n in d['notifications'])")"
check "page two still counts the badge" "$UNREAD" "$(body_field 'd.get("unreadCount", 0)')"
expect "a made-up page token" 400 GET "/v1/notifications?$INBOX=$DRIVER_ID&page_token=nonsense" "$DRIVER"
expect "mark all read" 200 POST /v1/notifications:read "$DRIVER" "{\"recipientType\":\"RECIPIENT_TYPE_DRIVER\",\"recipientId\":\"$DRIVER_ID\"}"
check "all of them" "$UNREAD" "$(body_field 'd.get("markedCount", 0)')"
expect "the badge again" 200 GET "/v1/notifications:unread-count?$INBOX=$DRIVER_ID" "$DRIVER"
check "is empty" 0 "$(body_field 'd.get("unreadCount", 0)')"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: account details, photos, name changes and the notification badge work end to end"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
