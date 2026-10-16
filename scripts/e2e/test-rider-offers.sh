#!/usr/bin/env bash
# End-to-end test of a rider's offers (what the app's home shows) on the
# real services, through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-rider-offers.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken).
#
# What it proves:
#   1. staff mark coupons featured; a rider sees the featured ones running
#      now that they may still use, with the uses left, never one only typed
#      in, turned off, or for new riders once they completed a trip
#   2. the first-ride discount for a rider with no completed trip, and how
#      far the next loyalty discount is for one with trips; neither when
#      staff turn it off
#   3. a typed code is checked for the rider: theirs to use, unknown, off,
#      for new riders only, or used up for them
#   4. only the rider reads their own offers
#
# It creates two riders and a staff member and removes them again with the
# coupons; the promotion settings are put back as they were.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
PREFIX="E2E-OFR-$RUN"
UNKNOWN="$(uuid)"
UNTIL="$(python3 -c 'import datetime; print((datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(days=2)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
OTHER_ID=""
SETTINGS=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  if [ -n "$SETTINGS" ]; then
    sql ride-pricing-postgres "update promotion_settings set $SETTINGS;" > /dev/null 2>&1 || true
  fi
  sql ride-staff-postgres "delete from staff_members where id = '$OWNER_STAFF_ID';" > /dev/null 2>&1 || true
  sql ride-pricing-postgres "
    delete from rider_trip_stats where rider_id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');
    delete from coupon_redemptions where coupon_id in (select id from coupons where code like '$PREFIX-%');
    delete from coupons where code like '$PREFIX-%';" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID$OTHER_ID" ]; then
    sql ride-rider-postgres "delete from riders where id in ('${RIDER_ID:-$UNKNOWN}', '${OTHER_ID:-$UNKNOWN}');" > /dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1"; }

body_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE"
}

http() { # <method> <path> <token> [json body]
  local method="$1" path="$2" token="$3" body="${4:-}"
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$method" "$BASE$path")

  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")

  curl "${args[@]}"
}

expect() { # <label> <expected status> <method> <path> <token> [json body]
  local actual
  actual="$(http "$3" "$4" "$5" "${6:-}")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s: %s\n' "$1" "$2" "$actual" "$(head -c 300 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

check() { # <label> <expected> <actual>
  if [ "$2" = "$3" ]; then
    printf '  ok    %s -> %s\n' "$1" "$3"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "$3"
    FAILURES=$((FAILURES + 1))
  fi
}

coupon() { # <suffix> <featured> [extra json fields]
  local extra="${3:-}"
  expect "coupon $1 (featured $2)" 200 POST /v1/admin/coupons "$OWNER" \
    "{\"code\":\"$PREFIX-$1\",\"discountType\":\"DISCOUNT_TYPE_PERCENTAGE\",\"discountValue\":\"30\",\"maxDiscountAmount\":\"3000\",\"validUntil\":\"$UNTIL\",\"featured\":$2${extra:+,$extra}}"
}

# The rider's test coupons, as code:usesLeft, sorted.
OUR_COUPONS="','.join(sorted(c['code'].replace('$PREFIX-', '') + ':' + str(c.get('usesLeft', 0)) for c in d.get('coupons', []) if c['code'].startswith('$PREFIX-')))"

offers() { # <label> <token> <rider id>
  expect "$1" 200 GET "/v1/riders/$3/offers" "$2"
}

check_code() { # <label> <code> <expected status>
  expect "$1" 200 POST "/v1/riders/$RIDER_ID/offers:check" "$RIDER" "{\"code\":\"$2\"}"
  check "  is $3" "$3" "$(body_field 'd.get("status", "COUPON_STATUS_UNSPECIFIED").replace("COUPON_STATUS_", "")')"
}

RIDER="$(mint "$RIDER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"

SETTINGS="$(sql ride-pricing-postgres "select format('first_ride_percent = %s, first_ride_max_amount = %s, loyalty_every = %s, loyalty_percent = %s, loyalty_max_amount = %s, updated_by = %s, updated_at = %L', first_ride_percent, coalesce(first_ride_max_amount::text, 'null'), loyalty_every, loyalty_percent, coalesce(loyalty_max_amount::text, 'null'), coalesce(quote_literal(updated_by::text), 'null'), updated_at) from promotion_settings")"
[ -n "$SETTINGS" ] || { echo "ABORT: promotion_settings is empty (is pricing migration 00014 applied?)" >&2; exit 2; }

echo "==> [0/4] two riders, an owner, the automatic discounts on, four coupons"
sql ride-staff-postgres "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
  values ('$OWNER_STAFF_ID', '$OWNER_IDENTITY', 'e2e-offers-$RUN@ride.test', 'E2E Offers', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values ('$OWNER_STAFF_ID', '$OWNER_ROLE');" > /dev/null
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Offers Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"
expect "another rider" 200 POST /v1/riders "$OTHER" "{\"identityId\":\"$OTHER_IDENTITY\",\"displayName\":\"E2E Offers Other\"}"
OTHER_ID="$(body_field 'd["rider"]["id"]')"
expect "first ride 50% up to 5000, loyalty 20% every 5th" 200 PUT /v1/admin/promotion-settings "$OWNER" \
  '{"firstRidePercent":"50","firstRideMaxAmount":"5000","loyaltyEvery":5,"loyaltyPercent":"20"}'
coupon SHOWN true '"perRiderLimit":2'
check "  staff see it featured" True "$(body_field 'd["coupon"].get("featured", False)')"
coupon TYPED false
coupon NEW true '"newRidersOnly":true'
coupon OFF true
expect "OFF is turned off" 200 PATCH "/v1/admin/coupons/$PREFIX-OFF" "$OWNER" '{"active":false}'

echo "==> [1/4] a rider with no trips"
offers "the rider's offers" "$RIDER" "$RIDER_ID"
check "the featured coupons they may use" "NEW:1,SHOWN:2" "$(body_field "$OUR_COUPONS")"
check "the first ride, 50% up to 5000" "50.0 5000.0" "$(body_field 'str(float(d["firstRide"]["percent"])) + " " + str(float(d["firstRide"]["maxAmount"]))')"
check "no loyalty yet" False "$(body_field '"loyalty" in d')"
check "never how much it was used" False "$(body_field '"redemptionCount" in json.dumps(d)')"

echo "==> [2/4] after three trips, and a coupon used up for them"
sql ride-pricing-postgres "insert into rider_trip_stats (rider_id, completed_trip_count) values ('$RIDER_ID', 3);" > /dev/null
sql ride-pricing-postgres "
  insert into coupon_redemptions (coupon_id, rider_id, discount_amount, status, redeemed_at)
  select id, '$RIDER_ID', 1000, 'redeemed', now() from coupons, generate_series(1, 2) where code = '$PREFIX-SHOWN';" > /dev/null
offers "the rider's offers" "$RIDER" "$RIDER_ID"
check "neither SHOWN (used twice) nor NEW (they have trips)" "" "$(body_field "$OUR_COUPONS")"
check "no first ride" False "$(body_field '"firstRide" in d')"
check "loyalty: two trips to the 5th" "5 3 2" "$(body_field '" ".join(str(d["loyalty"][k]) for k in ("every", "completedTrips", "tripsToGo"))')"

echo "==> [3/4] a typed code"
check_code "a coupon only typed in" "$(echo "$PREFIX-typed" | tr 'A-Z' 'a-z')" APPLIED
check "  with what it gives" "$PREFIX-TYPED 30" "$(body_field 'd["coupon"]["code"] + " " + d["coupon"]["discountValue"]')"
check_code "an unknown code" "$PREFIX-NOPE" NOT_FOUND
check_code "a coupon turned off" "$PREFIX-OFF" ENDED
check_code "for new riders only" "$PREFIX-NEW" NEW_RIDERS_ONLY
check_code "used up for them" "$PREFIX-SHOWN" ALREADY_USED

echo "==> [4/4] who may read them, and the discounts turned off"
expect "another rider's offers" 403 GET "/v1/riders/$RIDER_ID/offers" "$OTHER"
expect "another rider's check" 403 POST "/v1/riders/$RIDER_ID/offers:check" "$OTHER" '{"code":"X"}'
expect "without a token" 401 GET "/v1/riders/$RIDER_ID/offers" ""
expect "staff turn both off" 200 PUT /v1/admin/promotion-settings "$OWNER" \
  '{"firstRidePercent":"0","loyaltyEvery":0,"loyaltyPercent":"0"}'
offers "the other rider (no trips)" "$OTHER" "$OTHER_ID"
check "sees SHOWN and NEW, no first ride" "NEW:1,SHOWN:2 False" "$(body_field "$OUR_COUPONS + ' ' + str('firstRide' in d)")"
offers "the rider (three trips)" "$RIDER" "$RIDER_ID"
check "nothing at all" " False False" "$(body_field "$OUR_COUPONS + ' ' + str('firstRide' in d) + ' ' + str('loyalty' in d)")"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: a rider sees the featured coupons they may use and their automatic discounts, checks a typed code, and only their own"
