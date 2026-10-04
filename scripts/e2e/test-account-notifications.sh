#!/usr/bin/env bash
# End-to-end test of what people are told about their own money and bookings
# (P11b), on the real services, through the gateway. Run from the
# ride-platform repo root:
#   bash scripts/e2e/test-account-notifications.sh
#
# Needs the local identity signing key, grpcurl, buf, notification-service
# with migration 00017 and staff-service with the owner role.
#
# What it proves, each in the person's own inbox (GET /v1/notifications):
#   1. a driver tipped by a rider hears of it, with the amount
#   2. a driver whose balance falls past the limit is told they are suspended
#      and what clears it; lifted back above it, that they can take trips again
#   3. a driver's payout paid, and one rejected with the reason
#   4. a rider refunded for a trip
#   5. a rider whose ride booked ahead could not be made (no service at the
#      pickup) is told to request one now
#   6. with ZAINCASH_WEBHOOK_SECRET set in services/wallet-service/.env, a
#      ZainCash top-up that went through, with the new balance (skipped
#      otherwise)
#
# It creates a rider, a driver and an owner, settles a made-up trip with the
# internal token, and removes it all again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
WALLET_ADDR="${WALLET_GRPC_ADDRESS:-localhost:50058}"
GRPCURL="${GRPCURL:-grpcurl}"
PROTOSET="${PROTOSET:-/tmp/ride.binpb}"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
DRIVER_IDENTITY="$(uuid)"
OWNER_IDENTITY="${E2E_OWNER_IDENTITY:-$(uuid)}"
OWNER_STAFF_ID="$(uuid)"
TRIP="$(uuid)"
BOOKING="$(uuid)"
UNKNOWN="$(uuid)"
PLATE="NT-$RUN"

env_value() { # <name>: from services/wallet-service/.env
  [ -f services/wallet-service/.env ] && sed -n "s/^$1=//p" services/wallet-service/.env | head -1 | tr -d '"' | tr -d "'" || true
}

INTERNAL_TOKEN="$(env_value INTERNAL_SERVICE_TOKEN)"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-dev-internal-service-token-change-me}"
WEBHOOK_SECRET="$(env_value ZAINCASH_WEBHOOK_SECRET)"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
DRIVER_ID=""

# sql <service> <query>: in the service's database (the compose containers, or
# local databases named in E2E_LOCAL_DBS="wallet=db rider=db ...").
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
  local owners="'${RIDER_ID:-$UNKNOWN}', '${DRIVER_ID:-$UNKNOWN}'"
  sql staff "delete from staff_members where id = '$OWNER_STAFF_ID';" > /dev/null 2>&1 || true
  sql trip "delete from scheduled_trips where id = '$BOOKING';" > /dev/null 2>&1 || true
  sql wallet "
    delete from trip_tips where trip_id = '$TRIP';
    delete from wallet_adjustments where owner_id in ($owners) or driver_id in ($owners);
    delete from payout_requests where driver_id in ($owners);
    delete from provider_topups where owner_id in ($owners);
    delete from trip_settlements where trip_id = '$TRIP';
    delete from wallet_transactions where wallet_id in (select id from wallets where owner_id in ($owners));
    delete from wallets where owner_id in ($owners);" > /dev/null 2>&1 || true
  sql notification "delete from notifications where recipient_id in ($owners);" > /dev/null 2>&1 || true
  sql driver "delete from drivers where id in ($owners);" > /dev/null 2>&1 || true
  sql rider "delete from riders where id in ($owners);" > /dev/null 2>&1 || true
}
trap cleanup EXIT

mint() { go run scripts/tools/devtoken/main.go ${DEVTOKEN_KEY:+-key "$DEVTOKEN_KEY"} -sub "$1"; }

body_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE"
}

http() { # <method> <path> <token> [json body]
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$1" "$BASE$2")
  [ -n "$3" ] && args+=(-H "Authorization: Bearer $3")
  [ -n "${4:-}" ] && args+=(-H 'Content-Type: application/json' -d "$4")
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

internal_call() { # <method> <json>
  "$GRPCURL" -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$2" "$WALLET_ADDR" "ride.wallet.v1.WalletService/$1" > /dev/null
}

# told <label> <RIDER|DRIVER> <owner id> <token> <event key> <text the body must hold> [how many, default 1] [seconds]
told() {
  local label="$1" kind="$2" id="$3" token="$4" key="$5" text="$6" want="${7:-1}" wait="${8:-30}" got="none"

  for _ in $(seq 1 "$wait"); do
    if [ "$(http GET "/v1/notifications?recipient_type=RECIPIENT_TYPE_$kind&recipient_id=$id&limit=50" "$token")" = 200 ]; then
      got="$(body_field "(lambda m: f'{len(m)}|' + str(all('$text' in n.get('body', '') for n in m)))([n for n in d.get('notifications', []) if n.get('eventKey') == '$key'])")"
      [ "$got" = "$want|True" ] && break
    fi
    sleep 1
  done

  check "$label" "$want|True" "$got"
}

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

[ "$(sql notification "select count(*) from notification_templates where event_key = 'wallet.payout_paid'")" = 1 ] \
  || { echo "ABORT: the new templates do not exist: apply notification-service migration 00017 (goose up)" >&2; exit 2; }

[ -n "${E2E_SKIP_BUF:-}" ] || buf build -o "$PROTOSET"
RIDER="$(mint "$RIDER_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"

echo "==> [0/5] a rider with 20000, a driver with 40000 and an owner"
sql staff "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
  values ('$OWNER_STAFF_ID', '$OWNER_IDENTITY', 'e2e-notify-$RUN@ride.test', 'E2E Notify', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values ('$OWNER_STAFF_ID', '$OWNER_ROLE');" > /dev/null
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Notify Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "a driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Notify Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\",\"year\":2020}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
sql driver "update drivers set status = 'active' where id = '$DRIVER_ID';" > /dev/null
internal_call TopUp "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER_ID\",\"amount\":\"20000\",\"idempotency_key\":\"e2e-notify-rider-$RUN\"}"
internal_call TopUp "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRIVER_ID\",\"amount\":\"40000\",\"idempotency_key\":\"e2e-notify-driver-$RUN\"}"
internal_call SettleTrip "{\"trip_id\":\"$TRIP\",\"rider_id\":\"$RIDER_ID\",\"driver_id\":\"$DRIVER_ID\",\"fare_amount\":\"5000\",\"payment_method\":\"PAYMENT_METHOD_WALLET\"}"

echo "==> [1/5] a tip and a refund"
expect "the rider tips 2500" 200 POST "/v1/wallets/$RIDER_ID/trips/$TRIP/tip" "$RIDER" "{\"amount\":\"2500\",\"idempotencyKey\":\"tip-$RUN\"}"
told "the driver hears of the tip" DRIVER "$DRIVER_ID" "$DRIVER" wallet.tip_received "2500.00"
expect "staff refund 2000 of the trip" 200 POST "/v1/admin/trips/$TRIP/refunds" "$OWNER" "{\"amount\":\"2000\",\"driverAmount\":\"0\",\"reason\":\"a longer route\",\"idempotencyKey\":\"refund-$RUN\"}"
told "the rider hears of the refund" RIDER "$RIDER_ID" "$RIDER" wallet.refund_issued "2000.00"

echo "==> [2/5] payouts"
expect "the driver asks for 20000" 200 POST "/v1/drivers/$DRIVER_ID/payouts" "$DRIVER" "{\"amount\":\"20000\",\"destination\":\"ZainCash +9647500000009\",\"idempotencyKey\":\"p1-$RUN\"}"
PAYOUT="$(body_field 'd["payout"]["id"]')"
expect "paid" 200 POST "/v1/admin/payouts/$PAYOUT:markPaid" "$OWNER" "{\"reference\":\"ZC-$RUN\"}"
told "the driver hears it was sent" DRIVER "$DRIVER_ID" "$DRIVER" wallet.payout_paid "20000.00"
expect "another, for 10000" 200 POST "/v1/drivers/$DRIVER_ID/payouts" "$DRIVER" "{\"amount\":\"10000\",\"destination\":\"ZainCash +9647500000009\",\"idempotencyKey\":\"p2-$RUN\"}"
SECOND="$(body_field 'd["payout"]["id"]')"
expect "rejected" 200 POST "/v1/admin/payouts/$SECOND:reject" "$OWNER" '{"reason":"this ZainCash number does not exist"}'
told "the driver hears why it was not sent" DRIVER "$DRIVER_ID" "$DRIVER" wallet.payout_rejected "this ZainCash number does not exist"

echo "==> [3/5] suspended by the balance, then reinstated"
BALANCE="$(sql wallet "select balance from wallets where owner_type = 'driver' and owner_id = '$DRIVER_ID'")"
FLOOR="$(sql wallet "select suspension_threshold from wallet_configs order by created_at desc limit 1")"
TAKE="$(python3 -c "print(int(float('$BALANCE') + float('$FLOOR') + 1000))")"
DUE="$(python3 -c "print(f'{float(\"$FLOOR\") + 1000:.2f}')")"
expect "staff take $TAKE (past the limit)" 200 POST "/v1/admin/wallets/$DRIVER_ID/adjustments" "$OWNER" "{\"ownerType\":\"OWNER_TYPE_DRIVER\",\"amount\":\"-$TAKE\",\"reason\":\"commission owed\",\"idempotencyKey\":\"adj1-$RUN\"}"
told "the driver is told they are suspended and what clears it" DRIVER "$DRIVER_ID" "$DRIVER" driver.suspended "$DUE"
expect "staff take 100 more" 200 POST "/v1/admin/wallets/$DRIVER_ID/adjustments" "$OWNER" "{\"ownerType\":\"OWNER_TYPE_DRIVER\",\"amount\":\"-100\",\"reason\":\"more commission\",\"idempotencyKey\":\"adj2-$RUN\"}"
expect "the driver deposits enough" 200 POST "/v1/admin/wallets/$DRIVER_ID/adjustments" "$OWNER" "{\"ownerType\":\"OWNER_TYPE_DRIVER\",\"amount\":\"$TAKE\",\"reason\":\"paid at the office\",\"idempotencyKey\":\"adj3-$RUN\"}"
told "and that they can take trips again" DRIVER "$DRIVER_ID" "$DRIVER" driver.reinstated ""
told "told once that they were suspended, not again while still below" DRIVER "$DRIVER_ID" "$DRIVER" driver.suspended "" 1 1

echo "==> [4/5] a ride booked ahead that could not be made"
# Due long ago (past the 10 minute grace) and somewhere not served: the
# scheduler (every 15 s) gives up on it at once.
sql trip "
  insert into scheduled_trips (id, rider_id, idempotency_key, scheduled_at, pickup_latitude, pickup_longitude,
                               dropoff_latitude, dropoff_longitude, vehicle_class, payment_method, next_attempt_at)
  values ('$BOOKING', '$RIDER_ID', 'e2e-notify-$RUN', now() - interval '20 minutes', 0.5, 0.5, 0.6, 0.6,
          'economy', 'cash', now());" > /dev/null
told "the rider is told to request a ride now" RIDER "$RIDER_ID" "$RIDER" trip.schedule_failed "" 1 60
check "the booking failed" failed "$(sql trip "select status from scheduled_trips where id = '$BOOKING'")"

echo "==> [5/5] a ZainCash top-up that went through"
if [ -z "$WEBHOOK_SECRET" ]; then
  echo "  skip  ZAINCASH_WEBHOOK_SECRET is empty in services/wallet-service/.env"
else
  REFERENCE="$(sql wallet "insert into provider_topups (owner_type, owner_id, provider, amount, currency_code, status, provider_transaction_id) values ('rider', '$RIDER_ID', 'zaincash', 5000, 'IQD', 'pending', 'e2e-nz-$RUN') returning external_reference_id" | head -1)"
  expect "ZainCash: paid" 200 POST /v1/wallet/zaincash/webhook "" "{\"token\":\"$(zaincash_token "$REFERENCE" "e2e-nz-$RUN" SUCCESS)\"}"
  expect "ZainCash says it again" 200 POST /v1/wallet/zaincash/webhook "" "{\"token\":\"$(zaincash_token "$REFERENCE" "e2e-nz-$RUN" SUCCESS)\"}"
  NEW_BALANCE="$(sql wallet "select to_char(balance, 'FM999999990.00') from wallets where owner_type = 'rider' and owner_id = '$RIDER_ID'")"
  told "the rider is told once, with the new balance" RIDER "$RIDER_ID" "$RIDER" wallet.topped_up "$NEW_BALANCE"
fi

echo
if [ "$FAILURES" -ne 0 ]; then
  echo "FAIL: $FAILURES check(s) failed"
  exit 1
fi

echo "PASS: people are told about tips, refunds, payouts, their standing, failed bookings and top-ups"
