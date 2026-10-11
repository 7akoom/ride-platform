#!/usr/bin/env bash
# Issues test vouchers on a STAGING instance, so the Rider app's "use a voucher" can be
# tried with a real code. Run it on the server, from the repo root:
#   bash scripts/dev/make-voucher.sh [amount] [count]
# Example: bash scripts/dev/make-voucher.sh 10000 3
#
# Vouchers are staff business (vouchers.manage). The first run makes a test staff member:
# it signs in as SIM_STAFF_PHONE (+9647700000991 by default) with the code
# identity-service writes to its log on staging, and gives that identity the owner role
# straight in the staff database. instance/sim-staff.env keeps it for the next runs.
# Then it issues a batch through the gateway (seller STAGING, valid 30 days), exports it
# so the codes work, and prints them.
# It refuses to run unless instance/instance.env says IDENTITY_APP_ENV=test.
set -Euo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
[ "$(value IDENTITY_APP_ENV)" = test ] ||
  die "this instance is not staging (IDENTITY_APP_ENV=test); test vouchers have no place on a real one"
command -v python3 > /dev/null || die "python3 is needed"

AMOUNT="${1:-10000}"; COUNT="${2:-1}"; PHONE="${SIM_STAFF_PHONE:-+9647700000991}"
echo "$AMOUNT" | grep -Eq '^[1-9][0-9]*$' || die "the amount is a whole number, for example 10000"
echo "$COUNT" | grep -Eq '^[1-9][0-9]?$' && [ "$COUNT" -le 20 ] || die "the count is 1 to 20"

API="http://127.0.0.1:$(value GATEWAY_PORT)"
[ "$API" != "http://127.0.0.1:" ] || API="http://127.0.0.1:18080"
STATE="instance/sim-staff.env"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
TOKEN=""; REFRESH=""; IDENTITY=""; STATUS=""; BODY=""

field() { # <path, e.g. batch.id>: from the JSON on stdin; empty when missing
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

save() { (umask 077 && printf 'IDENTITY=%s\nREFRESH=%s\n' "$IDENTITY" "$REFRESH" > "$STATE"); }

login() { # signs in as the test staff member with the code from identity-service's log
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
  call POST /v1/auth/otp:verify "{\"challengeId\":\"$challenge\",\"code\":\"$code\",\"clientId\":\"admin-web\",\"deviceId\":\"sim-staff\",\"deviceName\":\"sim-staff\",\"platform\":\"web\",\"appVersion\":\"1.0.0\"}"
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

staff_db() { # runs the SQL on stdin in staff-service's database
  docker exec -i ride-postgres sh -c 'psql -U ride_admin -d "$STAFF_DB_NAME" -t -A -q -v ON_ERROR_STOP=1'
}

renew

# The test staff member, with the owner role (the one that holds vouchers.manage).
staff_db << SQL > /dev/null || die "could not make the test staff member in the database"
insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
select gen_random_uuid(), '$IDENTITY', 'sim-staff@ride.test', 'Sim Staff', 'active', now(), now()
 where not exists (select 1 from staff_members where identity_id = '$IDENTITY');
insert into staff_member_roles (staff_id, role_id)
select id, '$OWNER_ROLE' from staff_members where identity_id = '$IDENTITY'
on conflict do nothing;
SQL

RUN="$(date +%s)"
EXPIRES="$(python3 -c 'import datetime as t; print((t.datetime.now(t.timezone.utc) + t.timedelta(days=30)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
call POST /v1/admin/voucher-batches \
  "{\"label\":\"STAGING test $RUN\",\"seller\":\"STAGING\",\"amount\":\"$AMOUNT\",\"quantity\":$COUNT,\"expiresAt\":\"$EXPIRES\",\"idempotencyKey\":\"sim-staff-$RUN\"}"
[ "$STATUS" = 200 ] || die "the batch was refused ($STATUS): $BODY"
BATCH="$(echo "$BODY" | field batch.id)"
[ -n "$BATCH" ] || die "no batch id in: $BODY"

call POST "/v1/admin/voucher-batches/$BATCH:export" '{}'
[ "$STATUS" = 200 ] || die "the batch could not be exported ($STATUS): $BODY"

echo "==> $COUNT voucher(s) of $AMOUNT, valid until $EXPIRES:"
echo "$BODY" | python3 -c '
import json, sys
for v in json.load(sys.stdin).get("vouchers", []):
    print("  " + v["code"] + "   (" + v["serial"] + ")")'
