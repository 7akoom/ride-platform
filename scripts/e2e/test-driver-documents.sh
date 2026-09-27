#!/usr/bin/env bash
# End-to-end test of driver documents (driver-service + media-service +
# notification-service), on the real services, through the gateway. Run from
# the ride-platform repo root:
#   bash scripts/e2e/test-driver-documents.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken), the object store on 127.0.0.1:8333 (what upload URLs
# point at in development) and grpcurl. It waits for the expiry check, which
# runs every DRIVER_DOCUMENTS_CHECK_INTERVAL (1m by default): about a minute.
#
# What it proves:
#   1. the driver sees the document types; only staff with drivers.configure
#      change them, and an inactive type is only shown to staff
#   2. the driver uploads a file per type and hands it in; a missing number,
#      a past or unreadable expiry, someone else's file, a file used twice and
#      an unknown type are refused; nobody else reads the driver's documents
#   3. until every required document is approved the driver cannot be
#      approved or go online
#   4. staff (operations) work the review queue and view the files; the driver
#      cannot review; a rejection needs a reason and is shown; a resubmission
#      replaces the rejected one and its file is deleted; the reviewer can
#      correct the expiry date
#   5. then the driver is approved, goes online, is told (notifications), and
#      can no longer change their name
#   6. a renewal waits beside the approved one and replaces it once approved
#   7. withdrawing an approved document takes an online driver offline
#   8. the expiry check reminds before a document runs out, takes the driver
#      offline when it has, and a driver released from a trip meanwhile ends
#      up offline rather than available
#
# It creates throw-away identities, one driver, two staff members and one
# inactive document type, and removes them again. Audit entries stay: the log
# is append-only by design.
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
PLATE="E2E-DOCS-$RUN"
EXTRA_TYPE="e2e_permit_$RUN"
DRIVER_ID=""
LICENCE_NUMBER=""

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
  sql ride-driver-postgres "delete from drivers where vehicle_plate_number = '$PLATE'; delete from driver_document_types where code = '$EXTRA_TYPE';" > /dev/null 2>&1 || true
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

submit() { # <type> <media id> [number] [expires on]
  echo "{\"typeCode\":\"$1\",\"mediaId\":\"$2\",\"documentNumber\":\"${3:-}\",\"expiresOn\":\"${4:-}\"}"
}

# requirement <python expression over r> [type]: over the requirement of a type
# (the licence by default) in the last documents answer.
requirement() {
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); r=next(x for x in d['requirements'] if x['type']['code'] == sys.argv[2]); print($1)" "$BODY_FILE" "${2:-$LICENCE}"
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

echo "==> [1/8] document types"
[ "$(sql ride-driver-postgres "select to_regclass('public.driver_documents') is not null;")" = t ] \
  || { echo "ABORT: the driver_documents table does not exist: apply driver-service migration 00008 (goose up)" >&2; exit 2; }
buf build -o "$PROTOSET"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
OPS="$(mint "$OPS_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"

sql ride-staff-postgres "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at) values
    ('$OPS_ID', '$OPS_IDENTITY', 'e2e-docs-ops-$RUN@ride.test', 'E2E Docs Ops', 'active', now(), now()),
    ('$OWNER_ID', '$OWNER_IDENTITY', 'e2e-docs-owner-$RUN@ride.test', 'E2E Docs Owner', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values
    ('$OPS_ID', '$OPERATIONS_ROLE'), ('$OWNER_ID', '$OWNER_ROLE');" > /dev/null

must "registering the driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Documents Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"

expect "the driver lists the document types" 200 GET /v1/driver-document-types "$DRIVER"
cp "$BODY_FILE" "$WORK/types.json"
python3 - "$WORK/types.json" > "$WORK/types.txt" <<'PY'
import json, sys
for t in json.load(open(sys.argv[1]))["types"]:
    purpose = "MEDIA_PURPOSE_PROFILE_PHOTO" if t["mediaPurpose"] == "profile_photo" else "MEDIA_PURPOSE_DRIVER_DOCUMENT"
    print(t["code"], purpose, int(t.get("requiresNumber", False)), int(t.get("requiresExpiry", False)), int(t.get("required", False)))
PY
check "types are listed" True "$( [ "$(wc -l < "$WORK/types.txt")" -gt 0 ] && echo True || echo False)"
LICENCE="$(awk '$3 == 1 && $4 == 1 && $5 == 1 {print $1; exit}' "$WORK/types.txt")"
SECOND_EXPIRING="$(awk -v l="$LICENCE" '$4 == 1 && $5 == 1 && $1 != l {print $1; exit}' "$WORK/types.txt")"
[ -n "$LICENCE" ] && [ -n "$SECOND_EXPIRING" ] || { echo "ABORT: needs two required types with an expiry, one with a number" >&2; exit 2; }
echo "  (licence-like type: $LICENCE; another expiring type: $SECOND_EXPIRING)"

TYPE_BODY="{\"type\":{\"mediaPurpose\":\"driver_document\",\"nameEn\":\"E2E permit\",\"nameAr\":\"تصريح\",\"nameKu\":\"مۆڵەت\",\"required\":true,\"active\":false,\"sortOrder\":999}}"
expect "the driver changes a type" 403 PUT "/v1/admin/driver-document-types/$EXTRA_TYPE" "$DRIVER" "$TYPE_BODY"
expect "operations (no drivers.configure) changes a type" 403 PUT "/v1/admin/driver-document-types/$EXTRA_TYPE" "$OPS" "$TYPE_BODY"
expect "the owner adds an inactive type" 200 PUT "/v1/admin/driver-document-types/$EXTRA_TYPE" "$OWNER" "$TYPE_BODY"
expect "a bad type code" 400 PUT "/v1/admin/driver-document-types/Bad%20Code" "$OWNER" "$TYPE_BODY"
expect "staff list every type" 200 GET /v1/admin/driver-document-types "$OPS"
check "staff see the inactive type" True "$(body_field "any(t['code'] == '$EXTRA_TYPE' for t in d['types'])")"
expect "the driver lists the types again" 200 GET /v1/driver-document-types "$DRIVER"
check "drivers do not see it" False "$(body_field "any(t['code'] == '$EXTRA_TYPE' for t in d['types'])")"
expect "the driver lists every type" 403 GET /v1/admin/driver-document-types "$DRIVER"

echo "==> [2/8] handing documents in"
FUTURE="$(day 700)"
LATER="$(day 900)"
LICENCE_FILE="$(upload "$DRIVER" MEDIA_PURPOSE_DRIVER_DOCUMENT "$(picture licence-1)")"
OTHERS_FILE="$(upload "$OTHER" MEDIA_PURPOSE_DRIVER_DOCUMENT "$(picture other)")"

expect "without its number" 400 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit "$LICENCE" "$LICENCE_FILE" "" "$FUTURE")"
expect "already out of date" 400 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit "$LICENCE" "$LICENCE_FILE" "L-$RUN" "$(day -1)")"
expect "an unreadable date" 400 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit "$LICENCE" "$LICENCE_FILE" "L-$RUN" "31/12/2030")"
expect "someone else's file" 400 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit "$LICENCE" "$OTHERS_FILE" "L-$RUN" "$FUTURE")"
expect "an unknown type" 404 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit passport "$LICENCE_FILE")"
expect "an inactive type" 404 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit "$EXTRA_TYPE" "$LICENCE_FILE")"
expect "another user hands in for the driver" 403 POST "/v1/drivers/$DRIVER_ID/documents" "$OTHER" "$(submit "$LICENCE" "$OTHERS_FILE" "L-$RUN" "$FUTURE")"

declare -A FILE DOC
index=0
while read -r code purpose needs_number needs_expiry required; do
  index=$((index + 1))

  if [ "$code" = "$LICENCE" ]; then
    FILE[$code]="$LICENCE_FILE"
  else
    FILE[$code]="$(upload "$DRIVER" "$purpose" "$(picture "$code")")"
  fi

  number=""
  expires=""
  [ "$needs_number" = 1 ] && number="E2E-$RUN-$index"
  [ "$code" = "$LICENCE" ] && LICENCE_NUMBER="$number"
  [ "$needs_expiry" = 1 ] && expires="$FUTURE"

  must "handing in $code" 200 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit "$code" "${FILE[$code]}" "$number" "$expires")"
  DOC[$code]="$(body_field 'd["document"]["id"]')"
done < "$WORK/types.txt"
printf '  ok    handed in %d documents\n' "$index"

expect "the same file for another type" 409 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit "$SECOND_EXPIRING" "$LICENCE_FILE" "N-$RUN" "$FUTURE")"
expect "the driver reads their documents" 200 GET "/v1/drivers/$DRIVER_ID/documents" "$DRIVER"
check "not compliant yet" False "$(body_field 'd.get("compliant", False)')"
check "everything waits for review" True "$(body_field "all(r['state'] == 'DOCUMENT_REQUIREMENT_STATE_PENDING_REVIEW' for r in d['requirements'])")"
check "the licence keeps its number" "$LICENCE_NUMBER" "$(requirement "r['pending']['documentNumber']")"
expect "another user reads them" 403 GET "/v1/drivers/$DRIVER_ID/documents" "$OTHER"

echo "==> [3/8] no approval, no work, until the documents are approved"
expect "operations approves the driver" 400 POST "/v1/admin/drivers/$DRIVER_ID:approve" "$OPS" '{}'
check "the internal token approves the driver" FailedPrecondition "$(grpc_code "$INTERNAL_TOKEN" ApproveDriver "{\"driver_id\":\"$DRIVER_ID\"}")"
expect "a pending driver goes online" 400 PUT "/v1/drivers/$DRIVER_ID/availability" "$DRIVER" "$(online)"

echo "==> [4/8] review"
expect "operations reads the queue" 200 GET "/v1/admin/driver-documents?page_size=100" "$OPS"
check "the driver's documents are in it, with their name" True "$(body_field "any(x['document']['driverId'] == '$DRIVER_ID' and x['driverDisplayName'] == 'E2E Documents Driver' for x in d.get('documents', []))")"
expect "the driver reads the queue" 403 GET "/v1/admin/driver-documents" "$DRIVER"
expect "operations reads the driver's documents" 200 GET "/v1/drivers/$DRIVER_ID/documents" "$OPS"
expect "operations views the licence file" 200 GET "/v1/media/$LICENCE_FILE:download" "$OPS"
expect "the driver approves their own document" 403 POST "/v1/admin/driver-documents/${DOC[$LICENCE]}:approve" "$DRIVER" '{}'
expect "a rejection without a reason" 400 POST "/v1/admin/driver-documents/${DOC[$LICENCE]}:reject" "$OPS" '{}'
expect "operations rejects the licence" 200 POST "/v1/admin/driver-documents/${DOC[$LICENCE]}:reject" "$OPS" '{"reason":"The photo is blurry"}'
expect "the driver reads their documents" 200 GET "/v1/drivers/$DRIVER_ID/documents" "$DRIVER"
check "the licence is rejected" DOCUMENT_REQUIREMENT_STATE_REJECTED "$(requirement "r['state']")"
check "with the reason" "The photo is blurry" "$(requirement "r['rejected']['rejectionReason']")"

NEW_LICENCE_FILE="$(upload "$DRIVER" MEDIA_PURPOSE_DRIVER_DOCUMENT "$(picture licence-2)")"
expect "the driver hands the licence in again" 200 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit "$LICENCE" "$NEW_LICENCE_FILE" "E2E-$RUN-L" "$FUTURE")"
DOC[$LICENCE]="$(body_field 'd["document"]["id"]')"
expect "the driver reads their documents" 200 GET "/v1/drivers/$DRIVER_ID/documents" "$DRIVER"
check "the licence waits for review again" DOCUMENT_REQUIREMENT_STATE_PENDING_REVIEW "$(requirement "r['state']")"
check "the old rejection is history" False "$(requirement "r.get('rejected') is not None")"
expect "the driver reads the rejected file" 200 GET "/v1/media/$LICENCE_FILE" "$DRIVER"
check "it was deleted" MEDIA_STATUS_DELETED "$(body_field 'd["media"]["status"]')"

for code in "${!DOC[@]}"; do
  if [ "$code" = "$LICENCE" ]; then
    expect "operations approves the licence, correcting the date" 200 POST "/v1/admin/driver-documents/${DOC[$code]}:approve" "$OPS" "{\"expiresOn\":\"$LATER\"}"
    check "with the corrected date" "$LATER" "$(body_field 'd["document"]["expiresOn"]')"
  else
    must "approving $code" 200 POST "/v1/admin/driver-documents/${DOC[$code]}:approve" "$OPS" '{}'
  fi
done
expect "approving twice" 400 POST "/v1/admin/driver-documents/${DOC[$LICENCE]}:approve" "$OPS" '{}'
expect "the driver reads their documents" 200 GET "/v1/drivers/$DRIVER_ID/documents" "$DRIVER"
check "compliant" True "$(body_field 'd.get("compliant", False)')"
check "the licence is approved" DOCUMENT_REQUIREMENT_STATE_APPROVED "$(requirement "r['state']")"

echo "==> [5/8] approval, work, notifications"
expect "operations approves the driver" 200 POST "/v1/admin/drivers/$DRIVER_ID:approve" "$OPS" '{}'
check "active" DRIVER_STATUS_ACTIVE "$(body_field 'd["driver"]["status"]')"
expect "the driver goes online" 200 PUT "/v1/drivers/$DRIVER_ID/availability" "$DRIVER" "$(online)"
expect "and offline" 200 PUT "/v1/drivers/$DRIVER_ID/availability" "$DRIVER" "$(offline)"
check "told a document was turned down" True "$(notified driver.document_rejected)"
check "told a document was approved" True "$(notified driver.document_approved)"
check "told the account was approved" True "$(notified driver.account_approved)"
expect "the driver renames themselves" 400 PATCH "/v1/drivers/$DRIVER_ID" "$DRIVER" "{\"displayName\":\"Someone Else\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\"}}"
expect "sending the profile unchanged" 200 PATCH "/v1/drivers/$DRIVER_ID" "$DRIVER" "{\"displayName\":\"E2E Documents Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\"}}"

echo "==> [6/8] a renewal"
OLD_LICENCE="${DOC[$LICENCE]}"
RENEWAL_FILE="$(upload "$DRIVER" MEDIA_PURPOSE_DRIVER_DOCUMENT "$(picture licence-3)")"
expect "the driver hands in a renewed licence" 200 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit "$LICENCE" "$RENEWAL_FILE" "E2E-$RUN-L" "$(day 1000)")"
DOC[$LICENCE]="$(body_field 'd["document"]["id"]')"
expect "the driver reads their documents" 200 GET "/v1/drivers/$DRIVER_ID/documents" "$DRIVER"
check "the old one still counts" "DOCUMENT_REQUIREMENT_STATE_APPROVED $OLD_LICENCE" "$(requirement "r['state'] + ' ' + r['approved']['id']")"
check "the renewal waits beside it" "${DOC[$LICENCE]}" "$(requirement "r['pending']['id']")"
check "still compliant" True "$(body_field 'd.get("compliant", False)')"
expect "operations approves the renewal" 200 POST "/v1/admin/driver-documents/${DOC[$LICENCE]}:approve" "$OPS" '{}'
expect "the driver reads their history" 200 GET "/v1/drivers/$DRIVER_ID/documents?include_history=true" "$DRIVER"
check "the renewal is in force" "${DOC[$LICENCE]}" "$(requirement "r['approved']['id']")"
check "the old one is superseded" DRIVER_DOCUMENT_STATUS_SUPERSEDED "$(body_field "next(h['status'] for h in d['history'] if h['id'] == '$OLD_LICENCE')")"

echo "==> [7/8] withdrawing an approved document"
expect "the driver goes online" 200 PUT "/v1/drivers/$DRIVER_ID/availability" "$DRIVER" "$(online)"
expect "operations withdraws the licence" 200 POST "/v1/admin/driver-documents/${DOC[$LICENCE]}:reject" "$OPS" '{"reason":"The licence was reported stolen"}'
check "the driver was taken offline" AVAILABILITY_STATUS_OFFLINE "$(availability)"
expect "going online again" 400 PUT "/v1/drivers/$DRIVER_ID/availability" "$DRIVER" "$(online)"
check "told the document was withdrawn" True "$(notified driver.document_withdrawn)"
RESTORE_FILE="$(upload "$DRIVER" MEDIA_PURPOSE_DRIVER_DOCUMENT "$(picture licence-4)")"
must "handing a licence in again" 200 POST "/v1/drivers/$DRIVER_ID/documents" "$DRIVER" "$(submit "$LICENCE" "$RESTORE_FILE" "E2E-$RUN-L" "$FUTURE")"
DOC[$LICENCE]="$(body_field 'd["document"]["id"]')"
must "approving it" 200 POST "/v1/admin/driver-documents/${DOC[$LICENCE]}:approve" "$OPS" '{}'
expect "back online" 200 PUT "/v1/drivers/$DRIVER_ID/availability" "$DRIVER" "$(online)"

echo "==> [8/8] expiry (waits for the expiry check, about a minute)"
sql ride-driver-postgres "
  update driver_documents set expires_on = current_date - 1, expired_notified_at = null
   where id = '${DOC[$LICENCE]}';
  update driver_documents set expires_on = current_date + 5, reminded_days = null
   where driver_id = '$DRIVER_ID' and type_code = '$SECOND_EXPIRING' and status = 'approved';" > /dev/null

went_offline=False
for _ in $(seq 1 45); do
  if [ "$(availability)" = AVAILABILITY_STATUS_OFFLINE ]; then
    went_offline=True
    break
  fi
  sleep 2
done
check "the driver was taken offline when the licence ran out" True "$went_offline"
check "told the licence ran out" True "$(notified driver.document_expired)"
check "reminded before the other runs out" True "$(notified driver.document_expiring)"
expect "the driver reads their documents" 200 GET "/v1/drivers/$DRIVER_ID/documents" "$DRIVER"
check "the licence is out of date" "DOCUMENT_REQUIREMENT_STATE_EXPIRED True" "$(requirement "r['state'] + ' ' + str(r['approved'].get('expired', False))")"
check "the other is expiring soon" DOCUMENT_REQUIREMENT_STATE_EXPIRING_SOON "$(requirement "r['state']" "$SECOND_EXPIRING")"
check "no longer compliant" False "$(body_field 'd.get("compliant", False)')"
expect "going online" 400 PUT "/v1/drivers/$DRIVER_ID/availability" "$DRIVER" "$(online)"

sql ride-driver-postgres "update drivers set availability_status = 'busy' where id = '$DRIVER_ID';" > /dev/null
check "a trip ends: the release to available" OK "$(grpc_code "$INTERNAL_TOKEN" UpdateAvailability "{\"driver_id\":\"$DRIVER_ID\",\"availability_status\":\"AVAILABILITY_STATUS_AVAILABLE\"}")"
check "leaves the driver offline instead" AVAILABILITY_STATUS_OFFLINE "$(availability)"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: drivers hand documents in, staff review them, and expired or missing ones keep a driver offline"
