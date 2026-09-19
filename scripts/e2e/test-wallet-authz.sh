#!/usr/bin/env bash
# End-to-end wallet authorization test with REAL user tokens.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-wallet-authz.sh
#
# Tokens are minted for local development only (scripts/tools/devtoken) and
# are never printed. Needs the stack running with the wallet ownership patch.
#
# What it proves:
#   1. a user reaches only wallets they own
#   2. no user can call the money-moving RPCs (TopUp, SettleTrip), and a
#      minting attempt leaves the balance untouched
#   3. bad, expired and profile-less identities are refused
#   4. the internal token still works (services keep talking to each other)
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"
WALLET="localhost:50058"
SVC="ride.wallet.v1.WalletService"

DRIVER_A="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
DRIVER_B="4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
TEST_RIDER_IDENTITY="a0000000-0000-4000-8000-0000000000a1"
RIDER_PORT="$(awk '/^  rider-service:$/ {inside=1; next} inside && /^  [A-Za-z0-9_-]+:$/ {exit} inside && /GRPC_ADDRESS/ {gsub(/[^0-9]/, ""); print; exit}' infrastructure/compose/compose.yaml)"
[ -n "$RIDER_PORT" ] || { echo "FAIL: could not read rider-service GRPC_ADDRESS from infrastructure/compose/compose.yaml" >&2; exit 1; }
RIDER_ADDR="localhost:$RIDER_PORT"

FAILURES=0

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

mint() { # <identity> [extra devtoken flags...]
  local identity="$1"; shift
  go run scripts/tools/devtoken/main.go -sub "$identity" "$@"
}

internal_call() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$3" "$1" "$2"
}

rider_id_for_identity() { # <identity>
  internal_call "$RIDER_ADDR" ride.rider.v1.RiderService/GetRiderByIdentity "{\"identity_id\":\"$1\"}" 2>/dev/null \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["rider"]["id"])' 2>/dev/null || true
}

code_of() { # <token> <method> <json>  -> OK or the gRPC code name
  local output
  output="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$3" "$WALLET" "$SVC/$2" 2>&1 || true)"

  if grep -q '^ERROR:' <<<"$output"; then
    awk '/Code:/ {print $2; exit}' <<<"$output"
  else
    echo OK
  fi
}

expect() { # <label> <expected> <token> <method> <json>
  local actual
  actual="$(code_of "$3" "$4" "$5")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %-62s %s\n' "$1" "$actual"
  else
    printf '  FAIL  %-62s expected %s, got %s\n' "$1" "$2" "$actual"
    FAILURES=$((FAILURES + 1))
  fi
}

expect_authorized() { # <label> <token> <method> <json>  (any answer except a permission/auth refusal)
  local actual
  actual="$(code_of "$2" "$3" "$4")"

  case "$actual" in
    PermissionDenied|Unauthenticated|Unavailable)
      printf '  FAIL  %-62s refused with %s\n' "$1" "$actual"
      FAILURES=$((FAILURES + 1))
      ;;
    *)
      printf '  ok    %-62s reached the handler (%s)\n' "$1" "$actual"
      ;;
  esac
}

echo "==> [1/5] preparing: protoset, identities and tokens"
buf build -o "$PROTOSET"

IDENTITY_DRIVER_A="$(sql ride-driver-postgres "select identity_id from drivers where id='$DRIVER_A';")"
IDENTITY_DRIVER_B="$(sql ride-driver-postgres "select identity_id from drivers where id='$DRIVER_B';")"

[ -n "$IDENTITY_DRIVER_A" ] || { echo "FAIL: driver A has no identity" >&2; exit 1; }
[ -n "$IDENTITY_DRIVER_B" ] || { echo "FAIL: driver B has no identity" >&2; exit 1; }

RIDER_A="$(rider_id_for_identity "$TEST_RIDER_IDENTITY")"
if [ -z "$RIDER_A" ]; then
  internal_call "$RIDER_ADDR" ride.rider.v1.RiderService/CreateRider \
    "{\"identity_id\":\"$TEST_RIDER_IDENTITY\",\"display_name\":\"Authz Test Rider\"}" > /dev/null
  RIDER_A="$(rider_id_for_identity "$TEST_RIDER_IDENTITY")"
fi
[ -n "$RIDER_A" ] || { echo "FAIL: could not find or create the test rider in rider-service ($RIDER_ADDR)" >&2; exit 1; }
IDENTITY_RIDER_A="$TEST_RIDER_IDENTITY"

# the rider needs a wallet for the own-wallet checks; a repeated top-up is refused as a duplicate, which is fine
internal_call localhost:50058 ride.wallet.v1.WalletService/TopUp \
  "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER_A\",\"amount\":\"1000\",\"idempotency_key\":\"authz-test-rider-wallet\"}" > /dev/null 2>&1 || true

TOKEN_DRIVER_A="$(mint "$IDENTITY_DRIVER_A")"
TOKEN_RIDER_A="$(mint "$IDENTITY_RIDER_A")"
TOKEN_NOBODY="$(mint "00000000-0000-4000-8000-000000000001")"
TOKEN_EXPIRED="$(mint "$IDENTITY_DRIVER_A" -ttl=-1h)"

BALANCE_BEFORE="$(sql ride-wallet-postgres "select coalesce(sum(balance),0) from wallets where owner_id='$DRIVER_A';")"

echo "==> [2/5] a driver reaches only their own wallet"
expect "driver A reads own wallet"                  OK               "$TOKEN_DRIVER_A" GetWallet "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRIVER_A\"}"
expect "driver A reads driver B's wallet"           PermissionDenied "$TOKEN_DRIVER_A" GetWallet "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRIVER_B\"}"
expect "driver A reads a rider's wallet"            PermissionDenied "$TOKEN_DRIVER_A" GetWallet "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER_A\"}"
expect "driver A lists own transactions"            OK               "$TOKEN_DRIVER_A" ListTransactions "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRIVER_A\",\"limit\":5}"
expect "driver A lists driver B's transactions"     PermissionDenied "$TOKEN_DRIVER_A" ListTransactions "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRIVER_B\",\"limit\":5}"
expect "driver A checks own standing"               OK               "$TOKEN_DRIVER_A" CheckDriverStanding "{\"driver_id\":\"$DRIVER_A\"}"
expect "driver A checks driver B's standing"        PermissionDenied "$TOKEN_DRIVER_A" CheckDriverStanding "{\"driver_id\":\"$DRIVER_B\"}"
expect "driver A requests a payout for driver B"    PermissionDenied "$TOKEN_DRIVER_A" RequestPayout "{\"driver_id\":\"$DRIVER_B\",\"amount\":\"1\",\"idempotency_key\":\"authz-test-b\"}"
expect "driver A starts a top-up for driver B"      PermissionDenied "$TOKEN_DRIVER_A" InitiateTopUp "{\"driver_id\":\"$DRIVER_B\",\"amount\":\"1000\"}"
expect_authorized "driver A's own payout request is authorized" "$TOKEN_DRIVER_A" RequestPayout "{\"driver_id\":\"$DRIVER_A\",\"amount\":\"1\",\"idempotency_key\":\"authz-test-a\"}"

echo "==> [3/5] a rider reaches only their own wallet"
expect "rider reads own wallet"                     OK               "$TOKEN_RIDER_A" GetWallet "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER_A\"}"
expect "rider reads a driver's wallet"              PermissionDenied "$TOKEN_RIDER_A" GetWallet "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRIVER_A\"}"
expect "rider passes a driver id as a rider wallet" PermissionDenied "$TOKEN_RIDER_A" GetWallet "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$DRIVER_A\"}"
expect "rider requests a payout"                    PermissionDenied "$TOKEN_RIDER_A" RequestPayout "{\"driver_id\":\"$DRIVER_A\",\"amount\":\"1\",\"idempotency_key\":\"authz-test-r\"}"

echo "==> [4/5] nobody can move money through the API"
expect "driver A tops up own wallet (mint attempt)" PermissionDenied "$TOKEN_DRIVER_A" TopUp "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRIVER_A\",\"amount\":\"1000000\",\"idempotency_key\":\"authz-mint-a\"}"
expect "rider tops up own wallet (mint attempt)"    PermissionDenied "$TOKEN_RIDER_A"  TopUp "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER_A\",\"amount\":\"1000000\",\"idempotency_key\":\"authz-mint-r\"}"
expect "driver A settles a trip"                    PermissionDenied "$TOKEN_DRIVER_A" SettleTrip "{\"trip_id\":\"$DRIVER_A\",\"rider_id\":\"$RIDER_A\",\"driver_id\":\"$DRIVER_A\",\"fare_amount\":\"1000\",\"payment_method\":\"PAYMENT_METHOD_CASH\"}"

BALANCE_AFTER="$(sql ride-wallet-postgres "select coalesce(sum(balance),0) from wallets where owner_id='$DRIVER_A';")"
if [ "$(sql ride-wallet-postgres "select $BALANCE_BEFORE = $BALANCE_AFTER;")" = "t" ]; then
  printf '  ok    %-62s %s\n' "driver A's balance is unchanged" "$BALANCE_AFTER"
else
  printf '  FAIL  %-62s %s -> %s\n' "driver A's balance is unchanged" "$BALANCE_BEFORE" "$BALANCE_AFTER"
  FAILURES=$((FAILURES + 1))
fi

echo "==> [5/5] bad credentials, and the internal path"
expect "an identity with no profile"                PermissionDenied "$TOKEN_NOBODY"   GetWallet "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER_A\"}"
expect "an expired token"                           Unauthenticated  "$TOKEN_EXPIRED"  GetWallet "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRIVER_A\"}"
expect "a malformed token"                          Unauthenticated  "not-a-token"     GetWallet "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRIVER_A\"}"
expect_authorized "the internal token still reaches any wallet" "$INTERNAL_TOKEN" GetWallet "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRIVER_B\"}"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: users reach only their own wallets, cannot move money, and the internal path is intact"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
