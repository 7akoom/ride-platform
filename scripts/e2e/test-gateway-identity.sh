#!/usr/bin/env bash
# End-to-end test of the IDENTITY routes through the gateway.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-gateway-identity.sh
#
# It needs no phone and sends no SMS: every OTP request it makes is invalid, so no
# code is ever generated. What it proves:
#   1. the five public routes (OTP request and verify, refresh, logout, logout-all)
#      reach identity-service with no token at all, and also with a stale token in
#      the header (apps send one when they refresh) - the gateway does not refuse them
#   2. the seven routes that need a token answer 401 from identity-service without
#      one, from the gateway when the header is not a JWT, and reach identity with a
#      well-formed token
#   3. the internal service token is still refused at the gateway
#
# What it cannot prove, because it needs a real login: that identity records the
# USER's address, device and language instead of the gateway's. Use
# scripts/e2e/manual-login.sh for that, once, with your own phone.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"

RIDER_IDENTITY_1="a0000000-0000-4000-8000-0000000000a1"
ZERO_ID="00000000-0000-4000-8000-000000000000"

GATEWAY_REFUSAL="a user access token is required"
FAILURES=0
BODY_FILE="$(mktemp)"
trap 'rm -f "$BODY_FILE"' EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1" "${@:2}"; }

pass() { printf '  ok    %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

# http <method> <path> <token> [json body] -> the HTTP status; the body goes to $BODY_FILE
http() {
  local method="$1" path="$2" token="$3" body="${4:-}"
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$method" "$BASE$path")

  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")

  curl "${args[@]}"
}

message() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("message",""))' "$BODY_FILE" 2>/dev/null || true; }

# The router answers "Not Found" / "Method Not Allowed" / "Not Implemented"; the gateway's own
# credential check answers $GATEWAY_REFUSAL; anything else came from identity-service.
reached_identity() { # <status> <message>
  case "$2" in
    "Not Found" | "Method Not Allowed" | "Not Implemented" | "$GATEWAY_REFUSAL") return 1 ;;
  esac

  case "$1" in 405 | 501) return 1 ;; esac

  return 0
}

expect_reaches_identity() { # <label> <method> <path> <token> [json body]
  local status
  status="$(http "$2" "$3" "$4" "${5:-}")"

  if reached_identity "$status" "$(message)"; then
    printf '  ok    %s -> reached identity-service (%s)\n' "$1" "$status"
  else
    printf '  FAIL  %s -> %s: %s\n' "$1" "$status" "$(head -c 200 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

expect_status() { # <label> <expected status> <expected message: identity|gateway> <method> <path> <token> [json body]
  local status
  status="$(http "$4" "$5" "$6" "${7:-}")"

  local from=identity
  [ "$(message)" = "$GATEWAY_REFUSAL" ] && from=gateway

  if [ "$status" = "$2" ] && [ "$from" = "$3" ]; then
    printf '  ok    %s -> %s from %s\n' "$1" "$status" "$from"
  else
    printf '  FAIL  %s -> expected %s from %s, got %s from %s: %s\n' "$1" "$2" "$3" "$status" "$from" "$(head -c 200 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

echo "==> [1/4] preparing: the gateway, tokens"
curl -sS -o /dev/null --max-time 5 "$BASE/v1/zones" -H "Authorization: Bearer x" \
  || { echo "FAIL: the gateway is not answering on $BASE" >&2; exit 1; }

IDENTITY_A="a0000000-0000-4000-8000-0000000000a9"
TOKEN_VALID_SHAPE="$(mint "$IDENTITY_A")"
TOKEN_STALE="$(mint "$RIDER_IDENTITY_1" -ttl=-1h)"

echo "==> [2/4] the public routes need no token"
VERIFY="{\"challengeId\":\"$ZERO_ID\",\"code\":\"000000\",\"clientId\":\"e2e\",\"deviceId\":\"e2e\",\"deviceName\":\"e2e\",\"platform\":\"android\",\"appVersion\":\"0\"}"
expect_reaches_identity "otp:request (an invalid identifier: no code is sent)" POST "/v1/auth/otp:request" "" '{"identifier":{"type":"IDENTIFIER_TYPE_PHONE","value":""}}'
expect_reaches_identity "otp:verify with an unknown challenge"                 POST "/v1/auth/otp:verify" "" "$VERIFY"
expect_reaches_identity "token:refresh with an unknown refresh token"          POST "/v1/auth/token:refresh" "" '{"refreshToken":"not-a-real-refresh-token"}'
expect_reaches_identity "logout with an unknown refresh token"                 POST "/v1/auth/logout" "" '{"refreshToken":"not-a-real-refresh-token"}'
expect_reaches_identity "logout-all with an unknown refresh token"             POST "/v1/auth/logout-all" "" '{"refreshToken":"not-a-real-refresh-token"}'

echo "==> [3/4] a stale access token in the header does not block them"
expect_reaches_identity "otp:verify with an expired access token"              POST "/v1/auth/otp:verify" "$TOKEN_STALE" "$VERIFY"
expect_reaches_identity "token:refresh with an expired access token"           POST "/v1/auth/token:refresh" "$TOKEN_STALE" '{"refreshToken":"not-a-real-refresh-token"}'
expect_reaches_identity "logout with an expired access token"                  POST "/v1/auth/logout" "$TOKEN_STALE" '{"refreshToken":"not-a-real-refresh-token"}'

echo "==> [4/4] the routes that need a token"
for route in "GET /v1/me" "GET /v1/me/sessions" "DELETE /v1/me/sessions/$ZERO_ID" \
  "POST /v1/me/identifiers/link-otp" "POST /v1/me/identifiers/link" \
  "POST /v1/me/identifiers/unlink-otp" "POST /v1/me/identifiers/unlink"; do
  method="${route%% *}"
  path="${route#* }"
  body=""
  [ "$method" = POST ] && body='{}'

  expect_status "$method $path without a token" 401 identity "$method" "$path" "" "$body"
done

expect_status "GET /v1/me with a token that is not a JWT"      401 gateway GET "/v1/me" "not-a-token"
expect_status "GET /v1/me with the internal service token"     401 gateway GET "/v1/me" "$INTERNAL_TOKEN"
expect_reaches_identity "GET /v1/me with a well-formed token"       GET "/v1/me" "$TOKEN_VALID_SHAPE"
expect_reaches_identity "GET /v1/me/sessions with a well-formed token" GET "/v1/me/sessions" "$TOKEN_VALID_SHAPE"
expect_reaches_identity "DELETE /v1/me/sessions/{id} with a well-formed token" DELETE "/v1/me/sessions/$ZERO_ID" "$TOKEN_VALID_SHAPE"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: the identity routes work through the gateway: public ones open, the others need a token, the internal token stays out"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
