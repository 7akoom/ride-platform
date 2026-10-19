# Shared by the dev scripts that act as staff on a STAGING instance (make-voucher.sh,
# support-reply.sh). Sourced after die(), value() and PHONE are set. It signs in as the
# test staff member with the code identity-service writes to its log, keeps the session
# in instance/sim-staff.env, and gives that identity the owner role in the staff
# database. Afterwards call <method> <path> [json] sets STATUS and BODY, and
# field <path> reads a value from the JSON on stdin.

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

# The test staff member, with the owner role (it holds every permission).
staff_db << SQL > /dev/null || die "could not make the test staff member in the database"
insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
select gen_random_uuid(), '$IDENTITY', 'sim-staff@ride.test', 'Sim Staff', 'active', now(), now()
 where not exists (select 1 from staff_members where identity_id = '$IDENTITY');
insert into staff_member_roles (staff_id, role_id)
select id, '$OWNER_ROLE' from staff_members where identity_id = '$IDENTITY'
on conflict do nothing;
SQL

