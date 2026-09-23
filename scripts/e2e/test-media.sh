#!/usr/bin/env bash
# End-to-end test of file uploads (media-service + SeaweedFS), on the real
# services, through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-media.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken), and the object store reachable on 127.0.0.1:8333,
# which is what the upload URLs point at in development.
#
# What it proves:
#   1. a user reserves an upload, sends the bytes straight to the object store
#      with the signed URL (another type is refused), completes it, and
#      downloads a re-encoded copy: bytes hidden after the image are gone
#   2. a second PUT with the still-valid upload URL does not change the file
#      that is served
#   3. another user cannot read, download, complete or delete it, and a file
#      that does not exist looks the same (403); the bucket refuses unsigned
#      requests
#   4. staff with media.read (the operations role) can view it, and it is in
#      the audit log; they cannot delete it
#   5. a file whose content is not its declared type, and a PDF with
#      JavaScript, are rejected; wrong types and sizes are refused up front
#   6. only services hold and release a file (internal token); a held file
#      cannot be deleted by its owner
#
# It creates throw-away identities and one throw-away staff member, and
# removes them again. Audit entries stay: the log is append-only by design.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
STORE="${S3_URL:-http://127.0.0.1:8333}"
MEDIA_ADDR="localhost:50062"
PROTOSET="/tmp/ride.binpb"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
OWNER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
STAFF_IDENTITY="$(uuid)"
STAFF_ID="$(uuid)"

INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' services/media-service/.env | tr -d '"')"
if [ -z "$INTERNAL_TOKEN" ]; then
  echo "ABORT: INTERNAL_SERVICE_TOKEN is missing from services/media-service/.env" >&2
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
  sql ride-staff-postgres "delete from staff_members where id = '$STAFF_ID';" > /dev/null 2>&1 || true
  sql ride-media-postgres "delete from media_objects where owner_identity_id in ('$OWNER_IDENTITY', '$OTHER_IDENTITY');" > /dev/null 2>&1 || true
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

grpc_code() { # <token> <service/method> <json>
  local out
  out="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$3" "$MEDIA_ADDR" "$2" 2>&1)" && {
    echo OK
    return 0
  }

  echo "$out" | sed -n 's/^ *Code: *//p' | head -1
}

# reserve <token> <purpose> <content type> <file>: reserves an upload for the
# file's size; leaves the answer in $BODY_FILE and prints the status.
reserve() {
  local size
  size="$(stat -c %s "$4")"
  http POST /v1/media:upload "$1" "{\"purpose\":\"$2\",\"contentType\":\"$3\",\"sizeBytes\":$size}"
}

# send <upload answer json> <content type> <file>: PUTs the file to the signed
# URL with the signed headers (Content-Type overridden), prints the status.
send() {
  local url args=()
  url="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["uploadUrl"])' "$1")"

  while IFS= read -r header; do
    [ -n "$header" ] && args+=(-H "$header")
  done < <(python3 -c '
import json, sys
for name, value in json.load(open(sys.argv[1])).get("uploadHeaders", {}).items():
    if name.lower() not in ("content-type", "content-length"):
        print(f"{name}: {value}")
' "$1")

  curl -sS -o /dev/null -w '%{http_code}' -X PUT "${args[@]}" -H "Content-Type: $2" --data-binary "@$3" "$url"
}

fetch() { # <url> <output file>: prints the status
  curl -sS -o "$2" -w '%{http_code}' "$1"
}

# Sample files, made with the standard library only.
python3 - "$WORK" <<'PY'
import struct, sys, zlib

work = sys.argv[1]

def png(width, height):
    raw = b"".join(
        b"\x00" + b"".join(bytes((x * 5 % 256, y * 7 % 256, 120)) for x in range(width))
        for y in range(height))
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
    header = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")

open(f"{work}/photo.png", "wb").write(png(64, 48) + b"E2E-HIDDEN-TRAILER <script>alert(1)</script>")
open(f"{work}/fake.png", "wb").write(b"<html><body>not a picture</body></html>")
open(f"{work}/script.pdf", "wb").write(
    b"%PDF-1.4\n1 0 obj << /Type /Catalog /OpenAction << /S /JavaScript /JS (app.alert(1)) >> >> endobj\n"
    b"trailer << /Root 1 0 R >>\n%%EOF\n")
PY

echo "==> [1/6] upload, complete, download"
buf build -o "$PROTOSET"
OWNER="$(mint "$OWNER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"

check "reserve a driver document" 200 "$(reserve "$OWNER" MEDIA_PURPOSE_DRIVER_DOCUMENT image/png "$WORK/photo.png")"
cp "$BODY_FILE" "$WORK/upload.json"
MEDIA_ID="$(body_field 'd["media"]["id"]')"
check "the reservation is pending and the caller's" "MEDIA_STATUS_PENDING $OWNER_IDENTITY" "$(body_field 'd["media"]["status"] + " " + d["media"]["ownerIdentityId"]')"
check "the upload URL points at the object store" True "$(body_field "d['uploadUrl'].startswith('$STORE/')")"

check "PUT with another content type" 403 "$(send "$WORK/upload.json" image/jpeg "$WORK/photo.png")"
check "PUT the file" 200 "$(send "$WORK/upload.json" image/png "$WORK/photo.png")"

expect "another user completes it" 403 POST "/v1/media/$MEDIA_ID:complete" "$OTHER" '{}'
expect "complete" 200 POST "/v1/media/$MEDIA_ID:complete" "$OWNER" '{}'
check "ready, 64x48" "MEDIA_STATUS_READY 64x48" "$(body_field 'd["media"]["status"] + " " + str(d["media"]["width"]) + "x" + str(d["media"]["height"])')"
SHA="$(body_field 'd["media"]["sha256"]')"
expect "complete again" 200 POST "/v1/media/$MEDIA_ID:complete" "$OWNER" '{}'
check "the same file" "$SHA" "$(body_field 'd["media"]["sha256"]')"

expect "read it" 200 GET "/v1/media/$MEDIA_ID" "$OWNER"
expect "download URL" 200 GET "/v1/media/$MEDIA_ID:download" "$OWNER"
check "download" 200 "$(fetch "$(body_field 'd["url"]')" "$WORK/served.png")"
check "the served file is the checked copy" "$SHA" "$(sha256sum "$WORK/served.png" | cut -d' ' -f1)"
check "the hidden trailer is gone" 0 "$(grep -c 'E2E-HIDDEN-TRAILER' "$WORK/served.png" || true)"

echo "==> [2/6] a second PUT does not replace the checked file"
head -c "$(stat -c %s "$WORK/photo.png")" /dev/zero | tr '\0' 'X' > "$WORK/replacement.png"
check "PUT again with the same URL" 200 "$(send "$WORK/upload.json" image/png "$WORK/replacement.png")"
expect "download URL" 200 GET "/v1/media/$MEDIA_ID:download" "$OWNER"
fetch "$(body_field 'd["url"]')" "$WORK/served-again.png" > /dev/null
check "still the checked copy" "$SHA" "$(sha256sum "$WORK/served-again.png" | cut -d' ' -f1)"

echo "==> [3/6] nobody else"
expect "another user reads it" 403 GET "/v1/media/$MEDIA_ID" "$OTHER"
expect "another user downloads it" 403 GET "/v1/media/$MEDIA_ID:download" "$OTHER"
expect "another user deletes it" 403 DELETE "/v1/media/$MEDIA_ID" "$OTHER"
expect "a file that does not exist" 403 GET "/v1/media/$(uuid)" "$OWNER"
expect "no token" 401 GET "/v1/media/$MEDIA_ID" ""
check "the bucket without a signature" 403 "$(curl -sS -o /dev/null -w '%{http_code}' "$STORE/ride-media/")"

echo "==> [4/6] staff with media.read"
sql ride-staff-postgres "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
  values ('$STAFF_ID', '$STAFF_IDENTITY', 'e2e-media-$RUN@ride.test', 'E2E Media', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values ('$STAFF_ID', '$OPERATIONS_ROLE');" > /dev/null
STAFF="$(mint "$STAFF_IDENTITY")"

expect "the operator reads it" 200 GET "/v1/media/$MEDIA_ID" "$STAFF"
expect "the operator downloads it" 200 GET "/v1/media/$MEDIA_ID:download" "$STAFF"
expect "the operator deletes it" 403 DELETE "/v1/media/$MEDIA_ID" "$STAFF"
check "in the audit log" allowed "$(sql ride-staff-postgres "select decision from audit_entries where actor_staff_id = '$STAFF_ID' and permission = 'media.read' and target_id = '$MEDIA_ID' order by occurred_at limit 1;")"

echo "==> [5/6] rejected files"
check "reserve a fake picture" 200 "$(reserve "$OWNER" MEDIA_PURPOSE_PROFILE_PHOTO image/png "$WORK/fake.png")"
cp "$BODY_FILE" "$WORK/fake.json"
FAKE_ID="$(body_field 'd["media"]["id"]')"
expect "complete before the bytes arrive" 400 POST "/v1/media/$FAKE_ID:complete" "$OWNER" '{}'
send "$WORK/fake.json" image/png "$WORK/fake.png" > /dev/null
expect "complete the fake" 200 POST "/v1/media/$FAKE_ID:complete" "$OWNER" '{}'
check "rejected" MEDIA_STATUS_REJECTED "$(body_field 'd["media"]["status"]')"
expect "download a rejected file" 400 GET "/v1/media/$FAKE_ID:download" "$OWNER"

check "reserve a PDF with JavaScript" 200 "$(reserve "$OWNER" MEDIA_PURPOSE_DRIVER_DOCUMENT application/pdf "$WORK/script.pdf")"
cp "$BODY_FILE" "$WORK/pdf.json"
PDF_ID="$(body_field 'd["media"]["id"]')"
send "$WORK/pdf.json" application/pdf "$WORK/script.pdf" > /dev/null
expect "complete the PDF" 200 POST "/v1/media/$PDF_ID:complete" "$OWNER" '{}'
check "rejected" MEDIA_STATUS_REJECTED "$(body_field 'd["media"]["status"]')"

expect "a PDF as a profile photo" 400 POST /v1/media:upload "$OWNER" '{"purpose":"MEDIA_PURPOSE_PROFILE_PHOTO","contentType":"application/pdf","sizeBytes":100}'
expect "an SVG" 400 POST /v1/media:upload "$OWNER" '{"purpose":"MEDIA_PURPOSE_DRIVER_DOCUMENT","contentType":"image/svg+xml","sizeBytes":100}'
expect "over the size limit" 400 POST /v1/media:upload "$OWNER" '{"purpose":"MEDIA_PURPOSE_PROFILE_PHOTO","contentType":"image/jpeg","sizeBytes":6000000}'
expect "no purpose" 400 POST /v1/media:upload "$OWNER" '{"contentType":"image/jpeg","sizeBytes":100}'

echo "==> [6/6] hold and release"
HOLD="{\"media_id\":\"$MEDIA_ID\",\"owner_identity_id\":\"$OWNER_IDENTITY\",\"purpose\":\"MEDIA_PURPOSE_DRIVER_DOCUMENT\"}"
check "the owner holds their own file" PermissionDenied "$(grpc_code "$OWNER" ride.media.v1.MediaService/HoldMedia "$HOLD")"
check "a service holds it for another owner" FailedPrecondition "$(grpc_code "$INTERNAL_TOKEN" ride.media.v1.MediaService/HoldMedia "{\"media_id\":\"$MEDIA_ID\",\"owner_identity_id\":\"$OTHER_IDENTITY\",\"purpose\":\"MEDIA_PURPOSE_DRIVER_DOCUMENT\"}")"
check "a service holds it" OK "$(grpc_code "$INTERNAL_TOKEN" ride.media.v1.MediaService/HoldMedia "$HOLD")"
expect "the owner deletes a held file" 400 DELETE "/v1/media/$MEDIA_ID" "$OWNER"
check "the owner releases it" PermissionDenied "$(grpc_code "$OWNER" ride.media.v1.MediaService/ReleaseMedia "{\"media_id\":\"$MEDIA_ID\"}")"
check "a service releases it" OK "$(grpc_code "$INTERNAL_TOKEN" ride.media.v1.MediaService/ReleaseMedia "{\"media_id\":\"$MEDIA_ID\"}")"
expect "the owner deletes it" 200 DELETE "/v1/media/$MEDIA_ID" "$OWNER"
expect "download after delete" 400 GET "/v1/media/$MEDIA_ID:download" "$OWNER"
expect "delete again" 200 DELETE "/v1/media/$MEDIA_ID" "$OWNER"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: files are uploaded straight to the store, checked, served only to their owner and permitted staff"
