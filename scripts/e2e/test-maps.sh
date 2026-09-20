#!/usr/bin/env bash
# End-to-end test of MAPS through the gateway: the route between two points, place
# search, and reverse geocoding, over the real self-hosted OSRM and Nominatim. Run from
# the ride-platform repo root:
#   bash scripts/e2e/test-maps.sh
#
# Routes need OSRM (already running). Place search and reverse geocoding need Nominatim
# to have finished importing (bash scripts/tools/nominatim.sh status); until then the
# test checks they answer 503 ("try again shortly") and skips the rest, and says so.
# Tokens are minted for local development only (scripts/tools/devtoken), never printed.
#
# What it proves:
#   1. every map route needs a signed-in user
#   2. a route between two points in Erbil: a real distance and time, and a line that
#      starts and ends where it should and is as long as the distance says
#   3. bad requests are refused with the right code: a missing point, a latitude of 91,
#      a point in the middle of the sea
#   4. (with Nominatim) a search finds Erbil places, near a point first, and refuses a
#      query of one character and a language we do not have; reverse geocoding names a
#      point in Erbil, and a point in the sea has nothing
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
OSRM_URL="${OSRM_URL:-http://127.0.0.1:5000}"
NOMINATIM_URL="${NOMINATIM_URL:-http://127.0.0.1:8088}"
RIDER_IDENTITY="a0000000-0000-4000-8000-0000000000a1"

FAILURES=0
BODY_FILE="$(mktemp)"
trap 'rm -f "$BODY_FILE"' EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1" "${@:2}"; }

json_field() { # <python expression over d> (reads stdin)
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

body_field() { json_field "$1" < "$BODY_FILE"; }

pass() { printf '  ok    %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

http() { # <method> <path> <token> [json body] -> the HTTP status; the body goes to $BODY_FILE
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
    printf '  FAIL  %s -> expected %s, got %s: %s\n' "$1" "$2" "$actual" "$(head -c 200 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

# The whole path of a route, checked against the distance it claims: <polyline> <distance in metres>
polyline_check() {
  python3 - "$1" "$2" "$3" "$4" "$5" "$6" <<'PYEND'
import math, sys

encoded, distance = sys.argv[1], float(sys.argv[2])
from_lat, from_lng, to_lat, to_lng = (float(x) for x in sys.argv[3:7])


def decode(polyline):
    points, index, lat, lng = [], 0, 0, 0
    while index < len(polyline):
        for axis in (0, 1):
            shift = result = 0
            while True:
                byte = ord(polyline[index]) - 63
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
    return points


def haversine(a, b):
    p1, p2 = math.radians(a[0]), math.radians(b[0])
    dp, dl = p2 - p1, math.radians(b[1] - a[1])
    h = math.sin(dp / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dl / 2) ** 2
    return 2 * 6371000 * math.asin(math.sqrt(h))


points = decode(encoded)
problems = []

if len(points) < 20:
    problems.append(f"only {len(points)} points: the full geometry was expected")

start_gap = haversine(points[0], (from_lat, from_lng))
end_gap = haversine(points[-1], (to_lat, to_lng))

if start_gap > 300:
    problems.append(f"the line starts {start_gap:.0f} m from the origin")

if end_gap > 300:
    problems.append(f"the line ends {end_gap:.0f} m from the destination")

length = sum(haversine(a, b) for a, b in zip(points, points[1:]))

if not 0.9 * distance <= length <= 1.1 * distance:
    problems.append(f"the line is {length:.0f} m long but the route says {distance:.0f} m")

if problems:
    print("; ".join(problems))
    sys.exit(1)

print(f"{len(points)} points, {length:.0f} m long, {start_gap:.0f} m and {end_gap:.0f} m from the ends")
PYEND
}

ERBIL_LAT=36.1911
ERBIL_LNG=44.0092
AIRPORT_LAT=36.2367
AIRPORT_LNG=43.9631

echo "==> [1/5] preparing"
curl -sS -o /dev/null --max-time 5 "$BASE/v1/zones" -H "Authorization: Bearer x" \
  || { echo "FAIL: the gateway is not answering on $BASE" >&2; exit 1; }

curl -fsS --max-time 5 "$OSRM_URL/route/v1/driving/$ERBIL_LNG,$ERBIL_LAT;$AIRPORT_LNG,$AIRPORT_LAT?overview=false" > /dev/null \
  || { echo "FAIL: OSRM does not answer on $OSRM_URL (docker compose up -d osrm; has infrastructure/osrm/prepare-osrm-data.sh been run?)" >&2; exit 1; }

TOKEN="$(mint "$RIDER_IDENTITY")"

NOMINATIM_READY=no
if curl -fsS --max-time 5 "$NOMINATIM_URL/status" > /dev/null 2>&1; then
  NOMINATIM_READY=yes
  echo "    Nominatim is ready: place search and reverse geocoding are tested"
else
  echo "    Nominatim is not ready (bash scripts/tools/nominatim.sh status): place search is checked to answer 503, and skipped"
fi

echo "==> [2/5] every map route needs a signed-in user"
expect "a route without a token"                       401 POST "/v1/routes:compute" "" "{\"origin\":{\"latitude\":$ERBIL_LAT,\"longitude\":$ERBIL_LNG},\"destination\":{\"latitude\":$AIRPORT_LAT,\"longitude\":$AIRPORT_LNG}}"
expect "a search without a token"                      401 GET  "/v1/places:search?query=erbil" ""
expect "a reverse lookup without a token"              401 GET  "/v1/places:reverse?coordinates.latitude=$ERBIL_LAT&coordinates.longitude=$ERBIL_LNG" ""

echo "==> [3/5] a route between two points in Erbil"
ROUTE_BODY="{\"origin\":{\"latitude\":$ERBIL_LAT,\"longitude\":$ERBIL_LNG},\"destination\":{\"latitude\":$AIRPORT_LAT,\"longitude\":$AIRPORT_LNG}}"
expect "the route"                                      200 POST "/v1/routes:compute" "$TOKEN" "$ROUTE_BODY"

DISTANCE="$(body_field 'd["distanceMeters"]')"
DURATION="$(body_field 'd["durationSeconds"]')"
POLYLINE="$(body_field 'd["polyline"]')"
echo "    $DISTANCE m, $DURATION s"

if python3 -c "import sys; sys.exit(0 if 4000 <= float('$DISTANCE') <= 20000 else 1)"; then pass "the distance is plausible for two points in Erbil"; else fail "the distance $DISTANCE m is not plausible"; fi
if python3 -c "import sys; sys.exit(0 if 100 <= float('$DURATION') <= 3600 else 1)"; then pass "the time is plausible"; else fail "the time $DURATION s is not plausible"; fi

if DETAIL="$(polyline_check "$POLYLINE" "$DISTANCE" "$ERBIL_LAT" "$ERBIL_LNG" "$AIRPORT_LAT" "$AIRPORT_LNG")"; then
  pass "the line to draw starts and ends where it should and is as long as the distance says ($DETAIL)"
else
  fail "the line to draw is wrong: $DETAIL"
fi

expect "and the way back"                               200 POST "/v1/routes:compute" "$TOKEN" "{\"origin\":{\"latitude\":$AIRPORT_LAT,\"longitude\":$AIRPORT_LNG},\"destination\":{\"latitude\":$ERBIL_LAT,\"longitude\":$ERBIL_LNG}}"

echo "==> [4/5] bad requests are refused with the right code"
expect "a route with no destination"                    400 POST "/v1/routes:compute" "$TOKEN" "{\"origin\":{\"latitude\":$ERBIL_LAT,\"longitude\":$ERBIL_LNG}}"
expect "a route with no points at all"                  400 POST "/v1/routes:compute" "$TOKEN" '{}'
expect "a latitude of 91"                               400 POST "/v1/routes:compute" "$TOKEN" "{\"origin\":{\"latitude\":91,\"longitude\":$ERBIL_LNG},\"destination\":{\"latitude\":$AIRPORT_LAT,\"longitude\":$AIRPORT_LNG}}"
expect "a longitude of 181"                             400 POST "/v1/routes:compute" "$TOKEN" "{\"origin\":{\"latitude\":$ERBIL_LAT,\"longitude\":181},\"destination\":{\"latitude\":$AIRPORT_LAT,\"longitude\":$AIRPORT_LNG}}"
expect "a point in the middle of the sea (0,0)"          400 POST "/v1/routes:compute" "$TOKEN" "{\"origin\":{\"latitude\":0,\"longitude\":0},\"destination\":{\"latitude\":$AIRPORT_LAT,\"longitude\":$AIRPORT_LNG}}"
expect "a search of one character"                      400 GET  "/v1/places:search?query=%D8%A3" "$TOKEN"
expect "a search in a language we do not have"          400 GET  "/v1/places:search?query=erbil&language=fr" "$TOKEN"
expect "a reverse lookup at latitude 91"                400 GET  "/v1/places:reverse?coordinates.latitude=91&coordinates.longitude=44" "$TOKEN"

echo "==> [5/5] place search and reverse geocoding"
if [ "$NOMINATIM_READY" = yes ]; then
  expect "a search for Erbil, near Erbil"               200 GET  "/v1/places:search?query=%D8%A3%D8%B1%D8%A8%D9%8A%D9%84&near.latitude=$ERBIL_LAT&near.longitude=$ERBIL_LNG&limit=3" "$TOKEN"

  if [ "$(body_field 'len(d.get("places", [])) >= 1')" = "True" ]; then pass "it finds at least one place"; else fail "it finds nothing for أربيل: $(head -c 300 "$BODY_FILE")"; fi
  if [ "$(body_field 'len(d.get("places", [])) <= 3')" = "True" ]; then pass "and no more than the limit"; else fail "it returns more than the limit of 3"; fi
  if [ "$(body_field 'all(p.get("id") and p.get("displayName") and "coordinates" in p for p in d.get("places", []))')" = "True" ]; then pass "every place has an id, a name and a position"; else fail "a place lacks an id, a name or a position: $(head -c 300 "$BODY_FILE")"; fi
  if [ "$(body_field 'abs(float(d["places"][0]["coordinates"].get("latitude", 0)) - '"$ERBIL_LAT"') < 1 and abs(float(d["places"][0]["coordinates"].get("longitude", 0)) - '"$ERBIL_LNG"') < 1')" = "True" ]; then pass "the first place is in the Erbil area"; else fail "the first place is not near Erbil: $(head -c 300 "$BODY_FILE")"; fi

  expect "the same search in English"                    200 GET  "/v1/places:search?query=Erbil%20Citadel&language=en&limit=2" "$TOKEN"
  echo "    first place: $(body_field 'd.get("places", [{}])[0].get("displayName", "(nothing)")')"

  expect "a search for something that is nowhere"        200 GET  "/v1/places:search?query=qqqzzzxxxwww" "$TOKEN"
  if [ "$(body_field 'len(d.get("places", [])) == 0')" = "True" ]; then pass "it finds nothing, as an empty list"; else fail "it found something for nonsense: $(head -c 300 "$BODY_FILE")"; fi

  expect "what is at the centre of Erbil"               200 GET  "/v1/places:reverse?coordinates.latitude=$ERBIL_LAT&coordinates.longitude=$ERBIL_LNG&language=en" "$TOKEN"
  if [ "$(body_field 'bool(d["place"]["displayName"])')" = "True" ]; then pass "it names the point: $(body_field 'd["place"]["displayName"]')"; else fail "it does not name the point: $(head -c 300 "$BODY_FILE")"; fi

  expect "what is in the middle of the sea (0,0)"       404 GET  "/v1/places:reverse?coordinates.latitude=0&coordinates.longitude=0" "$TOKEN"
else
  expect "a search while Nominatim is not ready"        503 GET  "/v1/places:search?query=erbil" "$TOKEN"
  if [ "$(body_field '"try again" in d.get("message", "")')" = "True" ] && [ "$(body_field '"172." not in d.get("message", "") and "dial" not in d.get("message", "")')" = "True" ]; then pass "the message says to try again and does not leak an internal address"; else fail "unexpected message: $(head -c 300 "$BODY_FILE")"; fi
  expect "a reverse lookup while Nominatim is not ready" 503 GET  "/v1/places:reverse?coordinates.latitude=$ERBIL_LAT&coordinates.longitude=$ERBIL_LNG" "$TOKEN"
  echo "    SKIPPED: the checks on what a search and a reverse lookup return (run this again once Nominatim is ready)"
fi

echo
if [ "$FAILURES" -eq 0 ]; then
  if [ "$NOMINATIM_READY" = yes ]; then
    echo "PASS: routes, place search and reverse geocoding work through the gateway"
  else
    echo "PASS (routes fully; place search only checked to answer 503 until Nominatim is ready)"
  fi
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
