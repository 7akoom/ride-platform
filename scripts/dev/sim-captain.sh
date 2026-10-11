#!/usr/bin/env bash
# Plays a captain on a STAGING instance, so the Rider app's trip can be tried from
# start to end without the Driver app. Run it on the server, from the repo root, and
# leave it running:
#   bash scripts/dev/sim-captain.sh [--class economy|comfort] [--wait 20] [--at 36.1911,44.0092]
#
# The first run makes the test captain (phone SIM_CAPTAIN_PHONE, +9647700000990 by
# default): it signs in with the code identity-service writes to its log on staging,
# registers with a car, and marks the car and placeholder papers approved straight in
# the database (no files). instance/sim-captain.env keeps it for the next runs.
# Then, until Ctrl+C:
#   - online, standing at --at (the middle of Erbil), or 1.5 km from a ride that waits
#     farther away, so it is the captain dispatch offers it to
#   - takes the ride, drives to the pickup along the road (the rider sees the car move),
#     says it has arrived, waits --wait seconds, starts, drives to the destination and completes
#   - stops following a ride the rider cancels
# It refuses to run unless instance/instance.env says IDENTITY_APP_ENV=test.
set -Euo pipefail
export LC_ALL=C # awk prints 36.19, never 36,19

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
[ "$(value IDENTITY_APP_ENV)" = test ] ||
  die "this instance is not staging (IDENTITY_APP_ENV=test); a test captain has no place on a real one"
command -v python3 > /dev/null || die "python3 is needed"

CLASS="economy"; WAIT=20; AT="36.1911,44.0092"; PHONE="${SIM_CAPTAIN_PHONE:-+9647700000990}"
while [ $# -gt 0 ]; do
  case "$1" in
    --class) CLASS="$2"; shift 2 ;;
    --wait) WAIT="$2"; shift 2 ;;
    --at) AT="$2"; shift 2 ;;
    *) die "unknown option $1" ;;
  esac
done
case "$CLASS" in economy | comfort) ;; *) die "--class is economy or comfort" ;; esac
echo "$WAIT" | grep -Eq '^[0-9]+$' || die "--wait is a number of seconds"
echo "$AT" | grep -Eq '^-?[0-9.]+,-?[0-9.]+$' || die "--at is lat,lng"

API="http://127.0.0.1:$(value GATEWAY_PORT)"
[ "$API" != "http://127.0.0.1:" ] || API="http://127.0.0.1:18080"
STATE="instance/sim-captain.env"
TOKEN=""; REFRESH=""; IDENTITY=""; DRIVER=""; STATUS=""; BODY=""
LAT="${AT%,*}"; LNG="${AT#*,}"

field() { # <path, e.g. trip.pickup.latitude>: from the JSON on stdin; empty when missing
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

save() { (umask 077 && printf 'DRIVER=%s\nREFRESH=%s\n' "$DRIVER" "$REFRESH" > "$STATE"); }

login() { # signs in as the test captain with the code from identity-service's log
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
  call POST /v1/auth/otp:verify "{\"challengeId\":\"$challenge\",\"code\":\"$code\",\"clientId\":\"driver-app\",\"deviceId\":\"sim-captain\",\"deviceName\":\"sim-captain\",\"platform\":\"android\",\"appVersion\":\"1.0.0\"}"
  [ "$STATUS" = 200 ] || die "the login code was refused ($STATUS): $BODY"
  TOKEN="$(echo "$BODY" | field accessToken)"
  REFRESH="$(echo "$BODY" | field refreshToken)"
  IDENTITY="$(echo "$BODY" | field identityId)"
}

renew() { # a new token from the refresh token, or signing in again
  TOKEN=""
  if [ -n "$REFRESH" ]; then
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

db() { # <DRIVER|TRIP>: runs the SQL on stdin in that service's database
  docker exec -i ride-postgres sh -c "psql -U ride_admin -d \"\$$1_DB_NAME\" -t -A -q -v ON_ERROR_STOP=1"
}

setup() { # the test captain: signed up, with an approved car and papers
  login
  call GET "/v1/identities/$IDENTITY/driver"
  if [ "$STATUS" != 200 ]; then
    call POST /v1/drivers "{\"identityId\":\"$IDENTITY\",\"displayName\":\"Test Captain\",\"vehicle\":{\"make\":\"Toyota\",\"model\":\"Corolla\",\"color\":\"White\",\"plateNumber\":\"22 A 99990\",\"year\":2020,\"vehicleClass\":\"$CLASS\"}}"
    [ "$STATUS" = 200 ] || die "could not register the test captain ($STATUS): $BODY"
  fi
  DRIVER="$(echo "$BODY" | field driver.id)"
  echo "$DRIVER" | grep -Eq '^[0-9a-f-]{36}$' || die "no driver id in: $BODY"
  db DRIVER << SQL || die "could not approve the test captain in the database"
update vehicles set status = 'approved', reviewed_at = now(), updated_at = now()
 where driver_id = '$DRIVER' and active;
insert into driver_documents
  (id, driver_id, type_code, media_id, document_number, expires_on, status, reviewed_at, vehicle_id)
select gen_random_uuid(), '$DRIVER', t.code, gen_random_uuid(),
       case when t.requires_number then 'SIM-' || left(md5('$DRIVER' || t.code), 12) else '' end,
       case when t.requires_expiry then current_date + 365 end,
       'approved', now(), car.id
  from driver_document_types t
  left join vehicles car on t.scope = 'vehicle' and car.driver_id = '$DRIVER' and car.active
 where t.active and t.required
   and not exists (select 1 from driver_documents d
                    where d.driver_id = '$DRIVER' and d.type_code = t.code and d.status = 'approved'
                      and d.vehicle_id is not distinct from car.id)
on conflict do nothing;
update drivers set status = 'active', updated_at = now() where id = '$DRIVER';
SQL
  save
  echo "==> the test captain is ready ($PHONE, driver $DRIVER)"
}

report() { # <lat> <lng>: where the captain is now
  LAT="$1"; LNG="$2"
  api PUT "/v1/locations/$DRIVER" "{\"entityType\":\"ENTITY_TYPE_DRIVER\",\"coordinates\":{\"latitude\":$1,\"longitude\":$2}}"
}

availability() { # <OFFLINE|AVAILABLE>
  api PUT "/v1/drivers/$DRIVER/availability" "{\"availabilityStatus\":\"AVAILABILITY_STATUS_$1\"}"
}

T_STATUS=""; P_LAT=""; P_LNG=""; D_LAT=""; D_LNG=""
load_trip() { # <trip id>: its status and ends
  api GET "/v1/trips/$1"
  T_STATUS="$(echo "$BODY" | field trip.status)"
  P_LAT="$(echo "$BODY" | field trip.pickup.latitude)"; P_LNG="$(echo "$BODY" | field trip.pickup.longitude)"
  D_LAT="$(echo "$BODY" | field trip.dropoff.latitude)"; D_LNG="$(echo "$BODY" | field trip.dropoff.longitude)"
}

ROAD_PY='
import json, math, sys
try:
    encoded = json.load(sys.stdin)["polyline"]
except Exception:
    sys.exit(0)
points, index, lat, lng = [], 0, 0, 0
while index < len(encoded):
    for axis in (0, 1):
        shift = result = 0
        while True:
            byte = ord(encoded[index]) - 63
            index += 1
            result |= (byte & 0x1F) << shift
            shift += 5
            if byte < 0x20:
                break
        delta = ~(result >> 1) if result & 1 else result >> 1
        if axis == 0:
            lat += delta
        else:
            lng += delta
    points.append((lat / 1e5, lng / 1e5))
def metres(a, b):
    dy = (a[0] - b[0]) * 111000
    dx = (a[1] - b[1]) * 111000 * math.cos(math.radians(a[0]))
    return math.hypot(dx, dy)
total = sum(metres(points[i], points[i + 1]) for i in range(len(points) - 1))
step = max(30.0, total / 60)
out, travelled, goal = [], 0.0, step
for i in range(len(points) - 1):
    a, b = points[i], points[i + 1]
    length = metres(a, b)
    while length > 0 and travelled + length >= goal:
        t = (goal - travelled) / length
        out.append((a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t))
        goal += step
    travelled += length
out.append(points[-1])
for p in out:
    print("%.6f %.6f" % p)
'

drive() { # <trip> <to lat> <to lng>: along the road, a position every 2 s; fails when the ride ended
  local path i=0 lat lng
  api POST /v1/routes:compute "{\"origin\":{\"latitude\":$LAT,\"longitude\":$LNG},\"destination\":{\"latitude\":$2,\"longitude\":$3}}"
  path="$(echo "$BODY" | python3 -c "$ROAD_PY")"
  if [ -z "$path" ]; then
    # No road found: a straight line, so the trip still goes on.
    path="$(awk -v a="$LAT" -v b="$2" -v c="$LNG" -v d="$3" 'BEGIN {
      for (i = 1; i <= 20; i++) printf "%.6f %.6f\n", a + (b - a) * i / 20, c + (d - c) * i / 20 }')"
  fi
  while read -r lat lng; do
    i=$((i + 1))
    report "$lat" "$lng"
    sleep 2
    if [ $((i % 5)) = 0 ]; then
      load_trip "$1"
      case "$T_STATUS" in TRIP_STATUS_ACCEPTED | TRIP_STATUS_IN_PROGRESS) ;; *) return 1 ;; esac
    fi
  done <<< "$path"
  report "$2" "$3"
}

run_trip() { # <trip id>: follows it to its end
  local trip="$1" i
  load_trip "$trip"
  if [ "$T_STATUS" = TRIP_STATUS_ACCEPTED ]; then
    echo "==> ride $trip: driving to the pickup"
    drive "$trip" "$P_LAT" "$P_LNG" || { echo "==> the ride ended ($T_STATUS)"; return; }
    api POST "/v1/trips/$trip:arrived" '{}'
    echo "==> at the pickup (arrived: $STATUS); waiting ${WAIT}s"
    for i in $(seq 1 $((WAIT / 2))); do report "$P_LAT" "$P_LNG"; sleep 2; done
    load_trip "$trip"
    [ "$T_STATUS" = TRIP_STATUS_ACCEPTED ] || { echo "==> the ride ended ($T_STATUS)"; return; }
    api POST "/v1/trips/$trip:start" '{}'
    echo "==> started ($STATUS)"
    load_trip "$trip"
  fi
  if [ "$T_STATUS" = TRIP_STATUS_IN_PROGRESS ]; then
    echo "==> driving to the destination"
    drive "$trip" "$D_LAT" "$D_LNG" || { echo "==> the ride ended ($T_STATUS)"; return; }
    api POST "/v1/trips/$trip:complete" '{}'
    echo "==> completed ($STATUS). Waiting for the next ride."
  fi
}

far() { # <lat> <lng>: more than 4 km from where the captain is
  awk -v a="$LAT" -v b="$LNG" -v c="$1" -v d="$2" 'BEGIN {
    dy = (a - c) * 111000; dx = (b - d) * 111000 * cos(a * 3.14159 / 180)
    exit !(dx * dx + dy * dy > 4000 * 4000) }'
}

if [ -s "$STATE" ]; then
  DRIVER="$(sed -n 's/^DRIVER=//p' "$STATE")"
  REFRESH="$(sed -n 's/^REFRESH=//p' "$STATE")"
  renew
else
  setup
fi
echo "UPDATE vehicles SET vehicle_class = '$CLASS' WHERE driver_id = '$DRIVER' AND active;" | db DRIVER ||
  die "could not set the car's class"

trap 'availability OFFLINE; echo; echo "==> offline"; exit 0' INT TERM
report "$LAT" "$LNG"
availability AVAILABLE
[ "$STATUS" = 200 ] || die "could not go online ($STATUS): $BODY"
echo "==> the test captain ($CLASS) is online at $LAT,$LNG. Order a $CLASS ride in the Rider app. Ctrl+C to stop."

while true; do
  api GET "/v1/drivers/$DRIVER/offer"
  if [ "$STATUS" = 200 ]; then
    trip="$(echo "$BODY" | field offer.tripId)"
    api POST "/v1/trips/$trip:accept-offer" "{\"driverId\":\"$DRIVER\"}"
    echo "==> offered ride $trip: accepted ($STATUS)"
    [ "$STATUS" = 200 ] && run_trip "$trip"
    availability AVAILABLE
    continue
  fi

  api GET "/v1/trips:active?driver_id=$DRIVER"
  if [ "$STATUS" = 200 ]; then
    run_trip "$(echo "$BODY" | field trip.id)"
    availability AVAILABLE
    continue
  fi

  # A ride waiting farther away than dispatch looks: stand 1.5 km from its pickup.
  waiting="$(echo "select pickup_latitude || ',' || pickup_longitude from trips
                    where status = 'requested' and driver_id is null order by created_at desc limit 1;" | db TRIP || true)"
  if [ -n "$waiting" ] && far "${waiting%,*}" "${waiting#*,}"; then
    report "$(awk -v a="${waiting%,*}" 'BEGIN { printf "%.6f", a + 0.0135 }')" "${waiting#*,}"
  else
    report "$LAT" "$LNG"
  fi
  sleep 2
done
