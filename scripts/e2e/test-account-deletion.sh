#!/usr/bin/env bash
# End-to-end test of deleting an account, on the real services, through the
# gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-account-deletion.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken), grpcurl and buf (a wallet top-up and a payout are
# made through the internal wallet RPCs), the object store on 127.0.0.1:8333,
# identity-service migration 00012, and OTP_HASH_SECRET in
# services/identity-service/.env: no SMS is sent, the script writes its codes
# the way identity-service does. It waits for the eraser, which runs every
# ACCOUNT_DELETION_CHECK_INTERVAL (1m by default): about a minute.
#
# What it proves:
#   1. a person who is both rider and driver sees what deleting would meet:
#      an open payout stands in the way and no code is sent; once it is
#      settled only the wallet balances remain, to be accepted
#   2. the code must be right, and confirming without accepting the balance
#      is refused
#   3. confirming ends every session at once: the token stops working, the
#      driver goes offline, push devices go
#   4. signing in again during the grace period cancels the deletion
#   5. asked again and left to run out, the account is erased everywhere:
#      the phone, sessions and PIN in identity-service; the rider's name,
#      details and saved addresses; the driver's name, details, documents
#      and plate (freed for someone else); the passenger details on trips;
#      the wallets emptied (a forfeit on the ledger) and blocked; the inbox;
#      every file in media-service
#   6. the same phone signing in again gets a new, empty account
#
# Everything it creates is removed again, except the anonymous rows that
# stay by design (an erased identity, profiles and ledger rows without a
# person).
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
WALLET_GRPC="${WALLET_GRPC_ADDRESS:-localhost:50058}"
PROTOSET="${PROTOSET:-/tmp/ride.binpb}"
GRPCURL="${GRPCURL:-grpcurl}"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
IDENTITY="$(uuid)"
SESSION="$(uuid)"
PHONE="+9647$(python3 -c 'import random; print(random.randint(100000000, 999999999))')"
PLATE="E2E-DEL-$RUN"
INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' services/identity-service/.env 2>/dev/null | tr -d '"')}"
OTP_SECRET="${OTP_HASH_SECRET:-$(sed -n 's/^OTP_HASH_SECRET=//p' services/identity-service/.env 2>/dev/null | tr -d '"')}"
RIDER_ID=""
DRIVER_ID=""
NEW_IDENTITY=""

[ -n "$OTP_SECRET" ] || { echo "ABORT: OTP_HASH_SECRET is missing from services/identity-service/.env" >&2; exit 2; }
[ -n "$INTERNAL_TOKEN" ] || { echo "ABORT: INTERNAL_SERVICE_TOKEN is missing from services/identity-service/.env" >&2; exit 2; }

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"

# sql <service> <query>: in the service's database (the compose containers, or
# local databases named in E2E_LOCAL_DBS="identity=db rider=db ...").
sql() {
  local local_db=""

  for pair in ${E2E_LOCAL_DBS:-}; do
    [ "${pair%%=*}" = "$1" ] && local_db="${pair#*=}"
  done

  if [ -n "$local_db" ]; then
    psql -h "${E2E_PGHOST:-/tmp}" -p "${E2E_PGPORT:-5499}" -U "${E2E_PGUSER:-postgres}" -d "$local_db" -tAc "$2"
  else
    docker exec "ride-$1-postgres" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
  fi
}

cleanup() {
  rm -rf "$WORK"
  for id in "$IDENTITY" "${NEW_IDENTITY:-$IDENTITY}"; do
    sql identity "delete from identities where id = '$id';" > /dev/null 2>&1 || true
    sql media "delete from media_objects where owner_identity_id = '$id';" > /dev/null 2>&1 || true
  done
  sql identity "delete from identity_identifiers where normalized_value = '$PHONE';" > /dev/null 2>&1 || true
  [ -n "$RIDER_ID" ] && sql notification "delete from notifications where recipient_id = '$RIDER_ID'; delete from devices where recipient_id = '$RIDER_ID';" > /dev/null 2>&1 || true
  [ -n "$DRIVER_ID" ] && sql notification "delete from notifications where recipient_id = '$DRIVER_ID';" > /dev/null 2>&1 || true
}
trap cleanup EXIT

mint() { # <identity> <session>
  go run scripts/tools/devtoken/main.go ${DEVTOKEN_KEY:+-key "$DEVTOKEN_KEY"} -sub "$1" -sid "$2"
}

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

wallet_rpc() { # <method> <json>
  "$GRPCURL" -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$2" \
    "$WALLET_GRPC" "ride.wallet.v1.WalletService/$1" > /dev/null
}

# code_for <purpose> <target identity or ""> : writes a challenge for the phone
# with a known code, hashed as identity-service hashes it; prints its id.
code_for() {
  local id="e2e-$1-$(uuid)"
  local target="null"
  [ -n "$2" ] && target="'$2'"

  local hash
  hash="$(OTP_SECRET="$OTP_SECRET" python3 - "$id" 424242 <<'PY'
import base64, hashlib, hmac, os, sys
challenge, code = sys.argv[1], sys.argv[2]
message = f"otp:v1:{len(challenge)}:{challenge}:{len(code)}:{code}".encode()
print(base64.urlsafe_b64encode(hmac.new(os.environ["OTP_SECRET"].encode(), message, hashlib.sha256).digest()).decode().rstrip("="))
PY
)"

  sql identity "insert into otp_challenges (id, identifier_type, normalized_value, purpose, target_identity_id, code_hash, expires_at)
                values ('$id', 'phone', '$PHONE', '$1', $target, '$hash', now() + interval '5 minutes');" > /dev/null
  echo "$id"
}

# deletion_field <python expression over d["deletion"]>
deletion() {
  must "reading the deletion" 200 GET /v1/me/deletion "$TOKEN"
  python3 -c "import json,sys; d=json.load(open(sys.argv[1]))['deletion']; print($1)" "$BODY_FILE"
}

# wait_for <label> <service> <query> <expected>: polls up to ~100 s.
wait_for() {
  local got=""
  for _ in $(seq 1 50); do
    got="$(sql "$2" "$3" 2>/dev/null || true)"
    [ "$got" = "$4" ] && break
    sleep 2
  done
  check "$1" "$4" "$got"
}

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

# upload <purpose> <file>: a READY upload of the person; with E2E_FAKE_MEDIA=1
# (a media-service stand-in) just an id.
upload() {
  if [ -n "${E2E_FAKE_MEDIA:-}" ]; then
    uuid
    return
  fi

  local size id url args=()
  size="$(stat -c %s "$2")"
  must "reserving an upload" 200 POST /v1/media:upload "$TOKEN" "{\"purpose\":\"$1\",\"contentType\":\"image/png\",\"sizeBytes\":$size}"
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

  [ "$(curl -sS -o /dev/null -w '%{http_code}' -X PUT "${args[@]}" -H "Content-Type: image/png" --data-binary "@$2" "$url")" = 200 ] \
    || { echo "FAIL: sending a file to the object store" >&2; exit 1; }
  must "completing an upload" 200 POST "/v1/media/$id:complete" "$TOKEN" '{}'
  echo "$id"
}

echo "==> [0/6] preparing: an identity with a phone, a session, a rider and a driver"
[ "$(sql identity "select to_regclass('public.account_deletions') is not null;")" = t ] \
  || { echo "ABORT: the account_deletions table does not exist: apply identity-service migration 00012 (goose up)" >&2; exit 2; }
[ -n "${E2E_SKIP_BUF:-}" ] || buf build -o "$PROTOSET"

sql identity "
  insert into identities (id, status) values ('$IDENTITY', 'active');
  insert into identity_identifiers (identity_id, identifier_type, normalized_value, verified_at) values ('$IDENTITY', 'phone', '$PHONE', now());
  insert into auth_sessions (id, identity_id, expires_at) values ('$SESSION', '$IDENTITY', now() + interval '1 day');" > /dev/null
TOKEN="$(mint "$IDENTITY" "$SESSION")"

expect "the account works" 200 GET /v1/me "$TOKEN"
must "creating the rider" 200 POST /v1/riders "$TOKEN" "{\"identityId\":\"$IDENTITY\",\"displayName\":\"E2E Deleted Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
must "registering the driver" 200 POST /v1/drivers "$TOKEN" "{\"identityId\":\"$IDENTITY\",\"displayName\":\"E2E Deleted Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
sql driver "update drivers set status = 'active', availability_status = 'available' where id = '$DRIVER_ID';" > /dev/null

must "rider details" 200 PATCH "/v1/riders/$RIDER_ID/details" "$TOKEN" '{"gender":"GENDER_FEMALE","nationality":"IQ"}'
PHOTO="$(upload MEDIA_PURPOSE_PROFILE_PHOTO "$(picture rider)")"
must "rider photo" 200 PUT "/v1/riders/$RIDER_ID/photo" "$TOKEN" "{\"mediaId\":\"$PHOTO\"}"
must "a saved address" 200 POST "/v1/riders/$RIDER_ID/addresses" "$TOKEN" '{"kind":"SAVED_ADDRESS_KIND_OTHER","label":"Gym","coordinates":{"latitude":36.19,"longitude":44.01},"address":"E2E Gym","noteForDriver":"Blue gate"}'
must "driver details" 200 PATCH "/v1/drivers/$DRIVER_ID/details" "$TOKEN" '{"nationality":"IQ"}'
must "a push device" 200 POST /v1/devices "$TOKEN" "{\"recipientType\":\"RECIPIENT_TYPE_RIDER\",\"recipientId\":\"$RIDER_ID\",\"deviceToken\":\"e2e-device-$RUN\",\"platform\":\"PLATFORM_ANDROID\",\"locale\":\"ar\"}"
TRIP_ID="$(uuid)"
sql trip "insert into trips (id, rider_id, status, pickup_latitude, pickup_longitude, dropoff_latitude, dropoff_longitude,
              pickup_address, pickup_details, pickup_note, passenger_name, passenger_phone)
          values ('$TRIP_ID', '$RIDER_ID', 'cancelled', 36.19, 44.01, 36.2, 44.02, 'E2E Home', 'Floor 2', 'Blue gate', 'Sara', '+9647501111111');" > /dev/null

wallet_rpc TopUp "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER_ID\",\"amount\":\"2500\",\"idempotency_key\":\"e2e-del-r-$RUN\",\"description\":\"e2e account deletion\"}"
wallet_rpc TopUp "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRIVER_ID\",\"amount\":\"15000\",\"idempotency_key\":\"e2e-del-d-$RUN\",\"description\":\"e2e account deletion\"}"

echo "==> [1/6] what deleting would meet"
must "an open payout" 200 POST "/v1/drivers/$DRIVER_ID/payouts" "$TOKEN" "{\"amount\":\"10000\",\"idempotencyKey\":\"e2e-del-p-$RUN\",\"destination\":\"ZainCash +9647509998887\"}"
check "nothing pending, the payout stands in the way" "ACCOUNT_DELETION_STATUS_NONE|ACCOUNT_DELETION_BLOCKER_OPEN_PAYOUT|30" \
  "$(deletion 'd["status"] + "|" + ",".join(d.get("blockers", [])) + "|" + str(d["gracePeriodDays"])')"
expect "no code while something stands in the way" 400 POST /v1/me/deletion:request-otp "$TOKEN" '{}'
check "no code was written" 0 "$(sql identity "select count(*) from otp_challenges where target_identity_id = '$IDENTITY' and purpose = 'delete_account';")"
sql wallet "update payout_requests set status = 'paid', approved_at = now(), paid_at = now(), paid_reference = 'e2e' where driver_id = '$DRIVER_ID';" > /dev/null
check "settled: only the balances remain" "|rider:2500,driver:5000" \
  "$(deletion '",".join(d.get("blockers", [])) + "|" + ",".join(b["ownerType"] + ":" + format(__import__("decimal").Decimal(b["amount"]).normalize(), "f") for b in d.get("forfeitedBalances", []))')"

echo "==> [2/6] confirming"
expect "without a token" 401 GET /v1/me/deletion ""
WRONG="$(code_for delete_account "$IDENTITY")"
expect "a wrong code" 400 POST /v1/me/deletion:confirm "$TOKEN" "{\"challengeId\":\"$WRONG\",\"code\":\"000000\"}"
LOGIN_CODE="$(code_for login "")"
expect "a sign-in code is not a deletion code" 404 POST /v1/me/deletion:confirm "$TOKEN" "{\"challengeId\":\"$LOGIN_CODE\",\"code\":\"424242\"}"
CODE="$(code_for delete_account "$IDENTITY")"
expect "the balance not accepted" 400 POST /v1/me/deletion:confirm "$TOKEN" "{\"challengeId\":\"$CODE\",\"code\":\"424242\"}"
expect "confirmed" 200 POST /v1/me/deletion:confirm "$TOKEN" "{\"challengeId\":\"$CODE\",\"code\":\"424242\",\"acceptBalanceLoss\":true}"
check "pending for 30 days" "ACCOUNT_DELETION_STATUS_PENDING|True" \
  "$(python3 -c "import json,sys,datetime as t; d=json.load(open(sys.argv[1]))['deletion']; a=t.datetime.fromisoformat(d['requestedAt'].replace('Z','+00:00')); b=t.datetime.fromisoformat(d['purgeAfter'].replace('Z','+00:00')); print(d['status'] + '|' + str(abs((b-a).days - 30) <= 1))" "$BODY_FILE")"

echo "==> [3/6] the account stops at once"
expect "the token no longer works" 401 GET /v1/me "$TOKEN"
check "every session ended" 0 "$(sql identity "select count(*) from auth_sessions where identity_id = '$IDENTITY' and revoked_at is null;")"
wait_for "the driver went offline" driver "select availability_status from drivers where id = '$DRIVER_ID';" offline
wait_for "the push device is gone" notification "select count(*) from devices where recipient_id = '$RIDER_ID';" 0
check "nothing erased yet" "E2E Deleted Rider" "$(sql rider "select display_name from riders where id = '$RIDER_ID';")"

echo "==> [4/6] signing in again cancels it"
SIGN_IN="$(code_for login "")"
expect "signing in" 200 POST /v1/auth/otp:verify "" "{\"challengeId\":\"$SIGN_IN\",\"code\":\"424242\"}"
check "the same account" "$IDENTITY" "$(body_field 'd["identityId"]')"
TOKEN="$(body_field 'd["accessToken"]')"
check "the deletion is cancelled" "cancelled|ACCOUNT_DELETION_STATUS_NONE" \
  "$(sql identity "select status from account_deletions where identity_id = '$IDENTITY';")|$(deletion 'd["status"]')"

echo "==> [5/6] asked again and left to run out: erased everywhere"
CODE="$(code_for delete_account "$IDENTITY")"
must "confirming again" 200 POST /v1/me/deletion:confirm "$TOKEN" "{\"challengeId\":\"$CODE\",\"code\":\"424242\",\"acceptBalanceLoss\":true}"
sql identity "update account_deletions set purge_after = now(), requested_at = now() - interval '1 second' where identity_id = '$IDENTITY';" > /dev/null
echo "  (waiting for the eraser, up to about 100 s)"
wait_for "the account is erased" identity "select status from account_deletions where identity_id = '$IDENTITY';" completed
check "identity: no phone, session or PIN; disabled" "0|0|0|disabled" \
  "$(sql identity "select (select count(*) from identity_identifiers where identity_id = '$IDENTITY') || '|' ||
                          (select count(*) from auth_sessions where identity_id = '$IDENTITY') || '|' ||
                          (select count(*) from wallet_pins where identity_id = '$IDENTITY') || '|' ||
                          (select status from identities where id = '$IDENTITY');")"
check "no code sent to the phone is kept" 0 "$(sql identity "select count(*) from otp_challenges where normalized_value = '$PHONE';")"
wait_for "rider: renamed, suspended, no details or addresses" rider \
  "select display_name || '|' || status || '|' || (select count(*) from rider_details where rider_id = r.id) || '|' || (select count(*) from saved_addresses where rider_id = r.id) from riders r where id = '$RIDER_ID';" \
  "Deleted account|suspended|0|0"
wait_for "driver: renamed, suspended, offline, no details, documents or plate" driver \
  "select display_name || '|' || status || '|' || availability_status || '|' || (select count(*) from driver_details where driver_id = d.id) || '|' || (select count(*) from driver_documents where driver_id = d.id) || '|' || (vehicle_plate_number like 'DELETED-%') from drivers d where id = '$DRIVER_ID';" \
  "Deleted account|suspended|offline|0|0|true"
check "the plate is free" 0 "$(sql driver "select count(*) from vehicles where plate_number = '$PLATE' and status <> 'retired';")"
wait_for "trips: no passenger or pickup details" trip \
  "select passenger_name || '|' || passenger_phone || '|' || pickup_details || '|' || pickup_note || '|' || pickup_address from trips where id = '$TRIP_ID';" \
  "||||E2E Home"
wait_for "wallets: emptied and blocked" wallet \
  "select string_agg(owner_type || ':' || (balance = 0) || ':' || blocked, ',' order by owner_type) from wallets where owner_id in ('$RIDER_ID', '$DRIVER_ID');" \
  "driver:true:true,rider:true:true"
check "the forfeits are on the ledger" 2 "$(sql wallet "select count(*) from wallet_transactions where wallet_id in (select id from wallets where owner_id in ('$RIDER_ID', '$DRIVER_ID')) and description = 'Account deleted: balance forfeited';")"
check "the payout destination is gone" "" "$(sql wallet "select string_agg(destination, '') from payout_requests where driver_id = '$DRIVER_ID';")"
wait_for "the inbox is gone" notification "select count(*) from notifications where recipient_id in ('$RIDER_ID', '$DRIVER_ID');" 0
if [ -z "${E2E_FAKE_MEDIA:-}" ]; then
  check "every file is deleted" 0 "$(sql media "select count(*) from media_objects where owner_identity_id = '$IDENTITY' and status not in ('deleted', 'expired');")"
fi

echo "==> [6/6] the same phone later gets a new, empty account"
SIGN_IN="$(code_for login "")"
expect "signing in with the phone" 200 POST /v1/auth/otp:verify "" "{\"challengeId\":\"$SIGN_IN\",\"code\":\"424242\"}"
NEW_IDENTITY="$(body_field 'd["identityId"]')"
check "a new identity" True "$( [ -n "$NEW_IDENTITY" ] && [ "$NEW_IDENTITY" != "$IDENTITY" ] && echo True || echo False)"
expect "with no rider profile" 404 GET "/v1/identities/$NEW_IDENTITY/rider" "$(body_field 'd["accessToken"]')"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: deleting an account works end to end"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
