#!/usr/bin/env bash
# End-to-end test of riders asking each other for money (by phone, or with an
# open link / QR code anyone may pay), of the wallet statement, and of who
# may read a rider's unpaid fees, on the real services, through the gateway.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-wallet-requests.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken) and grpcurl. Like test-wallet-transfers.sh it puts
# its people straight into the identity database (an identity, its phone and
# a session each). Unpaid fees blocking trips are proved in
# test-trip-fees.sh, where the fees come from real cancelled trips.
#
# What it proves:
#   1. a rider asks another, by phone, for money: the one asked is told and
#      sees it; a retry with the same key is the same request; a stranger
#      does not see it; an open request is seen by anyone with its code, with
#      the requester's phone masked
#   2. paying it is a transfer with the payer's PIN, once: paying again
#      returns the same payment, and nobody else can pay it after
#   3. an open request is paid by whoever pays first
#   4. the one asked declines, the requester cancels, each only their own;
#      an expired request cannot be paid
#   5. a wallet's statement over a period: opening and closing balances,
#      totals in and out, filters and pages; only the owner reads it
#   6. only the rider (or a service) reads a rider's unpaid fees
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
UNKNOWN_PHONE="+964798${SUFFIX:1}"
UNKNOWN="$(uuid)"

INTERNAL_TOKEN=""
for env_file in services/wallet-service/.env services/trip-service/.env; do
  [ -z "$INTERNAL_TOKEN" ] && [ -f "$env_file" ] && INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' "$env_file" | head -1 | tr -d '"')"
done
INTERNAL_TOKEN="${INTERNAL_TOKEN:-dev-internal-service-token-change-me}"

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

message() { body_field 'd.get("message", "")'; }

# person <identity> <session> <phone>: an identity with a phone and a live
# session, as signing in leaves them.
person() {
  sql ride-identity-postgres "
    insert into identities (id) values ('$1');
    insert into identity_identifiers (identity_id, identifier_type, normalized_value, verified_at)
      values ('$1', 'phone', '$3', now());
    insert into auth_sessions (id, identity_id, expires_at) values ('$2', '$1', now() + interval '1 day');" > /dev/null
}

top_up() { # <rider id> <amount> <key>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" \
    -d "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$1\",\"amount\":\"$2\",\"idempotency_key\":\"$3\"}" \
    "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp > /dev/null
}

ask() { # <payer phone or ""> <amount> <key> [expires in hours]
  echo "{\"payerPhone\":\"$1\",\"amount\":\"$2\",\"idempotencyKey\":\"$3\",\"note\":\"lunch\",\"expiresInHours\":${4:-0}}"
}

req() { # <python expression over r, the money request>
  body_field "(lambda r: $1)(d['moneyRequest'])"
}

balance_of() { # <rider id> <token>
  http GET "/v1/wallets/$1?owner_type=OWNER_TYPE_RIDER" "$2" > /dev/null
  body_field 'd["wallet"]["balance"]'
}

# notified <rider id> <token> <event key>: True once the rider has that notification.
notified() {
  for _ in $(seq 1 20); do
    if [ "$(http GET "/v1/notifications?recipient_type=RECIPIENT_TYPE_RIDER&recipient_id=$1&limit=50" "$2")" = 200 ] \
      && [ "$(body_field "any(n['eventKey'] == '$3' for n in d.get('notifications', []))")" = True ]; then
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
START="$(python3 -c 'import datetime as t; print((t.datetime.now(t.timezone.utc) - t.timedelta(minutes=1)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"

echo "==> [0/6] three riders with PINs; the payer holds 20000, another 5000"
expect "the payer" 200 POST /v1/riders "$PAYER" "{\"identityId\":\"$PAYER_IDENTITY\",\"displayName\":\"E2E Payer\"}"
PAYER_ID="$(body_field 'd["rider"]["id"]')"
expect "the asker" 200 POST /v1/riders "$ASKER" "{\"identityId\":\"$ASKER_IDENTITY\",\"displayName\":\"E2E Asker\"}"
ASKER_ID="$(body_field 'd["rider"]["id"]')"
expect "another rider" 200 POST /v1/riders "$OTHER" "{\"identityId\":\"$OTHER_IDENTITY\",\"displayName\":\"E2E Other\"}"
OTHER_ID="$(body_field 'd["rider"]["id"]')"
expect "the payer's PIN" 200 PUT /v1/me/wallet-pin "$PAYER" '{"newPin":"2580"}'
expect "the asker's PIN" 200 PUT /v1/me/wallet-pin "$ASKER" '{"newPin":"2468"}'
expect "the other's PIN" 200 PUT /v1/me/wallet-pin "$OTHER" '{"newPin":"1357"}'
top_up "$PAYER_ID" 20000 "e2e-requests-payer-$RUN"
top_up "$OTHER_ID" 5000 "e2e-requests-other-$RUN"
check "the payer holds 20000" 20000 "$(balance_of "$PAYER_ID" "$PAYER")"

echo "==> [1/6] asking for money"
REQUESTS="/v1/wallets/$ASKER_ID/money-requests"
expect "without a token" 401 POST "$REQUESTS" "" "$(ask "$PAYER_PHONE" 2500 r-1)"
expect "from another rider's wallet" 403 POST "$REQUESTS" "$PAYER" "$(ask "$PAYER_PHONE" 2500 r-1)"
expect "from a phone nobody has" 404 POST "$REQUESTS" "$ASKER" "$(ask "$UNKNOWN_PHONE" 2500 r-x1)"
expect "from themselves" 400 POST "$REQUESTS" "$ASKER" "$(ask "$ASKER_PHONE" 2500 r-x2)"
expect "less than a transfer may be" 400 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 100 r-x3)"
expect "for 200 hours" 400 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 2500 r-x4 200)"
expect "2500 from the payer" 200 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 2500 r-1)"
CODE="$(req 'r["code"]')"
check "pending, for the payer, with a code" "requester pending $PAYER_PHONE 2500 lunch 10" "$(req '" ".join((r["role"], r["status"], r["payerPhone"], r["amount"], r["note"], str(len(r["code"]))))')"
expect "the same key again" 200 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 2500 r-1)"
check "is the same request" "$CODE" "$(req 'r["code"]')"
expect "the key for another amount" 409 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 3000 r-1)"
expect "the payer's incoming requests" 200 GET "/v1/wallets/$PAYER_ID/money-requests?role=incoming&status=pending" "$PAYER"
check "has it, from the asker" "1 $CODE payer $ASKER_PHONE" "$(body_field '" ".join((str(len(d["moneyRequests"])), d["moneyRequests"][0]["code"], d["moneyRequests"][0]["role"], d["moneyRequests"][0]["requesterPhone"]))')"
check "the payer is told" True "$(notified "$PAYER_ID" "$PAYER" wallet.money_requested)"
expect "the payer opens it by its code, typed in lower case" 200 GET "/v1/wallets/$PAYER_ID/money-requests/$(echo "$CODE" | tr 'A-Z' 'a-z')" "$PAYER"
expect "a stranger opens it" 404 GET "/v1/wallets/$OTHER_ID/money-requests/$CODE" "$OTHER"
expect "an open request (a link)" 200 POST "$REQUESTS" "$ASKER" "$(ask "" 1000 r-open 1)"
OPEN_CODE="$(req 'r["code"]')"
check "open, for no one" "True " "$(req 'str(r.get("open", False)) + " " + r.get("payerPhone", "")')"
expect "a stranger opens the link" 200 GET "/v1/wallets/$OTHER_ID/money-requests/$OPEN_CODE" "$OTHER"
check "sees it, the phone masked" "viewer ${ASKER_PHONE:0:$((${#ASKER_PHONE} - 7))}***${ASKER_PHONE: -4}" "$(req 'r["role"] + " " + r["requesterPhone"]')"
expect "a list for both sides" 400 GET "/v1/wallets/$ASKER_ID/money-requests?role=both" "$ASKER"

echo "==> [2/6] paying it"
PAY="/v1/wallets/$PAYER_ID/money-requests/$CODE:pay"
expect "with a wrong PIN" 403 POST "$PAY" "$PAYER" '{"pin":"9999"}'
expect "the asker pays their own" 400 POST "/v1/wallets/$ASKER_ID/money-requests/$CODE:pay" "$ASKER" '{"pin":"2468"}'
expect "from another rider's wallet" 403 POST "$PAY" "$OTHER" '{"pin":"1357"}'
expect "with the PIN" 200 POST "$PAY" "$PAYER" '{"pin":"2580"}'
TRANSFER_ID="$(body_field 'd["transfer"]["id"]')"
check "paid, a transfer to the asker, what is left" "paid sent $ASKER_PHONE 2500 17500 $TRANSFER_ID" "$(body_field '" ".join((d["moneyRequest"]["status"], d["transfer"]["direction"], d["transfer"]["counterpartPhone"], d["transfer"]["amount"], d["wallet"]["balance"], d["moneyRequest"]["transferId"]))')"
expect "paying again" 200 POST "$PAY" "$PAYER" '{"pin":"2580"}'
check "is the same payment, nothing more moved" "$TRANSFER_ID 17500" "$(body_field 'd["transfer"]["id"] + " " + d["wallet"]["balance"]')"
check "the asker holds 2500" 2500 "$(balance_of "$ASKER_ID" "$ASKER")"
expect "the asker's paid requests" 200 GET "/v1/wallets/$ASKER_ID/money-requests?role=outgoing&status=paid" "$ASKER"
check "has it" "1 $CODE" "$(body_field 'str(len(d["moneyRequests"])) + " " + d["moneyRequests"][0]["code"]')"
check "the asker is told the money came" True "$(notified "$ASKER_ID" "$ASKER" wallet.transfer_received)"

echo "==> [3/6] an open request, paid by whoever pays first"
expect "the other rider pays the link" 200 POST "/v1/wallets/$OTHER_ID/money-requests/$OPEN_CODE:pay" "$OTHER" '{"pin":"1357"}'
check "paid by them" "paid payer 4000" "$(body_field '" ".join((d["moneyRequest"]["status"], d["moneyRequest"]["role"], d["wallet"]["balance"]))')"
expect "the payer too" 400 POST "/v1/wallets/$PAYER_ID/money-requests/$OPEN_CODE:pay" "$PAYER" '{"pin":"2580"}'
check "is too late" "the money request is no longer pending" "$(message)"
check "the asker holds 3500" 3500 "$(balance_of "$ASKER_ID" "$ASKER")"

echo "==> [4/6] declining, cancelling, expiring"
expect "another request" 200 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 500 r-2)"
CODE2="$(req 'r["code"]')"
expect "the asker declines it" 403 POST "/v1/wallets/$ASKER_ID/money-requests/$CODE2:decline" "$ASKER" '{}'
expect "the payer declines it" 200 POST "/v1/wallets/$PAYER_ID/money-requests/$CODE2:decline" "$PAYER" '{}'
check "declined" declined "$(req 'r["status"]')"
expect "cancelling it after" 400 POST "/v1/wallets/$ASKER_ID/money-requests/$CODE2:cancel" "$ASKER" '{}'
expect "a third request" 200 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 500 r-3)"
CODE3="$(req 'r["code"]')"
expect "the payer cancels it" 403 POST "/v1/wallets/$PAYER_ID/money-requests/$CODE3:cancel" "$PAYER" '{}'
expect "the asker cancels it" 200 POST "/v1/wallets/$ASKER_ID/money-requests/$CODE3:cancel" "$ASKER" '{}'
check "cancelled" cancelled "$(req 'r["status"]')"
expect "paying a cancelled one" 400 POST "/v1/wallets/$PAYER_ID/money-requests/$CODE3:pay" "$PAYER" '{"pin":"2580"}'
expect "a fourth request" 200 POST "$REQUESTS" "$ASKER" "$(ask "$PAYER_PHONE" 500 r-4)"
CODE4="$(req 'r["code"]')"
sql ride-wallet-postgres "update money_requests set expires_at = now() - interval '1 minute' where code = '$CODE4';" > /dev/null
expect "paying it once it expired" 400 POST "/v1/wallets/$PAYER_ID/money-requests/$CODE4:pay" "$PAYER" '{"pin":"2580"}'
check "is refused" "the money request has expired" "$(message)"
expect "the asker's expired requests" 200 GET "/v1/wallets/$ASKER_ID/money-requests?role=outgoing&status=expired" "$ASKER"
check "has it" "1 $CODE4 expired" "$(body_field '" ".join((str(len(d["moneyRequests"])), d["moneyRequests"][0]["code"], d["moneyRequests"][0]["status"]))')"
check "the payer still holds 17500" 17500 "$(balance_of "$PAYER_ID" "$PAYER")"

echo "==> [5/6] the statement"
STATEMENT="/v1/wallets/$PAYER_ID/statement?owner_type=OWNER_TYPE_RIDER&from=$START"
expect "the payer's statement" 200 GET "$STATEMENT" "$PAYER"
check "0 -> 17500, 20000 in, 2500 out, two rows" "0 17500 20000 2500 2 IQD" "$(body_field '" ".join((d["openingBalance"], d["closingBalance"], d["totalIn"], d["totalOut"], str(len(d["entries"])), d["currencyCode"]))')"
check "newest first" "TRANSACTION_TYPE_TRANSFER_OUT TRANSACTION_TYPE_TOP_UP" "$(body_field 'd["entries"][0]["type"] + " " + d["entries"][1]["type"]')"
expect "only what went out" 200 GET "$STATEMENT&direction=out" "$PAYER"
check "one row, nothing in" "1 0 2500" "$(body_field '" ".join((str(len(d["entries"])), d["totalIn"], d["totalOut"]))')"
expect "only top-ups" 200 GET "$STATEMENT&types=TRANSACTION_TYPE_TOP_UP" "$PAYER"
check "one row" "1 TRANSACTION_TYPE_TOP_UP" "$(body_field 'str(len(d["entries"])) + " " + d["entries"][0]["type"]')"
expect "a page of one" 200 GET "$STATEMENT&page_size=1" "$PAYER"
TOKEN="$(body_field 'd.get("nextPageToken", "")')"
check "has a next page" 1 "$TOKEN"
expect "the next page" 200 GET "$STATEMENT&page_size=1&page_token=$TOKEN" "$PAYER"
check "the last row" "TRANSACTION_TYPE_TOP_UP " "$(body_field 'd["entries"][0]["type"] + " " + d.get("nextPageToken", "")')"
expect "before anything happened" 200 GET "/v1/wallets/$PAYER_ID/statement?owner_type=OWNER_TYPE_RIDER&from=2020-01-01T00:00:00Z&to=2020-02-01T00:00:00Z" "$PAYER"
check "is empty" "0 0 0" "$(body_field '" ".join((d["openingBalance"], d["closingBalance"], str(len(d.get("entries", [])))))')"
expect "both ways at once" 400 GET "$STATEMENT&direction=both" "$PAYER"
expect "two years" 400 GET "/v1/wallets/$PAYER_ID/statement?owner_type=OWNER_TYPE_RIDER&from=2023-01-01T00:00:00Z&to=2025-01-01T00:00:00Z" "$PAYER"
expect "another rider's" 403 GET "$STATEMENT" "$OTHER"
expect "without a token" 401 GET "$STATEMENT" ""

echo "==> [6/6] who reads a rider's unpaid fees"
expect "the rider" 200 GET "/v1/wallets/$PAYER_ID/dues" "$PAYER"
check "owes nothing, may request trips" "0 True" "$(body_field 'd["outstanding"] + " " + str(d.get("canRequestTrips", False))')"
expect "another rider" 403 GET "/v1/wallets/$PAYER_ID/dues" "$OTHER"
DUES="$({ grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "{\"rider_id\":\"$PAYER_ID\"}" "$WALLET_ADDR" ride.wallet.v1.WalletService/GetRiderDues 2>&1 || true; } | tr -d '\n ')"
check "a service, with the internal token" True "$(python3 -c 'import json,sys; d=json.loads(sys.argv[1]); print(d.get("canRequestTrips", False))' "$DUES" 2> /dev/null || echo "$DUES")"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: riders ask each other for money and pay it once with their PIN; the statement adds up; only the rider reads their fees"
