#!/usr/bin/env bash
# End-to-end test of the wallet PIN and of riders sending each other money,
# on the real services, through the gateway. Run from the ride-platform repo
# root:
#   bash scripts/e2e/test-wallet-transfers.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken) and grpcurl. identity-service only accepts a token
# whose session it has on file, so the script puts its people straight into
# the identity database (an identity, its phone and a session each) and
# mints their tokens for those sessions. No code is sent anywhere.
#
# What it proves:
#   1. a person sets a wallet PIN (not an easy one); changing it takes the
#      current one, and a wrong one counts
#   2. a rider sends money by phone with their PIN: the recipient must be a
#      registered rider, not themselves; the money moves with a ledger row on
#      each side; a retry with the same key is the same transfer; both see it
#      in their history; the recipient is told
#   3. the limits: the least per transfer, how many in a day, the balance
#   4. five wrong PINs lock it (even the right one then); signing in again
#      lets a new PIN be set without the old, and lifts the lock
#   5. identity's internal methods take only the internal token
#
# It creates three people (two riders and one person with no rider profile)
# and removes them again; a wallet config row it adds for the limits is
# removed too.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
IDENTITY_ADDR="localhost:50051"
WALLET_ADDR="localhost:50058"
PROTOSET="/tmp/ride.binpb"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
SENDER_IDENTITY="$(uuid)"
RECIPIENT_IDENTITY="$(uuid)"
NOBODY_IDENTITY="$(uuid)"
SENDER_SESSION="$(uuid)"
RECIPIENT_SESSION="$(uuid)"
NOBODY_SESSION="$(uuid)"
# Phones no real person has: +96479 and the run's last eight digits.
SUFFIX="$(printf '%08d' $((RUN % 100000000)))"
SENDER_PHONE="+964791${SUFFIX:1}"
RECIPIENT_PHONE="+964792${SUFFIX:1}"
NOBODY_PHONE="+964793${SUFFIX:1}"
UNKNOWN_PHONE="+964794${SUFFIX:1}"
UNKNOWN="$(uuid)"

INTERNAL_TOKEN=""
for env_file in services/wallet-service/.env services/trip-service/.env; do
  [ -z "$INTERNAL_TOKEN" ] && [ -f "$env_file" ] && INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' "$env_file" | head -1 | tr -d '"')"
done
INTERNAL_TOKEN="${INTERNAL_TOKEN:-dev-internal-service-token-change-me}"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
SENDER_ID=""
RECIPIENT_ID=""
CONFIG_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  if [ -n "$CONFIG_ID" ]; then
    sql ride-wallet-postgres "delete from wallet_configs where id = '$CONFIG_ID';" > /dev/null 2>&1 || true
  fi
  if [ -n "$SENDER_ID$RECIPIENT_ID" ]; then
    local riders="'${SENDER_ID:-$UNKNOWN}', '${RECIPIENT_ID:-$UNKNOWN}'"
    sql ride-wallet-postgres "
      delete from wallet_transactions where wallet_id in (select id from wallets where owner_id in ($riders));
      delete from wallet_transfers where sender_rider_id in ($riders) or recipient_rider_id in ($riders);
      delete from wallets where owner_id in ($riders);" > /dev/null 2>&1 || true
    sql ride-notification-postgres "delete from notifications where recipient_id in ($riders);" > /dev/null 2>&1 || true
    sql ride-rider-postgres "delete from riders where id in ($riders);" > /dev/null 2>&1 || true
  fi
  sql ride-identity-postgres "delete from identities where id in ('$SENDER_IDENTITY', '$RECIPIENT_IDENTITY', '$NOBODY_IDENTITY');" > /dev/null 2>&1 || true
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

grpc_call() { # <token or ""> <service/method> <json>: prints the gRPC error code
  local auth=()
  [ -n "$1" ] && auth=(-H "authorization: Bearer $1")
  { grpcurl -plaintext -protoset "$PROTOSET" "${auth[@]}" -d "$3" "$IDENTITY_ADDR" "$2" 2>&1 || true; } \
    | sed -n 's/^ *Code: *//p' | head -1
}

send() { # <token> <sender rider id> <phone> <amount> <pin> <key> [note]
  echo "{\"riderId\":\"$2\",\"recipientPhone\":\"$3\",\"amount\":\"$4\",\"pin\":\"$5\",\"idempotencyKey\":\"$6\",\"note\":\"${7:-}\"}"
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
person "$SENDER_IDENTITY" "$SENDER_SESSION" "$SENDER_PHONE"
person "$RECIPIENT_IDENTITY" "$RECIPIENT_SESSION" "$RECIPIENT_PHONE"
person "$NOBODY_IDENTITY" "$NOBODY_SESSION" "$NOBODY_PHONE"
SENDER="$(mint "$SENDER_IDENTITY" "$SENDER_SESSION")"
RECIPIENT="$(mint "$RECIPIENT_IDENTITY" "$RECIPIENT_SESSION")"
NOBODY="$(mint "$NOBODY_IDENTITY" "$NOBODY_SESSION")"

echo "==> [0/5] two riders (one with 20000 in the wallet) and a person who is no rider"
expect "the sender" 200 POST /v1/riders "$SENDER" "{\"identityId\":\"$SENDER_IDENTITY\",\"displayName\":\"E2E Sender\"}"
SENDER_ID="$(body_field 'd["rider"]["id"]')"
expect "the recipient" 200 POST /v1/riders "$RECIPIENT" "{\"identityId\":\"$RECIPIENT_IDENTITY\",\"displayName\":\"E2E Recipient\"}"
RECIPIENT_ID="$(body_field 'd["rider"]["id"]')"
grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" \
  -d "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$SENDER_ID\",\"amount\":\"20000\",\"idempotency_key\":\"e2e-transfers-$RUN\"}" \
  "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp > /dev/null
expect "the sender's wallet" 200 GET "/v1/wallets/$SENDER_ID?owner_type=OWNER_TYPE_RIDER" "$SENDER"
check "holds 20000" 20000 "$(body_field 'd["wallet"]["balance"]')"

echo "==> [1/5] the wallet PIN"
expect "without a token" 401 GET /v1/me/wallet-pin ""
expect "no PIN yet" 200 GET /v1/me/wallet-pin "$SENDER"
check "not set" "False 5" "$(body_field 'str(d.get("isSet", False)) + " " + str(d["attemptsLeft"])')"
expect "1234 is too easy" 400 PUT /v1/me/wallet-pin "$SENDER" '{"newPin":"1234"}'
expect "three digits" 400 PUT /v1/me/wallet-pin "$SENDER" '{"newPin":"258"}'
expect "2580" 200 PUT /v1/me/wallet-pin "$SENDER" '{"newPin":"2580"}'
check "set" True "$(body_field 'd["isSet"]')"
# Long after signing in, changing it takes the current one.
sql ride-identity-postgres "update auth_sessions set created_at = now() - interval '1 hour' where id = '$SENDER_SESSION';" > /dev/null
expect "a change without the current PIN" 400 PUT /v1/me/wallet-pin "$SENDER" '{"newPin":"1357"}'
expect "with a wrong current PIN" 403 PUT /v1/me/wallet-pin "$SENDER" '{"newPin":"1357","currentPin":"9999"}'
expect "the PIN" 200 GET /v1/me/wallet-pin "$SENDER"
check "the wrong one counted" 4 "$(body_field 'd["attemptsLeft"]')"
expect "with the current PIN" 200 PUT /v1/me/wallet-pin "$SENDER" '{"newPin":"1357","currentPin":"2580"}'
check "the count starts again" 5 "$(body_field 'd["attemptsLeft"]')"
expect "the recipient's PIN" 200 PUT /v1/me/wallet-pin "$RECIPIENT" '{"newPin":"2468"}'

echo "==> [2/5] sending money"
expect "a wrong PIN" 403 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$RECIPIENT_PHONE" 5000 2580 "k-wrong")"
check "says how many are left" "wrong PIN: 4 attempts left before it locks" "$(message)"
expect "another rider's wallet" 403 POST "/v1/wallets/$SENDER_ID/transfers" "$RECIPIENT" "$(send "$RECIPIENT" "$SENDER_ID" "$RECIPIENT_PHONE" 5000 2468 "k-other")"
expect "a local number" 400 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "0770123456" 5000 1357 "k-local")"
expect "a phone nobody has" 404 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$UNKNOWN_PHONE" 5000 1357 "k-unknown")"
expect "a person who is no rider" 404 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$NOBODY_PHONE" 5000 1357 "k-nobody")"
expect "to themselves" 400 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$SENDER_PHONE" 5000 1357 "k-self")"
expect "less than the least" 400 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$RECIPIENT_PHONE" 100 1357 "k-little")"
expect "5000 to the recipient" 200 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$RECIPIENT_PHONE" 5000 1357 "k-1" "dinner")"
TRANSFER_ID="$(body_field 'd["transfer"]["id"]')"
check "sent, to their phone, and what is left" "sent $RECIPIENT_PHONE 5000 15000" "$(body_field '" ".join((d["transfer"]["direction"], d["transfer"]["counterpartPhone"], d["transfer"]["amount"], d["wallet"]["balance"]))')"
expect "the same key again" 200 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$RECIPIENT_PHONE" 5000 1357 "k-1" "dinner")"
check "is the same transfer, nothing more moved" "$TRANSFER_ID 15000" "$(body_field 'd["transfer"]["id"] + " " + d["wallet"]["balance"]')"
expect "the key for another amount" 409 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$RECIPIENT_PHONE" 6000 1357 "k-1")"
expect "the recipient's wallet" 200 GET "/v1/wallets/$RECIPIENT_ID?owner_type=OWNER_TYPE_RIDER" "$RECIPIENT"
check "holds 5000" 5000 "$(body_field 'd["wallet"]["balance"]')"
expect "the recipient's transfers" 200 GET "/v1/wallets/$RECIPIENT_ID/transfers" "$RECIPIENT"
check "received from the sender, with the note" "received $SENDER_PHONE 5000 dinner" "$(body_field '" ".join((d["transfers"][0]["direction"], d["transfers"][0]["counterpartPhone"], d["transfers"][0]["amount"], d["transfers"][0]["note"]))')"
expect "the recipient's ledger" 200 GET "/v1/wallets/$RECIPIENT_ID/transactions?owner_type=OWNER_TYPE_RIDER" "$RECIPIENT"
check "a transfer_in row for it" "TRANSACTION_TYPE_TRANSFER_IN $TRANSFER_ID" "$(body_field 'd["transactions"][0]["type"] + " " + d["transactions"][0]["transferId"]')"
expect "the sender's ledger" 200 GET "/v1/wallets/$SENDER_ID/transactions?owner_type=OWNER_TYPE_RIDER" "$SENDER"
check "a transfer_out row" "TRANSACTION_TYPE_TRANSFER_OUT -5000" "$(body_field 'd["transactions"][0]["type"] + " " + d["transactions"][0]["amount"]')"
check "the recipient is told" True "$(notified "$RECIPIENT_ID" "$RECIPIENT" wallet.transfer_received)"

echo "==> [3/5] the limits"
CONFIG_ID="$(sql ride-wallet-postgres "
  insert into wallet_configs (currency_code, commission_rate, suspension_threshold, minimum_payout_amount,
                              max_change_credit, transfer_min_amount, transfer_max_amount,
                              transfer_daily_amount, transfer_daily_count)
  select currency_code, commission_rate, suspension_threshold, minimum_payout_amount,
         max_change_credit, 250, 10000, 20000, 3
  from wallet_configs order by created_at desc limit 1
  returning id;" | head -1)"
expect "more than the most" 400 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$RECIPIENT_PHONE" 10001 1357 "k-big")"
expect "a second one today" 200 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$RECIPIENT_PHONE" 1000 1357 "k-2")"
expect "a third" 200 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$RECIPIENT_PHONE" 1000 1357 "k-3")"
expect "a fourth in a day" 400 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" "$(send "$SENDER" "$SENDER_ID" "$RECIPIENT_PHONE" 1000 1357 "k-4")"
check "is one too many" "you have sent as many transfers as allowed in 24 hours" "$(message)"
expect "more than the recipient holds" 400 POST "/v1/wallets/$RECIPIENT_ID/transfers" "$RECIPIENT" "$(send "$RECIPIENT" "$RECIPIENT_ID" "$SENDER_PHONE" 9000 2468 "r-1")"
check "is refused" "insufficient wallet balance" "$(message)"
expect "the sender's wallet" 200 GET "/v1/wallets/$SENDER_ID?owner_type=OWNER_TYPE_RIDER" "$SENDER"
check "holds 13000" 13000 "$(body_field 'd["wallet"]["balance"]')"

echo "==> [4/5] wrong PINs lock it"
for i in 1 2 3 4; do
  http POST "/v1/wallets/$RECIPIENT_ID/transfers" "$RECIPIENT" "$(send "$RECIPIENT" "$RECIPIENT_ID" "$SENDER_PHONE" 500 1111 "lock-$i")" > /dev/null
done
expect "the fifth wrong PIN" 400 POST "/v1/wallets/$RECIPIENT_ID/transfers" "$RECIPIENT" "$(send "$RECIPIENT" "$RECIPIENT_ID" "$SENDER_PHONE" 500 1111 "lock-5")"
check "locks it" True "$(body_field 'd["message"].startswith("the PIN is locked")')"
expect "the right PIN now" 400 POST "/v1/wallets/$RECIPIENT_ID/transfers" "$RECIPIENT" "$(send "$RECIPIENT" "$RECIPIENT_ID" "$SENDER_PHONE" 500 2468 "lock-6")"
check "is locked too" True "$(body_field 'd["message"].startswith("the PIN is locked")')"
expect "the PIN" 200 GET /v1/me/wallet-pin "$RECIPIENT"
check "shows the lock" "True 0" "$(body_field 'str(bool(d.get("lockedUntil"))) + " " + str(d["attemptsLeft"])')"
# The recipient forgot it and signed in again moments ago (their session is fresh).
expect "a new PIN without the old" 200 PUT /v1/me/wallet-pin "$RECIPIENT" '{"newPin":"8642"}'
check "no lock any more" "False 5" "$(body_field 'str(bool(d.get("lockedUntil"))) + " " + str(d["attemptsLeft"])')"
expect "sending with it" 200 POST "/v1/wallets/$RECIPIENT_ID/transfers" "$RECIPIENT" "$(send "$RECIPIENT" "$RECIPIENT_ID" "$SENDER_PHONE" 500 8642 "back-1")"
check "goes through" "sent $SENDER_PHONE" "$(body_field 'd["transfer"]["direction"] + " " + d["transfer"]["counterpartPhone"]')"

echo "==> [5/5] identity's internal methods"
check "VerifyWalletPin with a person's token" PermissionDenied "$(grpc_call "$SENDER" ride.identity.v1.WalletPinService/VerifyWalletPin "{\"identity_id\":\"$SENDER_IDENTITY\",\"pin\":\"1357\"}")"
check "FindIdentityByPhone with a person's token" PermissionDenied "$(grpc_call "$SENDER" ride.identity.v1.IdentityDirectoryService/FindIdentityByPhone "{\"phone_number\":\"$RECIPIENT_PHONE\"}")"
check "with no token" Unauthenticated "$(grpc_call "" ride.identity.v1.IdentityDirectoryService/FindIdentityByPhone "{\"phone_number\":\"$RECIPIENT_PHONE\"}")"
FOUND="$({ grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "{\"phone_number\":\"$RECIPIENT_PHONE\"}" "$IDENTITY_ADDR" ride.identity.v1.IdentityDirectoryService/FindIdentityByPhone 2>&1 || true; } | tr -d '\n ')"
check "with the internal token, found" True "$(python3 -c 'import json,sys; d=json.loads(sys.argv[1]); print(d.get("found", False) and d.get("identityId") == sys.argv[2])' "$FOUND" "$RECIPIENT_IDENTITY" 2> /dev/null || echo "$FOUND")"
expect "no route to them from outside" 404 POST /v1/identity/directory:find "$SENDER" '{}'

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: a rider sets a PIN and sends money by phone with it, within the limits; wrong PINs lock it, and signing in again resets it"
