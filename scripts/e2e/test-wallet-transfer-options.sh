#!/usr/bin/env bash
# End-to-end test of what the app reads before a transfer: the transfer
# options and the recipient lookup, on the real services, through the
# gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-wallet-transfer-options.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken). Like test-wallet-transfers.sh it puts its people
# straight into the identity database and mints their tokens. No code is
# sent anywhere.
#
# What it proves:
#   1. the options: the limits of the active wallet config, and what the
#      rider sent in the last 24 hours (after a transfer, it counts)
#   2. the lookup finds exactly whom a transfer would reach: a rider (with a
#      short name), not a phone nobody has, not a person with no rider
#      profile, and says when it is the rider's own phone; the phone is never
#      in the answer
#   3. only the wallet's own rider may ask, and a bad phone is refused
#   4. lookups are counted: past the hourly limit the answer is 429, and
#      nothing is counted for a bad phone
#
# It creates three people (two riders and one person with no rider profile)
# and removes them again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
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
SENDER_PHONE="+964795${SUFFIX:1}"
RECIPIENT_PHONE="+964796${SUFFIX:1}"
NOBODY_PHONE="+964797${SUFFIX:1}"
UNKNOWN_PHONE="+964798${SUFFIX:1}"
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
SENDER_ID=""
RECIPIENT_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  if [ -n "$SENDER_ID$RECIPIENT_ID" ]; then
    local riders="'${SENDER_ID:-$UNKNOWN}', '${RECIPIENT_ID:-$UNKNOWN}'"
    sql ride-wallet-postgres "
      delete from transfer_recipient_lookups where rider_id in ($riders);
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

# person <identity> <session> <phone>: an identity with a phone and a live
# session, as signing in leaves them.
person() {
  sql ride-identity-postgres "
    insert into identities (id) values ('$1');
    insert into identity_identifiers (identity_id, identifier_type, normalized_value, verified_at)
      values ('$1', 'phone', '$3', now());
    insert into auth_sessions (id, identity_id, expires_at) values ('$2', '$1', now() + interval '1 day');" > /dev/null
}

lookup() { # <phone>
  echo "{\"phone\":\"$1\"}"
}

lookups_counted() {
  sql ride-wallet-postgres "select count(*) from transfer_recipient_lookups where rider_id = '$SENDER_ID';" | tr -d ' '
}

buf build -o "$PROTOSET"
person "$SENDER_IDENTITY" "$SENDER_SESSION" "$SENDER_PHONE"
person "$RECIPIENT_IDENTITY" "$RECIPIENT_SESSION" "$RECIPIENT_PHONE"
person "$NOBODY_IDENTITY" "$NOBODY_SESSION" "$NOBODY_PHONE"
SENDER="$(mint "$SENDER_IDENTITY" "$SENDER_SESSION")"
RECIPIENT="$(mint "$RECIPIENT_IDENTITY" "$RECIPIENT_SESSION")"

echo "==> [0/4] two riders (one with 20000 and a PIN) and a person who is no rider"
expect "the sender" 200 POST /v1/riders "$SENDER" "{\"identityId\":\"$SENDER_IDENTITY\",\"displayName\":\"E2E Sender\"}"
SENDER_ID="$(body_field 'd["rider"]["id"]')"
expect "the recipient" 200 POST /v1/riders "$RECIPIENT" "{\"identityId\":\"$RECIPIENT_IDENTITY\",\"displayName\":\"Haidar Kamal\"}"
RECIPIENT_ID="$(body_field 'd["rider"]["id"]')"
grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" \
  -d "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$SENDER_ID\",\"amount\":\"20000\",\"idempotency_key\":\"e2e-transfer-options-$RUN\"}" \
  "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp > /dev/null
expect "the sender's PIN" 200 PUT /v1/me/wallet-pin "$SENDER" '{"newPin":"2580"}'

echo "==> [1/4] the transfer options"
CONFIG="$(sql ride-wallet-postgres "select transfer_min_amount::numeric::text || ' ' || transfer_max_amount::numeric::text || ' ' ||
  transfer_daily_amount::numeric::text || ' ' || transfer_daily_count from wallet_configs order by created_at desc limit 1;" | head -1)"
expect "the sender's options" 200 GET "/v1/wallets/$SENDER_ID/transfer-options" "$SENDER"
OPTIONS="$(body_field 'd["minAmount"] + " " + d["maxAmount"] + " " + d["dailyAmount"] + " " + str(d["dailyCount"])')"
check "are the wallet config's limits" "$(python3 -c 'import sys; a=sys.argv[1].split(); print(" ".join([str(float(a[0])), str(float(a[1])), str(float(a[2])), a[3]]))' "$CONFIG")" \
  "$(python3 -c 'import sys; a=sys.argv[1].split(); print(" ".join([str(float(a[0])), str(float(a[1])), str(float(a[2])), a[3]]))' "$OPTIONS")"
check "nothing sent yet" "0 0" "$(body_field 'd.get("sentAmount", "0") + " " + str(d.get("sentCount", 0))')"
check "in the deployment's currency" True "$(body_field 'len(d["currencyCode"]) == 3')"
expect "5000 to the recipient" 200 POST "/v1/wallets/$SENDER_ID/transfers" "$SENDER" \
  "{\"riderId\":\"$SENDER_ID\",\"recipientPhone\":\"$RECIPIENT_PHONE\",\"amount\":\"5000\",\"pin\":\"2580\",\"idempotencyKey\":\"opt-1\"}"
expect "the options after it" 200 GET "/v1/wallets/$SENDER_ID/transfer-options" "$SENDER"
check "count it" "5000 1" "$(body_field 'd["sentAmount"] + " " + str(d["sentCount"])')"
expect "another rider's options" 403 GET "/v1/wallets/$SENDER_ID/transfer-options" "$RECIPIENT"
expect "without a token" 401 GET "/v1/wallets/$SENDER_ID/transfer-options" ""

echo "==> [2/4] looking up a recipient"
expect "a rider" 200 POST "/v1/wallets/$SENDER_ID/transfer-recipients:lookup" "$SENDER" "$(lookup "$RECIPIENT_PHONE")"
check "found, with a short name" "found|Haidar K." "$(body_field 'd["result"] + "|" + d.get("displayName", "")')"
check "the phone is not in the answer" False "$(body_field "'$RECIPIENT_PHONE' in json.dumps(d)")"
expect "a phone nobody has" 200 POST "/v1/wallets/$SENDER_ID/transfer-recipients:lookup" "$SENDER" "$(lookup "$UNKNOWN_PHONE")"
check "not found, no name" "not_found|" "$(body_field 'd["result"] + "|" + d.get("displayName", "")')"
expect "a person who is no rider" 200 POST "/v1/wallets/$SENDER_ID/transfer-recipients:lookup" "$SENDER" "$(lookup "$NOBODY_PHONE")"
check "not found either (a transfer would not reach them)" "not_found|" "$(body_field 'd["result"] + "|" + d.get("displayName", "")')"
expect "their own phone" 200 POST "/v1/wallets/$SENDER_ID/transfer-recipients:lookup" "$SENDER" "$(lookup "$SENDER_PHONE")"
check "self" "self|" "$(body_field 'd["result"] + "|" + d.get("displayName", "")')"
check "four lookups counted" 4 "$(lookups_counted)"

echo "==> [3/4] who may ask"
expect "for another rider's wallet" 403 POST "/v1/wallets/$SENDER_ID/transfer-recipients:lookup" "$RECIPIENT" "$(lookup "$RECIPIENT_PHONE")"
expect "without a token" 401 POST "/v1/wallets/$SENDER_ID/transfer-recipients:lookup" "" "$(lookup "$RECIPIENT_PHONE")"
expect "a local number" 400 POST "/v1/wallets/$SENDER_ID/transfer-recipients:lookup" "$SENDER" "$(lookup "07701234567")"
check "is not counted" 4 "$(lookups_counted)"

echo "==> [4/4] the hourly limit"
# Lookups made earlier this hour, up to one short of the limit.
sql ride-wallet-postgres "insert into transfer_recipient_lookups (rider_id, looked_up_at)
  select '$SENDER_ID', now() - interval '10 minutes' from generate_series(1, $((PER_HOUR - 5)));" > /dev/null
expect "the last one this hour" 200 POST "/v1/wallets/$SENDER_ID/transfer-recipients:lookup" "$SENDER" "$(lookup "$RECIPIENT_PHONE")"
expect "one more" 429 POST "/v1/wallets/$SENDER_ID/transfer-recipients:lookup" "$SENDER" "$(lookup "$RECIPIENT_PHONE")"
check "is refused and not counted" "$PER_HOUR" "$(lookups_counted)"
# An hour later the hourly limit is clear again.
sql ride-wallet-postgres "update transfer_recipient_lookups set looked_up_at = looked_up_at - interval '1 hour' where rider_id = '$SENDER_ID';" > /dev/null
expect "an hour later" 200 POST "/v1/wallets/$SENDER_ID/transfer-recipients:lookup" "$SENDER" "$(lookup "$RECIPIENT_PHONE")"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: the app reads the transfer limits and what was sent today, and looks up exactly whom a transfer reaches, a limited number of times"
