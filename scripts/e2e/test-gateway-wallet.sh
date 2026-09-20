#!/usr/bin/env bash
# End-to-end test of the WALLET routes through the gateway, with REAL user tokens.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-gateway-wallet.sh
#
# What it proves:
#   1. a user reads their own wallet, its transactions and (drivers) their standing
#      over HTTP, and gets 403 for anyone else's
#   2. a payout or a ZainCash top-up can be requested only by the driver themselves
#      (it is tried with an invalid amount, so no money ever moves)
#   3. top-ups of arbitrary wallets and trip settlement have no route
#   4. the ZainCash webhook is reachable without a user token, and the gateway does
#      not mistake a provider header for a refused user credential
#
# Tokens are minted for local development only (scripts/tools/devtoken) and are
# never printed. Nothing is changed in any wallet.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"

DRIVER_A="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
DRIVER_B="4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
RIDER_IDENTITY_1="a0000000-0000-4000-8000-0000000000a1"
RIDER_IDENTITY_2="a0000000-0000-4000-8000-0000000000a2"

compose_port() { # <compose service name>
  awk -v svc="$1" '
    $0 ~ ("^  " svc ":$") {inside=1; next}
    inside && /^  [A-Za-z0-9_-]+:$/ {exit}
    inside && /GRPC_ADDRESS/ {gsub(/[^0-9]/, ""); print; exit}
  ' infrastructure/compose/compose.yaml
}

RIDER_ADDR="localhost:$(compose_port rider-service)"
DRIVER_ADDR="localhost:$(compose_port driver-service)"
RIDER_SVC="ride.rider.v1.RiderService"
DRIVER_SVC="ride.driver.v1.DriverService"

FAILURES=0
BODY_FILE="$(mktemp)"
trap 'rm -f "$BODY_FILE"' EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1" "${@:2}"; }

internal_call() { # <address> <service/method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$3" "$1" "$2"
}

json_field() { # <python expression over d> (reads stdin)
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

body_field() { json_field "$1" < "$BODY_FILE"; }

# http <method> <path> <token> [json body] [extra header] -> the HTTP status; the body goes to $BODY_FILE
http() {
  local method="$1" path="$2" token="$3" body="${4:-}" header="${5:-}"
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$method" "$BASE$path")

  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$header" ] && args+=(-H "$header")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")

  curl "${args[@]}"
}

pass() { printf '  ok    %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

expect() { # <label> <expected status> <method> <path> <token> [json body]
  local actual
  actual="$(http "$3" "$4" "$5" "${6:-}")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s: %s\n' "$1" "$2" "$actual" "$(head -c 200 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

# Past authentication and authorization: what the business logic answered (an invalid
# amount, a wallet that does not exist yet...) is not this test's concern.
expect_authorized() { # <label> <method> <path> <token> [json body]
  local actual
  actual="$(http "$2" "$3" "$4" "${5:-}")"

  case "$actual" in
    401 | 403 | 405 | 501)
      printf '  FAIL  %s -> expected to be authorized, got %s: %s\n' "$1" "$actual" "$(head -c 200 "$BODY_FILE")"
      FAILURES=$((FAILURES + 1))
      ;;
    *) printf '  ok    %s -> authorized (%s)\n' "$1" "$actual" ;;
  esac
}

expect_no_route() { # <label> <method> <path> <token> [json body]
  local actual
  actual="$(http "$2" "$3" "$4" "${5:-}")"

  case "$actual" in
    404 | 405 | 501) printf '  ok    %s -> no route (%s)\n' "$1" "$actual" ;;
    *)
      printf '  FAIL  %s -> expected no route (404/405/501), got %s: %s\n' "$1" "$actual" "$(head -c 200 "$BODY_FILE")"
      FAILURES=$((FAILURES + 1))
      ;;
  esac
}

rider_id_for_identity() { # <identity>
  internal_call "$RIDER_ADDR" "$RIDER_SVC/GetRiderByIdentity" "{\"identity_id\":\"$1\"}" 2>/dev/null \
    | json_field 'd["rider"]["id"]' 2>/dev/null || true
}

ensure_rider() { # <identity> <name>
  local id
  id="$(rider_id_for_identity "$1")"

  if [ -z "$id" ]; then
    internal_call "$RIDER_ADDR" "$RIDER_SVC/CreateRider" "{\"identity_id\":\"$1\",\"display_name\":\"$2\"}" > /dev/null
    id="$(rider_id_for_identity "$1")"
  fi

  [ -n "$id" ] || { echo "FAIL: could not find or create rider $2" >&2; exit 1; }
  echo "$id"
}

driver_identity() { # <driver id>
  internal_call "$DRIVER_ADDR" "$DRIVER_SVC/GetDriver" "{\"driver_id\":\"$1\"}" | json_field 'd["driver"]["identityId"]'
}

echo "==> [1/6] preparing: protoset, riders, drivers, tokens"
curl -sS -o /dev/null --max-time 5 "$BASE/v1/zones" -H "Authorization: Bearer x" \
  || { echo "FAIL: the gateway is not answering on $BASE" >&2; exit 1; }

buf build -o "$PROTOSET"

RIDER_1="$(ensure_rider "$RIDER_IDENTITY_1" "Authz Test Rider")"
ensure_rider "$RIDER_IDENTITY_2" "Authz Test Rider Two" > /dev/null

IDENTITY_DRIVER_A="$(driver_identity "$DRIVER_A")"
IDENTITY_DRIVER_B="$(driver_identity "$DRIVER_B")"
[ -n "$IDENTITY_DRIVER_A" ] && [ -n "$IDENTITY_DRIVER_B" ] || { echo "FAIL: the test drivers have no identity" >&2; exit 1; }

T1="$(mint "$RIDER_IDENTITY_1")"
T2="$(mint "$RIDER_IDENTITY_2")"
TA="$(mint "$IDENTITY_DRIVER_A")"
TB="$(mint "$IDENTITY_DRIVER_B")"

DW="owner_type=OWNER_TYPE_DRIVER"
RW="owner_type=OWNER_TYPE_RIDER"

echo "==> [2/6] a driver's wallet"
expect "driver A reads their wallet"                     200 GET "/v1/wallets/$DRIVER_A?$DW" "$TA"
if [ "$(body_field 'd["wallet"]["ownerId"]')" = "$DRIVER_A" ]; then pass "the wallet is driver A's"; else fail "the wallet is not driver A's"; fi
expect "driver B reads driver A's wallet"                403 GET "/v1/wallets/$DRIVER_A?$DW" "$TB"
expect "rider 1 reads driver A's wallet"                 403 GET "/v1/wallets/$DRIVER_A?$DW" "$T1"
expect "nobody reads driver A's wallet"                  401 GET "/v1/wallets/$DRIVER_A?$DW" ""
expect "driver A reads their wallet as a rider's"        403 GET "/v1/wallets/$DRIVER_A?$RW" "$TA"
expect "driver A lists their transactions"               200 GET "/v1/wallets/$DRIVER_A/transactions?$DW&limit=5" "$TA"
expect "driver B lists driver A's transactions"          403 GET "/v1/wallets/$DRIVER_A/transactions?$DW&limit=5" "$TB"
expect "rider 1 lists driver A's transactions"           403 GET "/v1/wallets/$DRIVER_A/transactions?$DW&limit=5" "$T1"

echo "==> [3/6] a rider's wallet"
# A rider's wallet may not exist yet: 404 from the wallet service is fine, 404 from the router is not.
STATUS="$(http GET "/v1/wallets/$RIDER_1?$RW" "$T1")"
if [ "$STATUS" = 200 ] || { [ "$STATUS" = 404 ] && grep -qi 'wallet' "$BODY_FILE"; }; then
  pass "rider 1 reads their wallet -> $STATUS"
else
  fail "rider 1 reading their wallet -> $STATUS: $(head -c 200 "$BODY_FILE")"
fi
expect "rider 2 reads rider 1's wallet"                  403 GET "/v1/wallets/$RIDER_1?$RW" "$T2"
expect "driver A reads rider 1's wallet"                 403 GET "/v1/wallets/$RIDER_1?$RW" "$TA"

echo "==> [4/6] a driver's standing"
expect "driver A checks their standing"                  200 GET "/v1/drivers/$DRIVER_A/standing" "$TA"
if [ "$(body_field '"canTakeTrips" in d')" = "True" ]; then pass "the answer says whether they can take trips"; else fail "the answer has no canTakeTrips"; fi
expect "driver B checks driver A's standing"             403 GET "/v1/drivers/$DRIVER_A/standing" "$TB"
expect "rider 1 checks driver A's standing"              403 GET "/v1/drivers/$DRIVER_A/standing" "$T1"

echo "==> [5/6] payouts and top-ups (invalid amounts: no money moves)"
PAYOUT='{"amount":"not-a-number","idempotencyKey":"authz-http-payout-1"}'
expect_authorized "driver A requests a payout"           POST "/v1/drivers/$DRIVER_A/payouts" "$TA" "$PAYOUT"
expect "driver B requests a payout from driver A's wallet" 403 POST "/v1/drivers/$DRIVER_A/payouts" "$TB" "$PAYOUT"
expect "rider 1 requests a payout from driver A's wallet"  403 POST "/v1/drivers/$DRIVER_A/payouts" "$T1" "$PAYOUT"
expect "nobody requests a payout"                        401 POST "/v1/drivers/$DRIVER_A/payouts" "" "$PAYOUT"
expect_authorized "driver A starts a ZainCash top-up"    POST "/v1/wallet/topups/zaincash" "$TA" "{\"driverId\":\"$DRIVER_A\",\"amount\":\"not-a-number\"}"
expect "driver B starts a top-up for driver A"           403 POST "/v1/wallet/topups/zaincash" "$TB" "{\"driverId\":\"$DRIVER_A\",\"amount\":\"not-a-number\"}"
expect_no_route "topping up a wallet directly has no route"   POST "/v1/wallets/$DRIVER_A/topups" "$TA" '{"amount":"1000"}'
expect_no_route "settling a trip has no route"                POST "/v1/trips/$DRIVER_A:settle" "$TA" '{}'

echo "==> [6/6] the ZainCash webhook is called by ZainCash, not by a user"
# It carries no user token; here it carries a bogus token, so wallet-service must reject it.
# What matters: it reaches wallet-service (a route), and the gateway did not refuse it as a
# bad user credential, even with a provider-style Authorization header.
GATEWAY_REFUSAL="a user access token is required"

STATUS="$(http POST "/v1/wallet/zaincash/webhook" "" '{"token":"not-a-real-jwt"}')"
if [ "$STATUS" != 404 ] && [ "$STATUS" != 405 ] && [ "$STATUS" != 501 ] && ! grep -q "$GATEWAY_REFUSAL" "$BODY_FILE"; then
  pass "the webhook reaches wallet-service without a user token (-> $STATUS, a bogus token is rejected there)"
else
  fail "the webhook did not reach wallet-service (-> $STATUS): $(head -c 200 "$BODY_FILE")"
fi

STATUS="$(http POST "/v1/wallet/zaincash/webhook" "" '{"token":"not-a-real-jwt"}' 'Authorization: Bearer provider-shared-secret')"
if ! grep -q "$GATEWAY_REFUSAL" "$BODY_FILE"; then
  pass "a provider-style Authorization header is not refused by the gateway (-> $STATUS)"
else
  fail "the gateway refused the webhook's Authorization header as a bad user credential"
fi

STATUS="$(http POST "/v1/wallet/topups/zaincash" "" '{}' 'Authorization: Bearer provider-shared-secret')"
if [ "$STATUS" = 401 ] && grep -q "$GATEWAY_REFUSAL" "$BODY_FILE"; then
  pass "the same header on any other route is still refused at the gateway"
else
  fail "a non-JWT Authorization header was not refused on another route (-> $STATUS)"
fi

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: users reach only their own wallet, standing and payouts over HTTP, top-ups and settlement have no route, and the webhook still works"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
