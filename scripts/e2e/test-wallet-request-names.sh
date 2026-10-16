#!/usr/bin/env bash
# End-to-end test of the names on money requests, and of asking a phone
# counting as a recipient lookup, on the real services, through the gateway.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-wallet-request-names.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken) and grpcurl. Like test-wallet-requests.sh it puts
# its people straight into the identity database (an identity, its phone and
# a session each).
#
# What it proves:
#   1. a request for a rider keeps both short names: the one asked sees who
#      asks (in the list, when opening it, and in the push), the requester
#      whom they asked
#   2. an open request: a stranger opening it sees who asks (the name, the
#      phone masked), not who paid; once paid the requester sees who paid
#   3. asking a phone counts as a recipient lookup (an open request does
#      not, nor a retry); past the hourly limit the answer is 429 and no
#      request is made
#
# It creates three riders and removes them again, with everything they did.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
WALLET_ADDR="localhost:50058"
PROTOSET="/tmp/ride.binpb"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
PAYER_IDENTITY="$(uuid)"
ASKER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
PAYER_SESSION="$(uuid)"
ASKER_SESSION="$(uuid)"
OTHER_SESSION="$(uuid)"
# Phones no real person has: +96479 and the run's last eight digits.
SUFFIX="$(printf '%08d' $((RUN % 100000000)))"
PAYER_PHONE="+964795${SUFFIX:1}"
ASKER_PHONE="+964796${SUFFIX:1}"
OTHER_PHONE="+964797${SUFFIX:1}"
UNKNOWN="$(uuid)"

INTERNAL_TOKEN=""
for env_file in services/wallet-service/.env services/trip-service/.env; do
  [ -z "$INTERNAL_TOKEN" ] && [ -f "$env_file" ] && INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' "$env_file" | head -1 | tr -d '"')"
done
INTERNAL_TOKEN="${INTERNAL_TOKEN:-dev-internal-service-token-change-me}"

# The hourly lookup limit the running wallet-service uses (its default 30).
PER_HOUR="${TRANSFER_LOOKUP_MAX_PER_HOUR:-30}"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
PAYER_ID=""
ASKER_ID=""
OTHER_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  if [ -n "$PAYER_ID$ASKER_ID$OTHER_ID" ]; then
    local riders="'${PAYER_ID:-$UNKNOWN}', '${ASKER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}'"
    sql ride-wallet-postgres "
      delete from transfer_recipient_lookups where rider_id in ($riders);
      delete from money_requests where requester_rider_id in ($riders) or payer_rider_id in ($riders);
      delete from wallet_transactions where wallet_id in (select id from wallets where owner_id in ($riders));
      delete from wallet_transfers where sender_rider_id in ($riders) or recipient_rider_id in ($riders);
      delete from wallets where owner_id in ($riders);" > /dev/null 2>&1 || true
    sql ride-notification-postgres "delete from notifications where recipient_id in ($riders);" > /dev/null 2>&1 || true
    sql ride-rider-postgres "delete from riders where id in ($riders);" > /dev/null 2>&1 || true
  fi
  sql ride-identity-postgres "delete from identities where id in ('$PAYER_IDENTITY', '$ASKER_IDENTITY', '$OTHER_IDENTITY');" > /dev/null 2>&1 || true
}
trap cleanup EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1" -sid "$2"; }

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

# person <identity> <session> <phone>: an identity with a phone and a live
# session, as signing in leaves them.
person() {
  sql ride-identity-postgres "
    insert into identities (id) values ('$1');
    insert into identity_identifiers (identity_id, identifier_type, normalized_value, verified_at)
      values ('$1', 'phone', '$3', now());
    insert into auth_sessions (id, identity_id, expires_at) values ('$2', '$1', now() + interval '1 day');" > /dev/null
}

ask() { # <payer phone or ""> <amount> <key>
  echo "{\"payerPhone\":\"$1\",\"amount\":\"$2\",\"idempotencyKey\":\"$3\",\"note\":\"lunch\"}"
}

req() { # <python expression over r, the money request>
  body_field "(lambda r: $1)(d['moneyRequest'])"
}

lookups_counted() {
  sql ride-wallet-postgres "select count(*) from transfer_recipient_lookups where rider_id = '$ASKER_ID';" | tr -d ' '
}

requests_made() {
  sql ride-wallet-postgres "select count(*) from money_requests where requester_rider_id = '$ASKER_ID';" | tr -d ' '
}

# told_by_name <rider id> <token> <name>: True once the rider's money
# request notification names the requester.
told_by_name() {
  for _ in $(seq 1 20); do
    if [ "$(http GET "/v1/notifications?recipient_type=RECIPIENT_TYPE_RIDER&recipient_id=$1&limit=50" "$2")" = 200 ] \
      && [ "$(body_field "any(n['eventKey'] == 'wallet.money_requested' and '$3' in json.dumps(n, ensure_ascii=False) for n in d.get('notifications', []))")" = True ]; then
      echo True
      return
    fi
    sleep 1
  done
  echo False
}

buf build -o "$PROTOSET"
person "$PAYER_IDENTITY" "$PAYER_SESSION" "$PAYER_PHONE"
person "$ASKER_IDENTITY" "$ASKER_SESSION" "$ASKER_PHONE"
person "$OTHER_IDENTITY" "$OTHER_SESSION" "$OTHER_PHONE"
PAYER="$(mint "$PAYER_IDENTITY" "$PAYER_SESSION")"
ASKER="$(mint "$ASKER_IDENTITY" "$ASKER_SESSION")"
OTHER="$(mint "$OTHER_IDENTITY" "$OTHER_SESSION")"

echo "==> [0/3] three riders with names; another rider holds 5000 and a PIN"
expect "the payer" 200 POST /v1/riders "$PAYER" "{\"identityId\":\"$PAYER_IDENTITY\",\"displayName\":\"Sara Ahmed\"}"
PAYER_ID="$(body_field 'd["rider"]["id"]')"
expect "the asker" 200 POST /v1/riders "$ASKER" "{\"identityId\":\"$ASKER_IDENTITY\",\"displayName\":\"Haidar Kamal\"}"
ASKER_ID="$(body_field 'd["rider"]["id"]')"
expect "another rider" 200 POST /v1/riders "$OTHER" "{\"identityId\":\"$OTHER_IDENTITY\",\"displayName\":\"Omar Azad Hasan\"}"
OTHER_ID="$(body_field 'd["rider"]["id"]')"
expect "the other's PIN" 200 PUT /v1/me/wallet-pin "$OTHER" '{"newPin":"1357"}'
grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" \
  -d "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$OTHER_ID\",\"amount\":\"5000\",\"idempotency_key\":\"e2e-request-names-$RUN\"}" \
  "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp > /dev/null

REQUESTS="/v1/wallets/$ASKER_ID/money-requests"

echo "==> [1/3] a request for a rider keeps both names"
expect "2500 from the payer" 200 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 2500 n-1)"
CODE="$(req 'r["code"]')"
check "the requester sees whom they asked" "Haidar K.|Sara A." "$(req 'r.get("requesterName", "") + "|" + r.get("payerName", "")')"
expect "the payer's incoming requests" 200 GET "/v1/wallets/$PAYER_ID/money-requests?role=incoming&status=pending" "$PAYER"
check "say who asks" "$CODE Haidar K." "$(body_field 'd["moneyRequests"][0]["code"] + " " + d["moneyRequests"][0].get("requesterName", "")')"
expect "the payer opens it" 200 GET "/v1/wallets/$PAYER_ID/money-requests/$CODE" "$PAYER"
check "and sees who asks" "Haidar K." "$(req 'r.get("requesterName", "")')"
check "the push names the requester" True "$(told_by_name "$PAYER_ID" "$PAYER" "Haidar K.")"

echo "==> [2/3] an open request"
expect "an open request (a link)" 200 POST "$REQUESTS" "$ASKER" "$(ask "" 1000 n-open)"
OPEN_CODE="$(req 'r["code"]')"
check "has no payer yet" "Haidar K.|" "$(req 'r.get("requesterName", "") + "|" + r.get("payerName", "")')"
expect "a stranger opens the link" 200 GET "/v1/wallets/$OTHER_ID/money-requests/$OPEN_CODE" "$OTHER"
check "sees who asks, the phone masked" "viewer Haidar K. True" \
  "$(req 'r["role"] + " " + r.get("requesterName", "") + " " + str("***" in r["requesterPhone"])')"
expect "the stranger pays it" 200 POST "/v1/wallets/$OTHER_ID/money-requests/$OPEN_CODE:pay" "$OTHER" '{"pin":"1357"}'
check "paid, they are the payer now" "paid payer Omar H." "$(req 'r["status"] + " " + r["role"] + " " + r.get("payerName", "")')"
expect "the asker's paid requests" 200 GET "$REQUESTS?role=outgoing&status=paid" "$ASKER"
check "say who paid" "$OPEN_CODE Omar H." "$(body_field 'd["moneyRequests"][0]["code"] + " " + d["moneyRequests"][0].get("payerName", "")')"
expect "the payer opens the paid link" 200 GET "/v1/wallets/$PAYER_ID/money-requests/$OPEN_CODE" "$PAYER"
check "a third rider does not see who paid" "viewer|" "$(req 'r["role"] + "|" + r.get("payerName", "")')"

echo "==> [3/3] asking a phone counts as a recipient lookup"
check "one counted so far (the open request was not)" 1 "$(lookups_counted)"
expect "the same key again" 200 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 2500 n-1)"
check "is not counted again" 1 "$(lookups_counted)"
# Lookups made earlier this hour, up to one short of the limit.
sql ride-wallet-postgres "insert into transfer_recipient_lookups (rider_id, looked_up_at)
  select '$ASKER_ID', now() - interval '10 minutes' from generate_series(1, $((PER_HOUR - 2)));" > /dev/null
expect "the last one this hour" 200 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 2500 n-2)"
MADE="$(requests_made)"
expect "one more" 429 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 2500 n-3)"
check "is refused: nothing counted, no request" "$PER_HOUR $MADE" "$(lookups_counted) $(requests_made)"
expect "an open request still" 200 POST "$REQUESTS" "$ASKER" "$(ask "" 1000 n-open-2)"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: money requests say who asks and who is asked, and asking a phone counts as a lookup"
