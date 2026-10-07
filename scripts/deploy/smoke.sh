#!/usr/bin/env bash
# Checks the instance from the outside: through the public domain, nginx,
# TLS, the gateway and the services. Run on the server, from the repo root:
#   bash scripts/deploy/smoke.sh
# On a staging instance (codes in the log) it also signs in end to end.
set -uo pipefail

value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }
API="https://$(value API_DOMAIN)"
FILES="https://$(value FILES_DOMAIN)"
fails=0
check() { if [ "$2" = "$3" ]; then printf '  ok    %s -> %s\n' "$1" "$3"; else printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "$3"; fails=$((fails + 1)); fi; }
status() { curl -s -o /tmp/ride-smoke.json -w '%{http_code}' "$@"; }

echo "==> $API"
check "a protected route without a token (gateway -> service)" 401 "$(status "$API/v1/me")"
check "an unknown route" 404 "$(status "$API/v1/nothing-here")"
check "TLS certificate is valid" 0 "$(curl -s -o /dev/null "$API/v1/me"; echo $?)"
files_code="$(status "$FILES/")"
case "$files_code" in 200|403) check "the file store answers through nginx" ok ok ;; *) check "the file store answers through nginx" "200 or 403" "$files_code" ;; esac

if [ "$(value IDENTITY_APP_ENV)" = test ]; then
  echo "==> signing in (staging: the code is read from the log)"
  phone="+964750$(date +%s | tail -c 8)"
  code="$(status -X POST "$API/v1/auth/otp:request" -H 'Content-Type: application/json' \
    -d "{\"identifier\":{\"type\":\"IDENTIFIER_TYPE_PHONE\",\"value\":\"$phone\"},\"deliveryChannel\":\"OTP_DELIVERY_CHANNEL_AUTO\"}")"
  check "a code is asked for" 200 "$code"
  challenge="$(python3 -c 'import json; print(json.load(open("/tmp/ride-smoke.json")).get("challengeId", ""))')"
  sleep 2
  otp="$(docker logs --since 1m ride-identity-service 2>&1 | grep -o '"otp_code":"[0-9]*"' | tail -1 | grep -o '[0-9]\+')"
  code="$(status -X POST "$API/v1/auth/otp:verify" -H 'Content-Type: application/json' \
    -d "{\"challengeId\":\"$challenge\",\"code\":\"$otp\",\"clientId\":\"smoke\",\"deviceId\":\"smoke-$(hostname)\",\"deviceName\":\"Smoke test\",\"platform\":\"android\",\"appVersion\":\"1.0.0\"}")"
  check "the code signs in" 200 "$code"
  token="$(python3 -c 'import json; print(json.load(open("/tmp/ride-smoke.json")).get("accessToken", ""))')"
  check "who am I, with the token" 200 "$(status -H "Authorization: Bearer $token" "$API/v1/me")"
fi

rm -f /tmp/ride-smoke.json
echo
if [ "$fails" -gt 0 ]; then echo "FAIL: $fails check(s)"; exit 1; fi
echo "PASS: $API answers through nginx and TLS, the services behind it work"
