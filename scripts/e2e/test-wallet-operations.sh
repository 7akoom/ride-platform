#!/usr/bin/env bash
# End-to-end test of staff money operations and drivers' payouts on the real
# services, through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-wallet-operations.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken), grpcurl, wallet-service with migration 00015 and
# staff-service knowing wallets.read, wallets.adjust and payouts.manage.
#
# What it proves:
#   1. staff with wallets.read look at any wallet; with wallets.adjust they
#      credit or debit it with a reason, once per key; a rider never goes
#      below zero; neither a rider nor the operations role may do any of it
#   2. a settled trip is refunded, part of it taken back from the driver, and
#      never past its fare; the trip's refunds add up
#   3. a driver's payout is held at once, one open at a time; staff approve
#      it and mark it paid with a reference; a rejected one gives the money
#      back; the driver sees how each ended
#   4. staff read any wallet's statement
#
# It creates a rider, a driver and two staff members, settles a made-up trip
# with the internal token, and removes it all again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
WALLET_ADDR="localhost:50058"
PROTOSET="/tmp/ride.binpb"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
DRIVER_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OPERATOR_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
OPERATOR_STAFF_ID="$(uuid)"
TRIP="$(uuid)"
UNSETTLED="$(uuid)"
UNKNOWN="$(uuid)"
PLATE="E2E-OPS-$RUN"

INTERNAL_TOKEN=""
for env_file in services/wallet-service/.env services/trip-service/.env; do
  [ -z "$INTERNAL_TOKEN" ] && [ -f "$env_file" ] && INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' "$env_file" | head -1 | tr -d '"')"
done
INTERNAL_TOKEN="${INTERNAL_TOKEN:-dev-internal-service-token-change-me}"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
DRIVER_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  sql ride-staff-postgres "delete from staff_members where id in ('$OWNER_STAFF_ID', '$OPERATOR_STAFF_ID');" > /dev/null 2>&1 || true
  local owners="'${RIDER_ID:-$UNKNOWN}', '${DRIVER_ID:-$UNKNOWN}'"
  # The operations point at the ledger rows: they go first.
  sql ride-wallet-postgres "
    delete from wallet_adjustments where owner_id in ($owners) or driver_id in ($owners);
    delete from payout_requests where driver_id in ($owners);
    delete from trip_settlements where trip_id = '$TRIP';
    delete from wallet_transactions where wallet_id in (select id from wallets where owner_id in ($owners));
    delete from wallets where owner_id in ($owners);" > /dev/null 2>&1 || true
  sql ride-driver-postgres "delete from drivers where vehicle_plate_number = '$PLATE';" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID" ]; then
    sql ride-rider-postgres "delete from riders where id = '$RIDER_ID';" > /dev/null 2>&1 || true
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

internal_call() { # <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$2" "$WALLET_ADDR" "ride.wallet.v1.WalletService/$1" > /dev/null
}

top_up() { # <owner type> <owner id> <amount> <key>
  internal_call TopUp "{\"owner_type\":\"$1\",\"owner_id\":\"$2\",\"amount\":\"$3\",\"idempotency_key\":\"$4\"}"
}

add_staff() { # <staff id> <identity> <role>
  sql ride-staff-postgres "
    insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
    values ('$1', '$2', 'e2e-ops-$1@ride.test', 'E2E Operations', 'active', now(), now());
    insert into staff_member_roles (staff_id, role_id) values ('$1', '$3');" > /dev/null
}

balance_of() { # <owner type> <owner id>: read by the owner staff member
  http GET "/v1/admin/wallets/$2?owner_type=$1" "$OWNER" > /dev/null
  body_field 'd["wallet"]["balance"]'
}

adjust() { # <owner type> <amount> <reason> <key>
  echo "{\"ownerType\":\"$1\",\"amount\":\"$2\",\"reason\":\"$3\",\"idempotencyKey\":\"$4\"}"
}

refund() { # <amount> <driver amount> <key>
  echo "{\"amount\":\"$1\",\"driverAmount\":\"$2\",\"reason\":\"the driver took a longer route\",\"idempotencyKey\":\"$3\"}"
}

payout() { # <amount> <key>
  echo "{\"amount\":\"$1\",\"destination\":\"ZainCash +9647500000009\",\"idempotencyKey\":\"$2\"}"
}

[ "$(sql ride-wallet-postgres "select to_regclass('public.payout_requests') is not null")" = t ] \
  || { echo "ABORT: the payout tables do not exist: apply wallet-service migration 00015 (goose up)" >&2; exit 2; }

buf build -o "$PROTOSET"
RIDER="$(mint "$RIDER_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"
OPERATOR="$(mint "$OPERATOR_IDENTITY")"

echo "==> [0/4] a rider with 10000, a driver with 40000, an owner and an operator"
add_staff "$OWNER_STAFF_ID" "$OWNER_IDENTITY" "$OWNER_ROLE"
add_staff "$OPERATOR_STAFF_ID" "$OPERATOR_IDENTITY" "$OPERATIONS_ROLE"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Ops Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "a driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Ops Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
sql ride-driver-postgres "update drivers set status = 'active' where id = '$DRIVER_ID';" > /dev/null
top_up OWNER_TYPE_RIDER "$RIDER_ID" 10000 "e2e-ops-rider-$RUN"
top_up OWNER_TYPE_DRIVER "$DRIVER_ID" 40000 "e2e-ops-driver-$RUN"

echo "==> [1/4] looking at a wallet and adjusting it"
RIDER_WALLET="/v1/admin/wallets/$RIDER_ID"
expect "the rider looks at their own through the admin route" 403 GET "$RIDER_WALLET?owner_type=OWNER_TYPE_RIDER" "$RIDER"
expect "the operations role" 403 GET "$RIDER_WALLET?owner_type=OWNER_TYPE_RIDER" "$OPERATOR"
expect "the owner" 200 GET "$RIDER_WALLET?owner_type=OWNER_TYPE_RIDER" "$OWNER"
check "10000, one row, nothing owed" "10000 1 0" "$(body_field '" ".join((d["wallet"]["balance"], str(len(d["recentTransactions"])), d["outstandingDues"]))')"
expect "an adjustment by the operations role" 403 POST "$RIDER_WALLET/adjustments" "$OPERATOR" "$(adjust OWNER_TYPE_RIDER -500 "duplicate top-up" "a1-$RUN")"
expect "an adjustment without a reason" 400 POST "$RIDER_WALLET/adjustments" "$OWNER" "$(adjust OWNER_TYPE_RIDER -500 "" "a0-$RUN")"
expect "taking 500 back from the rider" 200 POST "$RIDER_WALLET/adjustments" "$OWNER" "$(adjust OWNER_TYPE_RIDER -500 "duplicate top-up" "a1-$RUN")"
ADJUSTMENT="$(body_field 'd["adjustment"]["id"]')"
check "9500, an adjustment" "9500 adjustment -500" "$(body_field '" ".join((d["wallet"]["balance"], d["adjustment"]["kind"], d["adjustment"]["amount"]))')"
expect "the same key again" 200 POST "$RIDER_WALLET/adjustments" "$OWNER" "$(adjust OWNER_TYPE_RIDER -500 "duplicate top-up" "a1-$RUN")"
check "is the same adjustment, nothing more moved" "$ADJUSTMENT 9500" "$(body_field '" ".join((d["adjustment"]["id"], d["wallet"]["balance"]))')"
expect "the same key for another amount" 409 POST "$RIDER_WALLET/adjustments" "$OWNER" "$(adjust OWNER_TYPE_RIDER -600 "duplicate top-up" "a1-$RUN")"
expect "taking more than the rider holds" 400 POST "$RIDER_WALLET/adjustments" "$OWNER" "$(adjust OWNER_TYPE_RIDER -20000 "too much" "a2-$RUN")"
expect "the rider's ledger" 200 GET "/v1/wallets/$RIDER_ID/transactions?owner_type=OWNER_TYPE_RIDER&limit=1" "$RIDER"
check "shows the reason" "TRANSACTION_TYPE_ADJUSTMENT Adjustment: duplicate top-up" "$(body_field '" ".join((d["transactions"][0]["type"], d["transactions"][0]["description"]))')"
expect "taking 1000 from the driver" 200 POST "/v1/admin/wallets/$DRIVER_ID/adjustments" "$OWNER" "$(adjust OWNER_TYPE_DRIVER -1000 "commission owed from last week" "a3-$RUN")"
check "the driver holds 39000" 39000 "$(body_field 'd["wallet"]["balance"]')"

echo "==> [2/4] refunding a trip"
internal_call SettleTrip "{\"trip_id\":\"$TRIP\",\"rider_id\":\"$RIDER_ID\",\"driver_id\":\"$DRIVER_ID\",\"fare_amount\":\"5000\",\"payment_method\":\"PAYMENT_METHOD_WALLET\"}"
check "the rider paid 5000 from the wallet" 4500 "$(balance_of OWNER_TYPE_RIDER "$RIDER_ID")"
DRIVER_AFTER_TRIP="$(balance_of OWNER_TYPE_DRIVER "$DRIVER_ID")"
REFUNDS="/v1/admin/trips/$TRIP/refunds"
expect "a trip not settled" 404 POST "/v1/admin/trips/$UNSETTLED/refunds" "$OWNER" "$(refund 1000 0 "r0-$RUN")"
expect "by the operations role" 403 POST "$REFUNDS" "$OPERATOR" "$(refund 3000 1000 "r1-$RUN")"
expect "the driver giving back more than the refund" 400 POST "$REFUNDS" "$OWNER" "$(refund 1000 2000 "r1x-$RUN")"
expect "3000, 1000 of it from the driver" 200 POST "$REFUNDS" "$OWNER" "$(refund 3000 1000 "r1-$RUN")"
check "the rider holds 7500" "7500 refund" "$(body_field '" ".join((d["wallet"]["balance"], d["adjustment"]["kind"]))')"
check "the driver gave 1000 back" "$(python3 -c "print(int(float('$DRIVER_AFTER_TRIP')) - 1000)")" "$(balance_of OWNER_TYPE_DRIVER "$DRIVER_ID" | cut -d. -f1)"
expect "2001 more (past the fare)" 400 POST "$REFUNDS" "$OWNER" "$(refund 2001 0 "r2-$RUN")"
expect "the trip's refunds" 200 GET "$REFUNDS" "$OWNER"
check "one of 3000, of a 5000 fare" "1 5000 3000" "$(body_field '" ".join((str(len(d["refunds"])), d["paidAmount"], d["refundedAmount"]))')"

echo "==> [3/4] payouts"
PAYOUTS="/v1/drivers/$DRIVER_ID/payouts"
DRIVER_BEFORE="$(balance_of OWNER_TYPE_DRIVER "$DRIVER_ID" | cut -d. -f1)"
expect "below the minimum" 400 POST "$PAYOUTS" "$DRIVER" "$(payout 100 "p0-$RUN")"
expect "by a rider" 403 POST "$PAYOUTS" "$RIDER" "$(payout 20000 "p1-$RUN")"
expect "the driver asks for 20000" 200 POST "$PAYOUTS" "$DRIVER" "$(payout 20000 "p1-$RUN")"
PAYOUT="$(body_field 'd["payout"]["id"]')"
check "pending, held at once" "pending TRANSACTION_TYPE_PAYOUT $((DRIVER_BEFORE - 20000))" "$(body_field '" ".join((d["payout"]["status"], d["transaction"]["type"], d["wallet"]["balance"].split(".")[0]))')"
expect "the same key again" 200 POST "$PAYOUTS" "$DRIVER" "$(payout 20000 "p1-$RUN")"
check "is the same request, nothing more held" "$PAYOUT $((DRIVER_BEFORE - 20000))" "$(body_field '" ".join((d["payout"]["id"], d["wallet"]["balance"].split(".")[0]))')"
expect "a second one while it is open" 400 POST "$PAYOUTS" "$DRIVER" "$(payout 10000 "p2-$RUN")"
expect "the rider reads the driver's payouts" 403 GET "$PAYOUTS" "$RIDER"
expect "the operations role reads the queue" 403 GET "/v1/admin/payouts?status=pending" "$OPERATOR"
expect "the owner reads the queue" 200 GET "/v1/admin/payouts?status=pending&page_size=100" "$OWNER"
check "it is there" True "$(body_field "any(p['id'] == '$PAYOUT' for p in d.get('payouts', []))")"
expect "approving it" 200 POST "/v1/admin/payouts/$PAYOUT:approve" "$OWNER" '{}'
check "approved" approved "$(body_field 'd["payout"]["status"]')"
expect "paid without a reference" 400 POST "/v1/admin/payouts/$PAYOUT:markPaid" "$OWNER" '{"reference":""}'
expect "paid" 200 POST "/v1/admin/payouts/$PAYOUT:markPaid" "$OWNER" "{\"reference\":\"ZC-$RUN\"}"
check "paid, with the reference" "paid ZC-$RUN" "$(body_field '" ".join((d["payout"]["status"], d["payout"]["paidReference"]))')"
expect "rejecting a paid one" 400 POST "/v1/admin/payouts/$PAYOUT:reject" "$OWNER" '{"reason":"too late"}'
expect "another, for 10000" 200 POST "$PAYOUTS" "$DRIVER" "$(payout 10000 "p3-$RUN")"
SECOND="$(body_field 'd["payout"]["id"]')"
expect "rejecting it" 200 POST "/v1/admin/payouts/$SECOND:reject" "$OWNER" '{"reason":"this ZainCash number does not exist"}'
check "rejected" rejected "$(body_field 'd["payout"]["status"]')"
check "the 10000 came back" "$((DRIVER_BEFORE - 20000))" "$(balance_of OWNER_TYPE_DRIVER "$DRIVER_ID" | cut -d. -f1)"
expect "the driver's payouts" 200 GET "$PAYOUTS" "$DRIVER"
check "rejected with the reason, then paid" "rejected:this ZainCash number does not exist paid:ZC-$RUN" "$(body_field '" ".join(p["status"] + ":" + (p["rejectReason"] or p["paidReference"]) for p in d["payouts"])')"

echo "==> [4/4] the statement, for staff"
expect "the driver's returned payouts" 200 GET "/v1/admin/wallets/$DRIVER_ID/statement?owner_type=OWNER_TYPE_DRIVER&types=TRANSACTION_TYPE_PAYOUT_RETURN" "$OWNER"
check "one row of 10000" "1 10000" "$(body_field '" ".join((str(len(d["entries"])), d["totalIn"]))')"
expect "by the operations role" 403 GET "/v1/admin/wallets/$DRIVER_ID/statement?owner_type=OWNER_TYPE_DRIVER" "$OPERATOR"
expect "the driver's own, as before" 200 GET "/v1/wallets/$DRIVER_ID/statement?owner_type=OWNER_TYPE_DRIVER" "$DRIVER"

echo
if [ "$FAILURES" -ne 0 ]; then
  echo "FAIL: $FAILURES check(s) failed"
  exit 1
fi

echo "PASS: staff look at and adjust wallets and refund trips within the fare; payouts are held, paid or given back"
