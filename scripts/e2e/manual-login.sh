#!/usr/bin/env bash
# One real login through the gateway, to prove the part automation cannot: that
# identity-service records the USER's address, device and language, not the
# gateway's. Needs a phone (or email) that can receive the code.
# Run from the ride-platform repo root:
#   PHONE=+9647XXXXXXXXX bash scripts/e2e/manual-login.sh
#   EMAIL=you@example.com  bash scripts/e2e/manual-login.sh
#
# It pretends to be a phone app behind a proxy: it sends
#   User-Agent: RideE2E/1.0 (manual-login)
#   Accept-Language: ar-IQ,ar;q=0.9
#   X-Forwarded-For: 203.0.113.77      (a documentation address, what nginx would add)
# and afterwards checks the session identity stored for this login:
#   - ipAddress must be 203.0.113.77 (not the gateway's 172.x address)
#   - userAgent must be RideE2E/1.0 (manual-login) (not grpc-go/...)
# The OTP message you receive should be in Arabic. Tokens are never printed.
#
# It ends the session it created at the end (logout), so it leaves nothing behind
# except the identity for that phone number, which login creates.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
FAKE_IP="203.0.113.77"
FAKE_UA="RideE2E/1.0 (manual-login)"
FAKE_LANG="ar-IQ,ar;q=0.9"

if [ -n "${PHONE:-}" ]; then
  IDENTIFIER="{\"type\":\"IDENTIFIER_TYPE_PHONE\",\"value\":\"$PHONE\"}"
elif [ -n "${EMAIL:-}" ]; then
  IDENTIFIER="{\"type\":\"IDENTIFIER_TYPE_EMAIL\",\"value\":\"$EMAIL\"}"
else
  echo "usage: PHONE=+9647XXXXXXXXX bash $0   (or EMAIL=you@example.com)" >&2
  exit 2
fi

BODY_FILE="$(mktemp)"
trap 'rm -f "$BODY_FILE"' EXIT

FAILURES=0
pass() { printf '  ok    %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

field() { python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE"; }

# call <method> <path> <token> [json body] -> status; body in $BODY_FILE; sends the fake client headers
call() {
  local method="$1" path="$2" token="$3" body="${4:-}"
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$method" "$BASE$path"
    -H "User-Agent: $FAKE_UA" -H "Accept-Language: $FAKE_LANG" -H "X-Forwarded-For: $FAKE_IP")

  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")

  curl "${args[@]}"
}

need() { # <label> <expected status> <actual status>
  if [ "$3" = "$2" ]; then
    pass "$1 -> $3"
  else
    fail "$1 -> expected $2, got $3: $(head -c 300 "$BODY_FILE")"
    exit 1
  fi
}

echo "==> [1/6] requesting a code (it should arrive in Arabic)"
STATUS="$(call POST "/v1/auth/otp:request" "" "{\"identifier\":$IDENTIFIER,\"deliveryChannel\":\"OTP_DELIVERY_CHANNEL_AUTO\"}")"
need "otp:request" 200 "$STATUS"
CHALLENGE="$(field 'd["challengeId"]')"

read -r -p "    Enter the code you received: " CODE

echo "==> [2/6] verifying the code"
STATUS="$(call POST "/v1/auth/otp:verify" "" "{\"challengeId\":\"$CHALLENGE\",\"code\":\"$CODE\",\"clientId\":\"e2e\",\"deviceId\":\"manual-login-device\",\"deviceName\":\"Manual login\",\"platform\":\"android\",\"appVersion\":\"1.0.0\"}")"
need "otp:verify" 200 "$STATUS"
ACCESS="$(field 'd["accessToken"]')"
REFRESH="$(field 'd["refreshToken"]')"

echo "==> [3/6] who am I"
STATUS="$(call GET "/v1/me" "$ACCESS")"
need "GET /v1/me" 200 "$STATUS"
echo "    identity: $(field 'd["identityId"]')"

echo "==> [4/6] the session identity stored for this login"
STATUS="$(call GET "/v1/me/sessions" "$ACCESS")"
need "GET /v1/me/sessions" 200 "$STATUS"

CURRENT_IP="$(field 'next((s.get("ipAddress","") for s in d["sessions"] if s.get("isCurrent")), "")')"
CURRENT_UA="$(field 'next((s.get("userAgent","") for s in d["sessions"] if s.get("isCurrent")), "")')"

echo "    ip address recorded: ${CURRENT_IP:-(none)}"
echo "    user agent recorded: ${CURRENT_UA:-(none)}"

if [ "$CURRENT_IP" = "$FAKE_IP" ]; then
  pass "the session has the user's address"
else
  fail "the session has '$CURRENT_IP' instead of $FAKE_IP: is TRUSTED_PROXY_CIDRS set for identity-service and does it cover the gateway's network?"
fi

if [ "$CURRENT_UA" = "$FAKE_UA" ]; then
  pass "the session has the user's device"
else
  fail "the session has '$CURRENT_UA' instead of '$FAKE_UA'"
fi

echo "==> [5/6] refreshing the token pair"
STATUS="$(call POST "/v1/auth/token:refresh" "" "{\"refreshToken\":\"$REFRESH\"}")"
need "token:refresh" 200 "$STATUS"
REFRESH="$(field 'd["refreshToken"]')"

echo "==> [6/6] logging out (ends the session this script created)"
STATUS="$(call POST "/v1/auth/logout" "" "{\"refreshToken\":\"$REFRESH\"}")"
need "logout" 200 "$STATUS"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: a real login works through the gateway and identity recorded the user's address and device"
  echo "      Also check: the code arrived in Arabic."
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
