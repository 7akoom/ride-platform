#!/usr/bin/env bash
# Requests a ride as a test rider on a STAGING instance and follows it, so a second
# rider can be played beside the Rider app on a phone (next trips, for one). Run it on
# the server, from the repo root:
#   bash scripts/dev/sim-ride.sh [--from 36.1911,44.0092] [--to 36.2050,44.0200] [--class economy|comfort]
#     [--from-name "Family Mall"] [--to-name "Erbil Citadel"]
# The names are the addresses the captain sees (a rider's app sends the place picked);
# without them the ride carries placeholder ones.
#
# The test rider is SIM_RIDER_PHONE (+9647700000992 by default), the one
# try-topup.sh uses; the first run signs it in with the code identity-service writes
# to its log on staging and signs it up. instance/sim-rider.env keeps it.
# The ride is paid in cash. Every change of the ride is printed until it ends;
# Ctrl+C cancels it if it is still going.
# It refuses to run unless instance/instance.env says IDENTITY_APP_ENV=test.
set -Euo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
[ "$(value IDENTITY_APP_ENV)" = test ] ||
  die "this instance is not staging (IDENTITY_APP_ENV=test); a test rider has no place on a real one"
command -v python3 > /dev/null || die "python3 is needed"

FROM="36.1911,44.0092"; TO="36.2050,44.0200"; CLASS="economy"; FROM_NAME="Test pickup"; TO_NAME="Test destination"; PHONE="${SIM_RIDER_PHONE:-+9647700000992}"
while [ $# -gt 0 ]; do
  case "$1" in
    --from) FROM="$2"; shift 2 ;;
    --to) TO="$2"; shift 2 ;;
    --class) CLASS="$2"; shift 2 ;;
    --from-name) FROM_NAME="$2"; shift 2 ;;
    --to-name) TO_NAME="$2"; shift 2 ;;
    *) die "unknown option $1" ;;
  esac
done
for point in "$FROM" "$TO"; do
  echo "$point" | grep -Eq '^-?[0-9.]+,-?[0-9.]+$' || die "--from and --to are lat,lng"
done
case "$CLASS" in economy | comfort) ;; *) die "--class is economy or comfort" ;; esac

API="http://127.0.0.1:$(value GATEWAY_PORT)"
[ "$API" != "http://127.0.0.1:" ] || API="http://127.0.0.1:18080"
STATE="instance/sim-rider.env"
TOKEN=""; REFRESH=""; IDENTITY=""; RIDER=""; STATUS=""; BODY=""

field() { # <path, e.g. trip.status>: from the JSON on stdin; empty when missing
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

renew

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

api GET "/v1/trips:active?rider_id=$RIDER"
[ "$STATUS" = 200 ] && die "the test rider already has a ride ($(echo "$BODY" | field trip.id)); cancel it first"

REQUEST="$(RIDER="$RIDER" FROM="$FROM" TO="$TO" CLASS="$CLASS" FROM_NAME="$FROM_NAME" TO_NAME="$TO_NAME" python3 -c '
import json, os
e = os.environ
point = lambda v: {"latitude": float(v.split(",")[0]), "longitude": float(v.split(",")[1])}
print(json.dumps({"riderId": e["RIDER"], "pickup": point(e["FROM"]), "dropoff": point(e["TO"]),
  "pickupAddress": e["FROM_NAME"], "dropoffAddress": e["TO_NAME"],
  "vehicleClass": e["CLASS"], "paymentMethod": "cash"}))')"
api POST /v1/trips "$REQUEST"
[ "$STATUS" = 200 ] || die "the ride was refused ($STATUS): $BODY"
TRIP="$(echo "$BODY" | field trip.id)"
echo "==> the test rider ($PHONE) asked for a ride $FROM -> $TO: $TRIP"

trap 'api POST "/v1/trips/$TRIP:cancel" "{\"reason\":\"sim-ride stopped\"}"; echo; echo "==> cancelled ($STATUS)"; exit 0' INT TERM

LAST=""
while true; do
  api GET "/v1/trips/$TRIP"
  now="$(echo "$BODY" | python3 -c '
import json, sys
t = json.load(sys.stdin).get("trip", {})
parts = [t.get("status", "?").replace("TRIP_STATUS_", "").lower()]
if t.get("waitingForPreviousTrip"):
    parts.append("the captain is finishing a nearby ride first")
if t.get("expectedPickupAt"):
    parts.append("expected at the pickup " + t["expectedPickupAt"])
if t.get("arrivedAt"):
    parts.append("the captain is at the pickup")
print(", ".join(parts))' 2> /dev/null || echo "?")"
  if [ "$now" != "$LAST" ]; then
    echo "    $(date +%T) $now"
    LAST="$now"
  fi
  case "$now" in completed* | cancelled*) trap - INT TERM; exit 0 ;; esac
  sleep 3
done
