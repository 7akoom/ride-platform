#!/usr/bin/env bash
# End-to-end test of tips and of top-ups through a payment provider, on the
# real services, through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-wallet-tips-topups.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken), grpcurl, and wallet-service with migration 00016.
# No payment is started at ZainCash: the top-up checks stop before it, and a
# ZainCash notification is played with ZAINCASH_WEBHOOK_SECRET from
# services/wallet-service/.env (skipped when it is empty).
#
# What it proves:
#   1. a rider tips the driver of their completed trip from their wallet,
#      once, within the limits; the driver gets all of it; the trip's
#      settlement shows it; nobody else tips that trip
#   2. a top-up is checked before any provider is asked: the owner's own
#      wallet, the limits, a known provider, an amount it takes
#   3. the provider's notification credits the rider's wallet once, and the
#      app reads how the top-up ended
#
# It creates two riders and a driver, settles a made-up trip with the
# internal token, and removes it all again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
WALLET_ADDR="localhost:50058"
PROTOSET="/tmp/ride.binpb"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
DRIVER_IDENTITY="$(uuid)"
TRIP="$(uuid)"
UNSETTLED="$(uuid)"
UNKNOWN="$(uuid)"
PLATE="E2E-TIP-$RUN"

env_value() { # <key>: from services/wallet-service/.env
  [ -f services/wallet-service/.env ] && sed -n "s/^$1=//p" services/wallet-service/.env | head -1 | tr -d '"' || true
}

INTERNAL_TOKEN="$(env_value INTERNAL_SERVICE_TOKEN)"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-dev-internal-service-token-change-me}"
WEBHOOK_SECRET="$(env_value ZAINCASH_WEBHOOK_SECRET)"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
OTHER_ID=""
DRIVER_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  local owners="'${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}', '${DRIVER_ID:-$UNKNOWN}'"
  sql ride-wallet-postgres "
    delete from trip_tips where trip_id = '$TRIP';
    delete from provider_topups where owner_id in ($owners);
    delete from trip_settlements where trip_id = '$TRIP';
    delete from wallet_transactions where wallet_id in (select id from wallets where owner_id in ($owners));
    delete from wallets where owner_id in ($owners);" > /dev/null 2>&1 || true
  sql ride-driver-postgres "delete from drivers where vehicle_plate_number = '$PLATE';" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID$OTHER_ID" ]; then
    sql ride-rider-postgres "delete from riders where id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');" > /dev/null 2>&1 || true
  fi
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

message() { body_field 'd.get("message", "")'; }

internal_call() { # <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$2" "$WALLET_ADDR" "ride.wallet.v1.WalletService/$1" > /dev/null
}

balance_of() { # <owner type> <owner id> <token>
  http GET "/v1/wallets/$2?owner_type=$1" "$3" > /dev/null
  body_field 'd["wallet"]["balance"].split(".")[0]'
}

tip() { # <amount> <key>
  echo "{\"amount\":\"$1\",\"idempotencyKey\":\"$2\"}"
}

topup() { # <amount> [provider]
  echo "{\"ownerType\":\"OWNER_TYPE_RIDER\",\"amount\":\"$1\",\"provider\":\"${2:-}\"}"
}

# zaincash_token <reference> <transaction id> <status>: a notification signed
# like ZainCash's (HS256 with the webhook secret).
zaincash_token() {
  python3 - "$WEBHOOK_SECRET" "$1" "$2" "$3" <<'PY'
import base64, hashlib, hmac, json, sys
secret, reference, transaction, status = sys.argv[1:5]
b64 = lambda raw: base64.urlsafe_b64encode(raw).rstrip(b"=").decode()
head = b64(json.dumps({"alg": "HS256", "typ": "JWT"}).encode())
body = b64(json.dumps({"transactionId": transaction, "merchantReferenceId": reference, "currentStatus": status}).encode())
sig = b64(hmac.new(secret.encode(), f"{head}.{body}".encode(), hashlib.sha256).digest())
print(f"{head}.{body}.{sig}")
PY
}

[ "$(sql ride-wallet-postgres "select to_regclass('public.trip_tips') is not null")" = t ] \
  || { echo "ABORT: the tip tables do not exist: apply wallet-service migration 00016 (goose up)" >&2; exit 2; }

buf build -o "$PROTOSET"
RIDER="$(mint "$RIDER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"

echo "==> [0/3] two riders, a driver, and a 5000 trip paid from the rider's wallet"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Tip Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "another rider" 200 POST /v1/riders "$OTHER" "{\"identityId\":\"$OTHER_IDENTITY\",\"displayName\":\"E2E Tip Other\"}"
OTHER_ID="$(body_field 'd["rider"]["id"]')"
expect "a driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Tip Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
internal_call TopUp "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER_ID\",\"amount\":\"10000\",\"idempotency_key\":\"e2e-tip-$RUN\"}"
internal_call SettleTrip "{\"trip_id\":\"$TRIP\",\"rider_id\":\"$RIDER_ID\",\"driver_id\":\"$DRIVER_ID\",\"fare_amount\":\"5000\",\"payment_method\":\"PAYMENT_METHOD_WALLET\"}"
check "the rider holds 5000" 5000 "$(balance_of OWNER_TYPE_RIDER "$RIDER_ID" "$RIDER")"
DRIVER_BEFORE="$(balance_of OWNER_TYPE_DRIVER "$DRIVER_ID" "$DRIVER")"

echo "==> [1/3] tipping the driver"
TIP="/v1/wallets/$RIDER_ID/trips/$TRIP/tip"
expect "without a token" 401 POST "$TIP" "" "$(tip 1000 "t-$RUN")"
expect "from another rider's wallet" 403 POST "$TIP" "$OTHER" "$(tip 1000 "t-$RUN")"
expect "another rider, their own wallet" 404 POST "/v1/wallets/$OTHER_ID/trips/$TRIP/tip" "$OTHER" "$(tip 1000 "o-$RUN")"
expect "a trip not settled" 404 POST "/v1/wallets/$RIDER_ID/trips/$UNSETTLED/tip" "$RIDER" "$(tip 1000 "u-$RUN")"
expect "below the least" 400 POST "$TIP" "$RIDER" "$(tip 100 "x1-$RUN")"
expect "more than the rider holds" 400 POST "$TIP" "$RIDER" "$(tip 6000 "x2-$RUN")"
expect "1000" 200 POST "$TIP" "$RIDER" "$(tip 1000 "t-$RUN")"
TIP_ID="$(body_field 'd["tip"]["id"]')"
check "the rider holds 4000" 4000 "$(body_field 'd["wallet"]["balance"].split(".")[0]')"
expect "the same key again" 200 POST "$TIP" "$RIDER" "$(tip 1000 "t-$RUN")"
check "is the same tip" "$TIP_ID 4000" "$(body_field '" ".join((d["tip"]["id"], d["wallet"]["balance"].split(".")[0]))')"
expect "a second tip" 400 POST "$TIP" "$RIDER" "$(tip 500 "t2-$RUN")"
check "is refused" "this trip was tipped already" "$(message)"
check "the driver got all of it" "$((DRIVER_BEFORE + 1000))" "$(balance_of OWNER_TYPE_DRIVER "$DRIVER_ID" "$DRIVER")"
expect "the driver's settlement" 200 GET "/v1/wallets/$DRIVER_ID/trips/$TRIP/settlement?owner_type=OWNER_TYPE_DRIVER" "$DRIVER"
check "shows the tip" 1000 "$(body_field 'd["tipAmount"]')"
expect "the driver's tips" 200 GET "/v1/wallets/$DRIVER_ID/statement?owner_type=OWNER_TYPE_DRIVER&types=TRANSACTION_TYPE_TIP" "$DRIVER"
check "one row of 1000" "1 1000" "$(body_field '" ".join((str(len(d["entries"])), d["totalIn"]))')"
check "the driver will be told" 1 "$(sql ride-wallet-postgres "select count(*) from outbox_events where event_type = 'wallet.tip_received' and aggregate_id = '$TIP_ID'")"

echo "==> [2/3] starting a top-up"
TOPUPS="/v1/wallets/$RIDER_ID/topups"
expect "into another rider's wallet" 403 POST "$TOPUPS" "$OTHER" "$(topup 5000)"
expect "below the least" 400 POST "$TOPUPS" "$RIDER" "$(topup 500)"
expect "an unknown provider" 400 POST "$TOPUPS" "$RIDER" "$(topup 5000 paypal)"
expect "a fraction of a dinar" 400 POST "$TOPUPS" "$RIDER" "$(topup 5000.5)"
check "is refused before ZainCash is asked" True "$(message | grep -q 'whole units' && echo True || echo False)"

echo "==> [3/3] ZainCash says it is paid"
if [ -z "$WEBHOOK_SECRET" ]; then
  echo "  skip  ZAINCASH_WEBHOOK_SECRET is empty in services/wallet-service/.env (set any value and restart wallet-service to play a notification)"
else
  REFERENCE="$(sql ride-wallet-postgres "insert into provider_topups (owner_type, owner_id, provider, amount, currency_code, status, provider_transaction_id) values ('rider', '$RIDER_ID', 'zaincash', 5000, 'IQD', 'pending', 'e2e-zc-$RUN') returning external_reference_id" | head -1)"
  TOPUP_ID="$(sql ride-wallet-postgres "select id from provider_topups where external_reference_id = '$REFERENCE'")"
  expect "the app reads it: pending" 200 GET "$TOPUPS/$TOPUP_ID?owner_type=OWNER_TYPE_RIDER" "$RIDER"
  check "pending, zaincash, 5000" "pending zaincash 5000" "$(body_field '" ".join((d["status"], d["provider"], d["amount"]))')"
  expect "another rider reads it" 403 GET "$TOPUPS/$TOPUP_ID?owner_type=OWNER_TYPE_RIDER" "$OTHER"
  expect "a notification signed by someone else" 400 POST /v1/wallet/zaincash/webhook "" "{\"token\":\"$(WEBHOOK_SECRET=wrong zaincash_token "$REFERENCE" "e2e-zc-$RUN" SUCCESS)\"}"
  expect "ZainCash: still pending" 200 POST /v1/wallet/zaincash/webhook "" "{\"token\":\"$(zaincash_token "$REFERENCE" "e2e-zc-$RUN" PENDING)\"}"
  check "nothing credited yet" 4000 "$(balance_of OWNER_TYPE_RIDER "$RIDER_ID" "$RIDER")"
  expect "ZainCash: paid" 200 POST /v1/wallet/zaincash/webhook "" "{\"token\":\"$(zaincash_token "$REFERENCE" "e2e-zc-$RUN" SUCCESS)\"}"
  check "the rider holds 9000" 9000 "$(balance_of OWNER_TYPE_RIDER "$RIDER_ID" "$RIDER")"
  expect "ZainCash says it again" 200 POST /v1/wallet/zaincash/webhook "" "{\"token\":\"$(zaincash_token "$REFERENCE" "e2e-zc-$RUN" SUCCESS)\"}"
  check "credited once" 9000 "$(balance_of OWNER_TYPE_RIDER "$RIDER_ID" "$RIDER")"
  expect "the app reads it: succeeded" 200 GET "$TOPUPS/$TOPUP_ID?owner_type=OWNER_TYPE_RIDER" "$RIDER"
  check "succeeded" succeeded "$(body_field 'd["status"]')"
fi

echo
if [ "$FAILURES" -ne 0 ]; then
  echo "FAIL: $FAILURES check(s) failed"
  exit 1
fi

echo "PASS: a rider tips a completed trip's driver once; top-ups are checked before the provider and credited once from its notification"
