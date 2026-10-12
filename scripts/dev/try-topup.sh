#!/usr/bin/env bash
# Tries a wallet top-up through ZainCash on a STAGING instance, end to end, without
# the Rider app: so the backend is known to work with ZainCash's test environment
# (pg-api-uat) before the app's screen is built. Run it on the server, from the repo
# root:
#   bash scripts/dev/try-topup.sh [amount]
# Example: bash scripts/dev/try-topup.sh 1000
#
# The first run makes a test rider: it signs in as SIM_RIDER_PHONE (+9647700000992
# by default) with the code identity-service writes to its log on staging, and
# signs up as a rider. instance/sim-rider.env keeps it for the next runs.
# Then it:
#   - reads the top-up options (limits, providers set up)
#   - opens a ZainCash payment and prints its page: open it, and pay with a test
#     wallet (9647802999569, PIN 1111, OTP 111111), or cancel there
#   - asks how the top-up stands every 5 seconds (wallet-service asks ZainCash:
#     Transaction Inquiry) until it succeeded or failed, then prints the balance
# It refuses to run unless instance/instance.env says IDENTITY_APP_ENV=test.
set -Euo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
[ "$(value IDENTITY_APP_ENV)" = test ] ||
  die "this instance is not staging (IDENTITY_APP_ENV=test); a test rider has no place on a real one"
command -v python3 > /dev/null || die "python3 is needed"

AMOUNT="${1:-1000}"; PHONE="${SIM_RIDER_PHONE:-+9647700000992}"
echo "$AMOUNT" | grep -Eq '^[1-9][0-9]*$' || die "the amount is a whole number of dinars, for example 1000"

API="http://127.0.0.1:$(value GATEWAY_PORT)"
[ "$API" != "http://127.0.0.1:" ] || API="http://127.0.0.1:18080"
STATE="instance/sim-rider.env"
TOKEN=""; REFRESH=""; IDENTITY=""; RIDER=""; STATUS=""; BODY=""

field() { # <path, e.g. wallet.balance>: from the JSON on stdin; empty when missing
  python3 -c '
import json, sys
try:
    value = json.load(sys.stdin)
    for key in sys.argv[1].split("."):
        value = value[key]
    print(json.dumps(value) if isinstance(value, (dict, list)) else value)
except Exception:
    print("")' "$1"
}

call() { # <method> <path> [json]: sets STATUS and BODY
  local args=(-s -X "$1" "$API$2" -H "Content-Type: application/json" -w $'\n%{http_code}')
  local out
  if [ -n "$TOKEN" ]; then args+=(-H "Authorization: Bearer $TOKEN"); fi
  if [ $# -ge 3 ]; then args+=(-d "$3"); fi
  out="$(curl "${args[@]}")" || out=$'\n000'
  STATUS="${out##*$'\n'}"
  BODY="${out%$'\n'*}"
}

save() { (umask 077 && printf 'IDENTITY=%s\nREFRESH=%s\nRIDER=%s\n' "$IDENTITY" "$REFRESH" "$RIDER" > "$STATE"); }

login() { # signs in as the test rider with the code from identity-service's log
  local challenge code="" try
  TOKEN=""
  call POST /v1/auth/otp:request "{\"identifier\":{\"type\":\"IDENTIFIER_TYPE_PHONE\",\"value\":\"$PHONE\"}}"
  [ "$STATUS" = 200 ] || die "could not ask for a login code ($STATUS): $BODY"
  challenge="$(echo "$BODY" | field challengeId)"
  for try in 1 2 3 4 5 6; do
    sleep 1
    code="$(docker logs --since 2m ride-identity-service 2>&1 | grep -F "${PHONE#+}" | grep otp_code | tail -1 |
      grep -oE 'otp_code[^0-9]{1,4}[0-9]{4,8}' | grep -oE '[0-9]{4,8}$' || true)"
    [ -n "$code" ] && break
  done
  [ -n "$code" ] || die "no login code for $PHONE in identity-service's log"
  call POST /v1/auth/otp:verify "{\"challengeId\":\"$challenge\",\"code\":\"$code\",\"clientId\":\"rider-app\",\"deviceId\":\"sim-rider\",\"deviceName\":\"sim-rider\",\"platform\":\"android\",\"appVersion\":\"1.0.0\"}"
  [ "$STATUS" = 200 ] || die "the login code was refused ($STATUS): $BODY"
  TOKEN="$(echo "$BODY" | field accessToken)"
  REFRESH="$(echo "$BODY" | field refreshToken)"
  IDENTITY="$(echo "$BODY" | field identityId)"
  echo "$IDENTITY" | grep -Eq '^[0-9a-f-]{36}$' || die "no identity id in: $BODY"
}

renew() { # a token from the saved refresh token, or signing in again
  if [ -f "$STATE" ]; then
    IDENTITY="$(sed -n 's/^IDENTITY=//p' "$STATE")"
    REFRESH="$(sed -n 's/^REFRESH=//p' "$STATE")"
    RIDER="$(sed -n 's/^RIDER=//p' "$STATE")"
  fi
  if [ -n "$REFRESH" ] && [ -n "$IDENTITY" ]; then
    call POST /v1/auth/token:refresh "{\"refreshToken\":\"$REFRESH\"}"
    if [ "$STATUS" = 200 ]; then
      TOKEN="$(echo "$BODY" | field accessToken)"
      REFRESH="$(echo "$BODY" | field refreshToken)"
      save
      return
    fi
  fi
  login
  save
}

api() { # like call, renewing the token once when it ran out
  call "$@"
  if [ "$STATUS" = 401 ]; then renew; call "$@"; fi
}

balance() { # the test rider's balance; 0 before the wallet exists
  api GET "/v1/wallets/$RIDER?owner_type=OWNER_TYPE_RIDER"
  local b
  b="$(echo "$BODY" | field wallet.balance)"
  echo "${b:-0}"
}

renew

# The test rider: signed up once.
if [ -z "$RIDER" ]; then
  api GET "/v1/identities/$IDENTITY/rider"
  if [ "$STATUS" != 200 ]; then
    api POST /v1/riders "{\"identityId\":\"$IDENTITY\",\"displayName\":\"Test Rider\"}"
    [ "$STATUS" = 200 ] || die "could not sign up the test rider ($STATUS): $BODY"
  fi
  RIDER="$(echo "$BODY" | field rider.id)"
  echo "$RIDER" | grep -Eq '^[0-9a-f-]{36}$' || die "no rider id in: $BODY"
  save
fi
echo "==> the test rider: $PHONE (rider $RIDER)"

echo "==> the top-up options"
api GET "/v1/wallets/$RIDER/topup-options?owner_type=OWNER_TYPE_RIDER"
[ "$STATUS" = 200 ] || die "the options were refused ($STATUS): $BODY"
echo "  from $(echo "$BODY" | field minAmount) to $(echo "$BODY" | field maxAmount) $(echo "$BODY" | field currencyCode); providers: $(echo "$BODY" | field providers)"
echo "$BODY" | field providers | grep -q zaincash ||
  die "ZainCash is not set up: ZAINCASH_CLIENT_ID and ZAINCASH_CLIENT_SECRET in instance/wallet-service.env, then scripts/deploy/pay-pages.sh"

BEFORE="$(balance)"
echo "  balance now: $BEFORE"

echo "==> opening a payment of $AMOUNT at ZainCash"
api POST "/v1/wallets/$RIDER/topups" \
  "{\"ownerType\":\"OWNER_TYPE_RIDER\",\"amount\":\"$AMOUNT\",\"provider\":\"zaincash\",\"language\":\"ar\"}"
[ "$STATUS" = 200 ] || die "the payment could not be opened ($STATUS): $BODY
  (wallet-service's log says why: docker logs --since 5m ride-wallet-service 2>&1 | grep -i zaincash | tail -5)"
TOPUP="$(echo "$BODY" | field topUpId)"
PAGE="$(echo "$BODY" | field redirectUrl)"
[ -n "$TOPUP" ] && [ -n "$PAGE" ] || die "no top-up id or payment page in: $BODY"

echo
echo "  Open this page (phone or computer) and pay, or cancel there:"
echo
echo "  $PAGE"
echo
echo "  Test wallet: 9647802999569   PIN 1111   OTP 111111"
echo "  (Ctrl+C stops waiting; the top-up is still settled by wallet-service every 2 minutes)"
echo

echo "==> waiting for ZainCash (top-up $TOPUP)"
LAST=""; END=$((SECONDS + 15 * 60))
while [ $SECONDS -lt $END ]; do
  api GET "/v1/wallets/$RIDER/topups/$TOPUP?owner_type=OWNER_TYPE_RIDER"
  NOW="$(echo "$BODY" | field status)"
  [ "$STATUS" = 200 ] || NOW="($STATUS)"
  if [ "$NOW" != "$LAST" ]; then echo "  $(date +%H:%M:%S)  $NOW"; LAST="$NOW"; fi
  case "$NOW" in succeeded | failed) break ;; esac
  sleep 5
done

AFTER="$(balance)"
echo "==> balance: $BEFORE -> $AFTER"
case "$LAST" in
  succeeded)
    python3 -c "import sys; from decimal import Decimal as D; sys.exit(0 if D('$AFTER') - D('$BEFORE') == D('$AMOUNT') else 1)" ||
      die "the top-up succeeded but the balance did not grow by $AMOUNT"
    echo "PASS: ZainCash took $AMOUNT and the wallet got it, once" ;;
  failed)
    echo "PASS: the payment did not go through, and nothing was credited (reason, for staff:)"
    docker exec -i ride-postgres sh -c 'psql -U ride_admin -d "$WALLET_DB_NAME" -t -A' <<< "select failure_reason from provider_topups where id = '$TOPUP';" | sed 's/^/  /' || true ;;
  *)
    die "still $LAST after 15 minutes" ;;
esac
