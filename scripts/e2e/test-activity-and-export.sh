#!/usr/bin/env bash
# End-to-end test of the activity page and "Download your data", on the real
# services, through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-activity-and-export.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken), grpcurl and buf (wallet top-ups go through the
# internal wallet RPC), the object store on 127.0.0.1:8333, identity-service
# migration 00013, media-service migration 00002 and notification-service
# migration 00016. It waits for the export maker, which runs every
# DATA_EXPORT_CHECK_INTERVAL (30s by default).
#
# What it proves:
#   1. the activity page lists the rider's trips and wallet top-ups in one
#      list, newest first, paged without gaps or repeats; the driver's page has
#      the driver's own; nobody else reads them; a made-up page token is
#      refused
#   2. a person asks for their data: once a day; the export is made, they are
#      told, and they download a ZIP with a README and JSON files from every
#      service (account and sign-in methods, rider profile and addresses,
#      trips, wallet, tickets, inbox); nobody else can download it
#
# Everything it creates is removed again.
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
OTHER="$(uuid)"
OTHER_SESSION="$(uuid)"
PHONE="+9647$(python3 -c 'import random; print(random.randint(100000000, 999999999))')"
PLATE="E2E-ACT-$RUN"
INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' services/identity-service/.env 2>/dev/null | tr -d '"')}"
RIDER_ID=""
DRIVER_ID=""

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
  [ -n "$RIDER_ID" ] && sql trip "delete from trips where rider_id = '$RIDER_ID';" > /dev/null 2>&1 || true
  sql support "delete from support_messages where ticket_id in (select id from support_tickets where requester_identity_id = '$IDENTITY'); delete from support_tickets where requester_identity_id = '$IDENTITY';" > /dev/null 2>&1 || true
  [ -n "$RIDER_ID" ] && sql notification "delete from notifications where recipient_id = '$RIDER_ID';" > /dev/null 2>&1 || true
  sql media "delete from media_objects where owner_identity_id = '$IDENTITY';" > /dev/null 2>&1 || true
  sql driver "delete from drivers where vehicle_plate_number = '$PLATE';" > /dev/null 2>&1 || true
  sql rider "delete from riders where identity_id = '$IDENTITY';" > /dev/null 2>&1 || true
  sql identity "delete from identities where id in ('$IDENTITY', '$OTHER');" > /dev/null 2>&1 || true
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

top_up() { # <owner type> <owner id> <amount> <key>
  "$GRPCURL" -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" \
    -d "{\"owner_type\":\"$1\",\"owner_id\":\"$2\",\"amount\":\"$3\",\"idempotency_key\":\"$4\",\"description\":\"e2e activity\"}" \
    "$WALLET_GRPC" ride.wallet.v1.WalletService/TopUp > /dev/null
}

echo "==> [0/2] preparing: a person with a rider and a driver profile, trips and top-ups"
[ "$(sql identity "select to_regclass('public.data_exports') is not null;")" = t ] \
  || { echo "ABORT: the data_exports table does not exist: apply identity-service migration 00013 (goose up)" >&2; exit 2; }
[ -n "${E2E_SKIP_BUF:-}" ] || buf build -o "$PROTOSET"

sql identity "
  insert into identities (id, status) values ('$IDENTITY', 'active'), ('$OTHER', 'active');
  insert into identity_identifiers (identity_id, identifier_type, normalized_value, verified_at) values ('$IDENTITY', 'phone', '$PHONE', now());
  insert into auth_sessions (id, identity_id, expires_at) values
    ('$SESSION', '$IDENTITY', now() + interval '1 day'), ('$OTHER_SESSION', '$OTHER', now() + interval '1 day');" > /dev/null
TOKEN="$(mint "$IDENTITY" "$SESSION")"
OTHER_TOKEN="$(mint "$OTHER" "$OTHER_SESSION")"

must "creating the rider" 200 POST /v1/riders "$TOKEN" "{\"identityId\":\"$IDENTITY\",\"displayName\":\"E2E Activity Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
must "registering the driver" 200 POST /v1/drivers "$TOKEN" "{\"identityId\":\"$IDENTITY\",\"displayName\":\"E2E Activity Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
must "a saved address" 200 POST "/v1/riders/$RIDER_ID/addresses" "$TOKEN" '{"kind":"SAVED_ADDRESS_KIND_HOME","coordinates":{"latitude":36.19,"longitude":44.01},"address":"E2E Home"}'

for i in 1 2 3; do
  sql trip "insert into trips (id, rider_id, status, pickup_latitude, pickup_longitude, dropoff_latitude, dropoff_longitude,
                pickup_address, dropoff_address, requested_at)
            values ('$(uuid)', '$RIDER_ID', 'cancelled', 36.19, 44.01, 36.2, 44.02, 'E2E Home', 'E2E Mall $i', now() - interval '$((i * 10)) minutes');" > /dev/null
done
top_up OWNER_TYPE_RIDER "$RIDER_ID" 1000 "e2e-act-r1-$RUN"
top_up OWNER_TYPE_RIDER "$RIDER_ID" 2000 "e2e-act-r2-$RUN"
top_up OWNER_TYPE_DRIVER "$DRIVER_ID" 3000 "e2e-act-d1-$RUN"
must "a ticket" 200 POST /v1/support/tickets "$TOKEN" '{"audience":"AUDIENCE_RIDER","categoryKey":"other","body":"E2E question about my account"}'

echo "==> [1/2] the activity page"
SEEN="$WORK/seen.txt"
: > "$SEEN"
TOKEN_PAGE=""
PAGES=0
while :; do
  must "an activity page" 200 GET "/v1/activity?rider_id=$RIDER_ID&page_size=2${TOKEN_PAGE:+&page_token=$TOKEN_PAGE}" "$TOKEN"
  python3 -c "
import json,sys
d=json.load(open(sys.argv[1]))
for it in d.get('items', []):
    print(it['occurredAt'], it['kind'], it['id'], (it.get('trip') or {}).get('dropoffAddress', ''), (it.get('wallet') or {}).get('type', ''))
" "$BODY_FILE" >> "$SEEN"
  PAGES=$((PAGES + 1))
  TOKEN_PAGE="$(body_field 'd.get("nextPageToken", "")')"
  [ -z "$TOKEN_PAGE" ] || [ "$PAGES" -ge 10 ] && break
done
check "five items over three pages" "5|3" "$(wc -l < "$SEEN" | tr -d ' ')|$PAGES"
check "three trips and two top-ups, no repeats" "3|2|5" \
  "$(grep -c ACTIVITY_ITEM_KIND_TRIP "$SEEN")|$(grep -c 'ACTIVITY_ITEM_KIND_WALLET .* top_up' "$SEEN")|$(cut -d' ' -f3 "$SEEN" | sort -u | wc -l | tr -d ' ')"
check "newest first" True "$(python3 -c "
import sys
times=[l.split()[0] for l in open(sys.argv[1])]
print(times == sorted(times, reverse=True))" "$SEEN")"
expect "the driver's page" 200 GET "/v1/activity?driver_id=$DRIVER_ID" "$TOKEN"
check "has the driver's top-up only" "1|top_up" "$(body_field 'str(len(d["items"])) + "|" + d["items"][0]["wallet"]["type"]')"
expect "someone else's page" 403 GET "/v1/activity?rider_id=$RIDER_ID" "$OTHER_TOKEN"
expect "both profiles at once" 403 GET "/v1/activity?rider_id=$RIDER_ID&driver_id=$DRIVER_ID" "$TOKEN"
expect "a made-up page token" 400 GET "/v1/activity?rider_id=$RIDER_ID&page_token=nonsense" "$TOKEN"

echo "==> [2/2] download your data"
expect "asking for the data" 200 POST /v1/me/data-exports "$TOKEN" '{}'
EXPORT_ID="$(body_field 'd["export"]["id"]')"
check "being made" DATA_EXPORT_STATUS_PENDING "$(body_field 'd["export"]["status"]')"
expect "asking again the same day" 429 POST /v1/me/data-exports "$TOKEN" '{}'
expect "not downloadable yet" 400 GET "/v1/me/data-exports/$EXPORT_ID/download" "$TOKEN"
echo "  (waiting for the export maker, up to about 100 s)"
STATUS=""
for _ in $(seq 1 50); do
  must "listing the exports" 200 GET /v1/me/data-exports "$TOKEN"
  STATUS="$(body_field 'd["exports"][0]["status"]')"
  [ "$STATUS" = DATA_EXPORT_STATUS_READY ] && break
  sleep 2
done
check "ready, kept for a week, the next request tomorrow" "DATA_EXPORT_STATUS_READY|True|True" \
  "$STATUS|$(python3 -c "
import json,sys,datetime as t
d=json.load(open(sys.argv[1])); e=d['exports'][0]
p=lambda s: t.datetime.fromisoformat(s.replace('Z','+00:00'))
print(str(abs((p(e['expiresAt'])-p(e['readyAt'])).days - 7) <= 1) + '|' + str('nextRequestAt' in d))" "$BODY_FILE")"
expect "someone else downloading it" 404 GET "/v1/me/data-exports/$EXPORT_ID/download" "$OTHER_TOKEN"
expect "the download link" 200 GET "/v1/me/data-exports/$EXPORT_ID/download" "$TOKEN"
if [ -n "${E2E_EXPORT_ZIP:-}" ]; then
  cp "$E2E_EXPORT_ZIP" "$WORK/export.zip"
else
  [ "$(curl -sS -o "$WORK/export.zip" -w '%{http_code}' "$(body_field 'd["url"]')")" = 200 ] \
    || { echo "FAIL: downloading the ZIP" >&2; exit 1; }
fi
check "the ZIP has a README and every service's part" True "$(python3 - "$WORK/export.zip" <<'PY'
import sys, zipfile
names = set(zipfile.ZipFile(sys.argv[1]).namelist())
want = {"README.txt", "identity/account.json", "identity/sign_in_methods.json", "rider/profile.json",
        "rider/saved_addresses.json", "driver/profile.json", "trips/as_rider.json",
        "wallet/rider_transactions.json", "wallet/driver_transactions.json", "support/tickets.json",
        "notifications/rider_inbox.json"}
missing = want - names
print("True" if not missing else "missing " + ",".join(sorted(missing)))
PY
)"
check "with the person's data in it" "$PHONE|3|2|E2E Home|1|E2E question about my account" "$(python3 - "$WORK/export.zip" <<'PY'
import json, sys, zipfile
z = zipfile.ZipFile(sys.argv[1])
read = lambda n: json.loads(z.read(n))
print("|".join([
    read("identity/sign_in_methods.json")[0]["value"],
    str(len(read("trips/as_rider.json"))),
    str(len(read("wallet/rider_transactions.json"))),
    read("rider/saved_addresses.json")[0]["address"],
    str(len(read("support/tickets.json"))),
    read("support/tickets.json")[0]["messages"][0]["body"],
]))
PY
)"
FOUND=False
for _ in $(seq 1 20); do
  if [ "$(http GET "/v1/notifications?recipient_type=RECIPIENT_TYPE_RIDER&recipient_id=$RIDER_ID&limit=50" "$TOKEN")" = 200 ] \
    && [ "$(body_field "any(n['eventKey'] == 'account.data_export_ready' for n in d.get('notifications', []))")" = True ]; then
    FOUND=True
    break
  fi
  sleep 1
done
check "the person is told" True "$FOUND"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: the activity page and the data export work end to end"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
