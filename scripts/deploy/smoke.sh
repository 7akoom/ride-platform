#!/usr/bin/env bash
# Checks the instance: first the gateway on this server, then from the
# outside through the public domain, nginx and TLS. Run on the server, from
# the repo root:
#   bash scripts/deploy/smoke.sh
# Signing in end to end:
#   - staging (IDENTITY_APP_ENV=test): the code is read from the log
#   - production: only when asked, with a phone that receives the code:
#       SMOKE_PHONE=+9647501234567 bash scripts/deploy/smoke.sh
#     it asks for the code that arrived (by WhatsApp or SMS)
set -uo pipefail

value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }
API="https://$(value API_DOMAIN)"
FILES="https://$(value FILES_DOMAIN)"
LOCAL="http://127.0.0.1:$(value GATEWAY_PORT)"
BODY="$(mktemp)"
trap 'rm -f "$BODY"' EXIT
fails=0
check() { if [ "$2" = "$3" ]; then printf '  ok    %s -> %s\n' "$1" "$3"; else printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "$3"; fails=$((fails + 1)); fi; }
status() { curl -s -o "$BODY" -w '%{http_code}' "$@"; }
field() { python3 -c 'import json,sys
try: print(json.load(open(sys.argv[1])).get(sys.argv[2], ""))
except Exception: print("")' "$BODY" "$1"; }

echo "==> the gateway on this server ($LOCAL)"
check "a protected route without a token" 401 "$(status "$LOCAL/v1/me")"
if [ "$fails" -gt 0 ]; then
  echo
  echo "FAIL: the gateway itself does not answer; see: docker logs ride-gateway --tail 50"
  exit 1
fi

echo "==> $API"
check "a protected route without a token (nginx -> gateway -> service)" 401 "$(status "$API/v1/me")"
check "an unknown route" 404 "$(status "$API/v1/nothing-here")"
request_id="$(curl -s -o /dev/null -D - "$API/v1/me" | tr -d '\r' | sed -n 's/^[Xx]-[Rr]equest-[Ii]d: //p')"
check "every answer carries its request id (X-Request-Id)" ok "$( [[ "$request_id" =~ ^[0-9a-f]{32}$ ]] && echo ok || echo "'$request_id'")"
check "TLS certificate is valid" 0 "$(curl -s -o /dev/null "$API/v1/me"; echo $?)"
files_code="$(status "$FILES/")"
case "$files_code" in 200|403) check "the file store answers through nginx" ok ok ;; *) check "the file store answers through nginx" "200 or 403" "$files_code" ;; esac
if [ "$fails" -gt 0 ]; then
  echo "  (the gateway answers here but not through $API: is the nginx site on? sudo bash scripts/deploy/nginx-site.sh)"
fi

TILES="$(value TILES_DOMAIN)"
if [ -n "$TILES" ]; then
  echo "==> https://$TILES (the map)"
  check "a style" 200 "$(status "https://$TILES/styles/light-ar.json")"
  check "a piece of the basemap (range request)" 206 "$(status -H 'Range: bytes=0-126' "https://$TILES/basemap.pmtiles")"
fi

sign_in() { # <phone> <how to get the code: log|ask>
  local phone="$1" code otp challenge token
  code="$(status -X POST "$API/v1/auth/otp:request" -H 'Content-Type: application/json' \
    -d "{\"identifier\":{\"type\":\"IDENTIFIER_TYPE_PHONE\",\"value\":\"$phone\"},\"deliveryChannel\":\"OTP_DELIVERY_CHANNEL_AUTO\"}")"
  check "a code is asked for" 200 "$code"
  [ "$code" = 200 ] || { echo "        $(head -c 300 "$BODY")"; return; }
  challenge="$(field challengeId)"

  if [ "$2" = log ]; then
    sleep 2
    otp="$(docker logs --since 1m ride-identity-service 2>&1 | grep -o '"otp_code":"[0-9]*"' | tail -1 | grep -o '[0-9]\+')"
  else
    read -r -p "  the code that arrived on $phone: " otp
  fi

  code="$(status -X POST "$API/v1/auth/otp:verify" -H 'Content-Type: application/json' \
    -d "{\"challengeId\":\"$challenge\",\"code\":\"$otp\",\"clientId\":\"smoke\",\"deviceId\":\"smoke-$(hostname)\",\"deviceName\":\"Smoke test\",\"platform\":\"android\",\"appVersion\":\"1.0.0\"}")"
  check "the code signs in" 200 "$code"
  token="$(field accessToken)"
  check "who am I, with the token" 200 "$(status -H "Authorization: Bearer $token" "$API/v1/me")"
}

if [ "$(value IDENTITY_APP_ENV)" = test ]; then
  echo "==> signing in (staging: the code is read from the log)"
  sign_in "+964750$(date +%s | tail -c 8)" log
elif [ -n "${SMOKE_PHONE:-}" ]; then
  echo "==> signing in with a real code sent to $SMOKE_PHONE"
  sign_in "$SMOKE_PHONE" ask
else
  echo "==> signing in: skipped (codes are really sent; SMOKE_PHONE=+964... to try one)"
fi

echo
if [ "$fails" -gt 0 ]; then echo "FAIL: $fails check(s)"; exit 1; fi
echo "PASS: $API answers through nginx and TLS, the services behind it work"
