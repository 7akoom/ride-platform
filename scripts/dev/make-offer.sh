#!/usr/bin/env bash
# Puts test offers on a STAGING instance, so the Rider app's home can be seen with
# them and without them. Run it on the server, from the repo root:
#   bash scripts/dev/make-offer.sh             two featured coupons, first ride and loyalty on
#   bash scripts/dev/make-offer.sh --off       the test coupons ended, first ride and loyalty off:
#                                              the home shows no offers
#   bash scripts/dev/make-offer.sh --defaults  first ride and loyalty back to the built-in
#                                              50% / 20% every 10th
#
# Offers are staff business (promotions.manage): it signs in as the test staff member
# of make-voucher.sh (SIM_STAFF_PHONE, +9647700000991 by default, the owner role), made
# on the first run of either script. The coupons it made are kept in
# instance/sim-offers.env, for --off.
# It refuses to run unless instance/instance.env says IDENTITY_APP_ENV=test.
set -Euo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
[ "$(value IDENTITY_APP_ENV)" = test ] ||
  die "this instance is not staging (IDENTITY_APP_ENV=test); test offers have no place on a real one"
command -v python3 > /dev/null || die "python3 is needed"

MODE="${1:-on}"; PHONE="${SIM_STAFF_PHONE:-+9647700000991}"
case "$MODE" in on | --off | --defaults) ;; *) die "usage: make-offer.sh [--off | --defaults]" ;; esac

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

# The test staff member, with the owner role (it holds promotions.manage).
staff_db << SQL > /dev/null || die "could not make the test staff member in the database"
insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
select gen_random_uuid(), '$IDENTITY', 'sim-staff@ride.test', 'Sim Staff', 'active', now(), now()
 where not exists (select 1 from staff_members where identity_id = '$IDENTITY');
insert into staff_member_roles (staff_id, role_id)
select id, '$OWNER_ROLE' from staff_members where identity_id = '$IDENTITY'
on conflict do nothing;
SQL

OFFERS="instance/sim-offers.env"

settings() { # <first ride %> <first ride cap> <loyalty every> <loyalty %>
  call PUT /v1/admin/promotion-settings \
    "{\"firstRidePercent\":\"$1\",\"firstRideMaxAmount\":\"$2\",\"loyaltyEvery\":$3,\"loyaltyPercent\":\"$4\"}"
  [ "$STATUS" = 200 ] || die "the promotion settings were refused ($STATUS): $BODY"
}

coupon() { # <code> <json fields>: a featured coupon, printed
  call POST /v1/admin/coupons "{\"code\":\"$1\",\"featured\":true,$2}"
  [ "$STATUS" = 200 ] || die "the coupon $1 was refused ($STATUS): $BODY"
  echo "$1" >> "$OFFERS"
}

case "$MODE" in
  --off)
    if [ -f "$OFFERS" ]; then
      while read -r code; do
        [ -n "$code" ] || continue
        call PATCH "/v1/admin/coupons/$code" '{"active":false}'
        [ "$STATUS" = 200 ] || [ "$STATUS" = 404 ] || die "the coupon $code could not be ended ($STATUS): $BODY"
        echo "  ended   $code"
      done < "$OFFERS"
      rm -f "$OFFERS"
    fi
    settings 0 "" 0 0
    echo "==> no test offers; first ride and loyalty off (bring them back: --defaults)"
    ;;
  --defaults)
    settings 50 "" 10 20
    echo "==> first ride 50%, loyalty 20% every 10th trip (the built-in discounts)"
    ;;
  on)
    RUN="$(printf '%04d' $(( $(date +%s) % 10000 )))"
    WEEK="$(python3 -c 'import datetime as t; print((t.datetime.now(t.timezone.utc) + t.timedelta(days=7)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
    DAYS="$(python3 -c 'import datetime as t; print((t.datetime.now(t.timezone.utc) + t.timedelta(days=3)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
    (umask 077 && touch "$OFFERS")
    coupon "SAVE30-$RUN" "\"description\":\"30% off your next two rides\",\"discountType\":\"DISCOUNT_TYPE_PERCENTAGE\",\"discountValue\":\"30\",\"maxDiscountAmount\":\"3000\",\"perRiderLimit\":2,\"validUntil\":\"$WEEK\""
    coupon "RIDE2000-$RUN" "\"description\":\"2,000 off a ride of 4,000 or more\",\"discountType\":\"DISCOUNT_TYPE_FIXED_AMOUNT\",\"discountValue\":\"2000\",\"minimumFareAmount\":\"4000\",\"validUntil\":\"$DAYS\""
    settings 50 5000 5 20
    echo "==> featured coupons: SAVE30-$RUN (30%, up to 3,000, two rides, 7 days) and"
    echo "    RIDE2000-$RUN (2,000 off from 4,000, 3 days); first ride 50% up to 5,000;"
    echo "    loyalty 20% every 5th trip. Pull to refresh the app's home. Off again: --off"
    ;;
esac
