#!/usr/bin/env bash
# End-to-end test of wallet vouchers on the real services, through the
# gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-wallet-vouchers.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken), and wallet-service with migration 00014 and
# staff-service knowing vouchers.manage.
#
# What it proves:
#   1. vouchers are staff business: vouchers.manage (the owner, not the
#      operations role, not a rider) issues a batch; bad batches are refused;
#      the same key is the same batch
#   2. nothing is redeemable before the export; the export gives every code
#      (and a CSV) once, and never again
#   3. a rider redeems a code however they type it; the wallet and the ledger
#      show it; the same rider again gets the same redemption, another rider
#      is told it is used; staff look the voucher up by its serial
#   4. a voided voucher and a cancelled batch are not redeemable; the batch
#      counts what happened
#   5. wrong codes are counted: after five the rider waits (429)
#
# It creates two riders and two staff members and removes them again, with
# every batch the owner issued.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
SECOND_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OPERATOR_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
OPERATOR_STAFF_ID="$(uuid)"
UNKNOWN="$(uuid)"
IN_A_DAY="$(python3 -c 'import datetime as t; print((t.datetime.now(t.timezone.utc) + t.timedelta(days=1)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
IN_A_MINUTE="$(python3 -c 'import datetime as t; print((t.datetime.now(t.timezone.utc) + t.timedelta(minutes=1)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
SECOND_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  sql ride-staff-postgres "delete from staff_members where id in ('$OWNER_STAFF_ID', '$OPERATOR_STAFF_ID');" > /dev/null 2>&1 || true
  local riders="'${RIDER_ID:-$UNKNOWN}', '${SECOND_ID:-$UNKNOWN}'"
  # The vouchers point at the ledger rows: they go first.
  sql ride-wallet-postgres "
    delete from vouchers where batch_id in (select id from voucher_batches where created_by = '$OWNER_IDENTITY');
    delete from voucher_batches where created_by = '$OWNER_IDENTITY';
    delete from voucher_redeem_failures where rider_id in ($riders);
    delete from wallet_transactions where wallet_id in (select id from wallets where owner_id in ($riders));
    delete from wallets where owner_id in ($riders);" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID$SECOND_ID" ]; then
    sql ride-rider-postgres "delete from riders where id in ($riders);" > /dev/null 2>&1 || true
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

add_staff() { # <staff id> <identity> <role>
  sql ride-staff-postgres "
    insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
    values ('$1', '$2', 'e2e-voucher-$1@ride.test', 'E2E Voucher', 'active', now(), now());
    insert into staff_member_roles (staff_id, role_id) values ('$1', '$3');" > /dev/null
}

batch() { # <label> <amount> <quantity> <key> [expires at]
  local expires=""
  [ -n "${5:-}" ] && expires=",\"expiresAt\":\"$5\""
  echo "{\"label\":\"$1\",\"seller\":\"zaincash\",\"amount\":\"$2\",\"quantity\":$3,\"idempotencyKey\":\"$4\"$expires}"
}

redeem() { # <code>
  echo "{\"code\":\"$1\"}"
}

balance_of() { # <rider id> <token>
  http GET "/v1/wallets/$1?owner_type=OWNER_TYPE_RIDER" "$2" > /dev/null
  body_field 'd["wallet"]["balance"]'
}

[ "$(sql ride-wallet-postgres "select to_regclass('public.voucher_batches') is not null")" = t ] \
  || { echo "ABORT: the voucher tables do not exist: apply wallet-service migration 00014 (goose up)" >&2; exit 2; }

RIDER="$(mint "$RIDER_IDENTITY")"
SECOND="$(mint "$SECOND_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"
OPERATOR="$(mint "$OPERATOR_IDENTITY")"

echo "==> [0/5] two riders, an owner and an operator"
add_staff "$OWNER_STAFF_ID" "$OWNER_IDENTITY" "$OWNER_ROLE"
add_staff "$OPERATOR_STAFF_ID" "$OPERATOR_IDENTITY" "$OPERATIONS_ROLE"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Voucher Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "a second rider" 200 POST /v1/riders "$SECOND" "{\"identityId\":\"$SECOND_IDENTITY\",\"displayName\":\"E2E Voucher Second\"}"
SECOND_ID="$(body_field 'd["rider"]["id"]')"

echo "==> [1/5] issuing a batch"
BATCHES=/v1/admin/voucher-batches
expect "without a token" 401 POST "$BATCHES" "" "$(batch "E2E $RUN" 5000 3 "b-$RUN")"
expect "by a rider" 403 POST "$BATCHES" "$RIDER" "$(batch "E2E $RUN" 5000 3 "b-$RUN")"
expect "by the operations role" 403 POST "$BATCHES" "$OPERATOR" "$(batch "E2E $RUN" 5000 3 "b-$RUN")"
expect "no vouchers" 400 POST "$BATCHES" "$OWNER" "$(batch "E2E $RUN" 5000 0 "b-x1-$RUN")"
expect "ten thousand and one" 400 POST "$BATCHES" "$OWNER" "$(batch "E2E $RUN" 5000 10001 "b-x2-$RUN")"
expect "a zero amount" 400 POST "$BATCHES" "$OWNER" "$(batch "E2E $RUN" 0 3 "b-x3-$RUN")"
expect "ending in a minute" 400 POST "$BATCHES" "$OWNER" "$(batch "E2E $RUN" 5000 3 "b-x4-$RUN" "$IN_A_MINUTE")"
expect "three of 5000 by the owner" 200 POST "$BATCHES" "$OWNER" "$(batch "E2E $RUN" 5000 3 "b-$RUN" "$IN_A_DAY")"
BATCH_ID="$(body_field 'd["batch"]["id"]')"
NUMBER="$(body_field 'd["batch"]["number"]')"
check "created, nothing sold yet" "created 3 5000 IQD" "$(body_field '" ".join((d["batch"]["status"], str(d["batch"]["quantity"]), d["batch"]["amount"], d["batch"]["currencyCode"]))')"
expect "the same key again" 200 POST "$BATCHES" "$OWNER" "$(batch "E2E $RUN" 5000 3 "b-$RUN" "$IN_A_DAY")"
check "is the same batch" "$BATCH_ID" "$(body_field 'd["batch"]["id"]')"
expect "the same key for another batch" 409 POST "$BATCHES" "$OWNER" "$(batch "E2E $RUN" 5000 4 "b-$RUN" "$IN_A_DAY")"
expect "the owner lists created batches" 200 GET "$BATCHES?status=created" "$OWNER"
check "the batch is there" True "$(body_field "any(b['id'] == '$BATCH_ID' for b in d.get('batches', []))")"
expect "a bad status filter" 400 GET "$BATCHES?status=sold" "$OWNER"

echo "==> [2/5] exporting it, once"
expect "by the operations role" 403 POST "$BATCHES/$BATCH_ID:export" "$OPERATOR" '{}'
expect "by the owner" 200 POST "$BATCHES/$BATCH_ID:export" "$OWNER" '{}'
check "exported, three codes, a CSV with a header" "exported 3 4" "$(body_field '" ".join((d["batch"]["status"], str(len(d["vouchers"])), str(len(d["csv"].strip().splitlines()))))')"
check "serials follow the batch number" "V$NUMBER-00001 V$NUMBER-00002 V$NUMBER-00003" "$(body_field '" ".join(v["serial"] for v in d["vouchers"])')"
check "codes are grouped in fours" True "$(body_field 'all(len(v["code"]) == 19 and v["code"].count("-") == 3 for v in d["vouchers"])')"
CODE1="$(body_field 'd["vouchers"][0]["code"]')"
CODE2="$(body_field 'd["vouchers"][1]["code"]')"
CODE3="$(body_field 'd["vouchers"][2]["code"]')"
SERIAL1="V$NUMBER-00001"
SERIAL2="V$NUMBER-00002"
expect "exporting again" 400 POST "$BATCHES/$BATCH_ID:export" "$OWNER" '{}'
check "is refused" True "$(message | grep -q 'shown once' && echo True || echo False)"
check "no code is kept in the database" 0 "$(sql ride-wallet-postgres "select count(*) from vouchers where batch_id = '$BATCH_ID' and code_sealed is not null")"

echo "==> [3/5] redeeming"
REDEEM="/v1/wallets/$RIDER_ID/vouchers:redeem"
TYPED="$(echo "$CODE1" | tr 'A-Z' 'a-z' | tr '-' ' ')"
expect "without a token" 401 POST "$REDEEM" "" "$(redeem "$CODE1")"
expect "into another rider's wallet" 403 POST "$REDEEM" "$SECOND" "$(redeem "$CODE1")"
expect "not in the form of a code" 400 POST "$REDEEM" "$RIDER" "$(redeem "hello")"
expect "typed in lower case with spaces" 200 POST "$REDEEM" "$RIDER" "$(redeem "$TYPED")"
check "5000, a voucher row, its serial" "$SERIAL1 5000 5000 TRANSACTION_TYPE_VOUCHER" "$(body_field '" ".join((d["serial"], d["amount"], d["wallet"]["balance"], d["transaction"]["type"]))')"
TRANSACTION="$(body_field 'd["transaction"]["id"]')"
expect "the same rider again" 200 POST "$REDEEM" "$RIDER" "$(redeem "$CODE1")"
check "is the same redemption" "$TRANSACTION" "$(body_field 'd["transaction"]["id"]')"
check "the rider holds 5000" 5000 "$(balance_of "$RIDER_ID" "$RIDER")"
expect "another rider" 400 POST "/v1/wallets/$SECOND_ID/vouchers:redeem" "$SECOND" "$(redeem "$CODE1")"
check "is told it is used" "this voucher code was already used" "$(message)"
expect "the statement's vouchers" 200 GET "/v1/wallets/$RIDER_ID/statement?owner_type=OWNER_TYPE_RIDER&types=TRANSACTION_TYPE_VOUCHER" "$RIDER"
check "one row of 5000" "1 5000" "$(body_field '" ".join((str(len(d["entries"])), d["totalIn"]))')"
expect "the owner looks it up by serial" 200 GET "/v1/admin/vouchers/$SERIAL1" "$OWNER"
check "redeemed by the rider, the ledger row" "redeemed $RIDER_ID $TRANSACTION False" "$(body_field '" ".join((d["voucher"]["status"], d["voucher"]["redeemedByRiderId"], d["voucher"]["transactionId"], str(d["voucher"]["redeemable"])))')"
expect "a rider looks it up" 403 GET "/v1/admin/vouchers/$SERIAL1" "$RIDER"

echo "==> [4/5] voiding and cancelling"
expect "voiding without a reason" 400 POST "/v1/admin/vouchers/$SERIAL2:void" "$OWNER" '{"reason":""}'
expect "voiding the second" 200 POST "/v1/admin/vouchers/$SERIAL2:void" "$OWNER" '{"reason":"card reported lost"}'
check "void, not redeemable" "void False" "$(body_field '" ".join((d["voucher"]["status"], str(d["voucher"]["redeemable"])))')"
expect "voiding a redeemed one" 400 POST "/v1/admin/vouchers/$SERIAL1:void" "$OWNER" '{"reason":"too late"}'
expect "the second rider redeems the void one" 400 POST "/v1/wallets/$SECOND_ID/vouchers:redeem" "$SECOND" "$(redeem "$CODE2")"
check "is told it was cancelled" "this voucher code was cancelled" "$(message)"
expect "cancelling the batch" 200 POST "$BATCHES/$BATCH_ID:cancel" "$OWNER" '{"reason":"end of the e2e test"}'
check "cancelled, one redeemed, one void" "cancelled 1 1 5000" "$(body_field '" ".join((d["batch"]["status"], str(d["batch"]["redeemedCount"]), str(d["batch"]["voidCount"]), d["batch"]["redeemedAmount"]))')"
expect "the second rider redeems the third" 400 POST "/v1/wallets/$SECOND_ID/vouchers:redeem" "$SECOND" "$(redeem "$CODE3")"
check "is told it was cancelled" "this voucher code was cancelled" "$(message)"
expect "cancelling again" 400 POST "$BATCHES/$BATCH_ID:cancel" "$OWNER" '{"reason":"twice"}'
check "the first rider still holds 5000" 5000 "$(balance_of "$RIDER_ID" "$RIDER")"

echo "==> [5/5] guessing codes"
# The second rider has three failed codes already (used, void, cancelled).
expect "a made-up code" 404 POST "/v1/wallets/$SECOND_ID/vouchers:redeem" "$SECOND" "$(redeem "ABCD-EFGH-JKMN-PQRS")"
expect "another made-up code" 404 POST "/v1/wallets/$SECOND_ID/vouchers:redeem" "$SECOND" "$(redeem "SRQP-NMKJ-HGFE-DCBA")"
expect "the sixth try" 429 POST "/v1/wallets/$SECOND_ID/vouchers:redeem" "$SECOND" "$(redeem "ZZZZ-ZZZZ-ZZZZ-ZZZZ")"
check "says to wait" True "$(message | grep -q 'try again after' && echo True || echo False)"
expect "the first rider is not held back" 404 POST "$REDEEM" "$RIDER" "$(redeem "ZZZZ-ZZZZ-ZZZZ-ZZZZ")"

echo
if [ "$FAILURES" -ne 0 ]; then
  echo "FAIL: $FAILURES check(s) failed"
  exit 1
fi

echo "PASS: staff issue vouchers and export them once; a rider redeems a code once; void, cancelled and guessed codes do not pay"
