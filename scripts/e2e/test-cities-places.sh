#!/usr/bin/env bash
# End-to-end test of cities, zones inside them and curated places, on the real
# services, through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-cities-places.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken). Nominatim does not need to be running: search
# still answers with curated places when the map search is down.
#
# What it proves:
#   1. staff with zones.manage create and change cities (the time zone must be
#      a real IANA zone, a name is used once); users only read active cities
#   2. a zone belongs to a city; a served point reports the city and its time
#      zone; switching the city off stops serving its zones
#   3. staff with places.manage curate places; users list and read active ones,
#      by city and category, a page at a time
#   4. search lists curated places first, in any language, named in the one
#      asked for
#   5. an inactive place disappears for users, and every staff change is in the
#      audit log
#
# It creates throw-away cities (far from any real one), zones, places and one
# staff member, and removes them again. Audit entries stay (append-only).
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
STAFF_IDENTITY="$(uuid)"
STAFF_ID="$(uuid)"
CITY="E2E Geo City $RUN"
AIRPORT="E2E Geo Airport $RUN"
MALL="E2E Geo Mall $RUN"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  sql ride-staff-postgres "delete from staff_members where id = '$STAFF_ID';" > /dev/null 2>&1 || true
  sql ride-location-postgres "
    delete from curated_places where city_id in (select id from cities where name like 'E2E Geo City%');
    delete from zones where city_id in (select id from cities where name like 'E2E Geo City%');
    delete from cities where name like 'E2E Geo City%';" > /dev/null 2>&1 || true
}
trap cleanup EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1"; }

body_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE"
}

urlencode() { python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1]))' "$1"; }

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

RIDER="$(mint "$RIDER_IDENTITY")"

sql ride-staff-postgres "
  insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
  values ('$STAFF_ID', '$STAFF_IDENTITY', 'e2e-geo-$RUN@ride.test', 'E2E Geo', 'active', now(), now());
  insert into staff_member_roles (staff_id, role_id) values ('$STAFF_ID', '$OPERATIONS_ROLE');" > /dev/null
STAFF="$(mint "$STAFF_IDENTITY")"

CITY_BODY="{\"name\":\"$CITY\",\"names\":{\"ar\":\"مدينة الاختبار $RUN\",\"ku\":\"شاری تاقیکردنەوە $RUN\"},\"timeZone\":\"Asia/Baghdad\",\"center\":{\"latitude\":10.05,\"longitude\":10.05}}"

echo "==> [1/5] cities"
expect "a rider creates a city" 403 POST /v1/admin/cities "$RIDER" "$CITY_BODY"
expect "a made-up time zone" 400 POST /v1/admin/cities "$STAFF" "${CITY_BODY/Asia\/Baghdad/Mars\/Olympus}"
expect "a language that is not ar, ku or en" 400 POST /v1/admin/cities "$STAFF" "{\"name\":\"$CITY\",\"names\":{\"fr\":\"x\"},\"timeZone\":\"UTC\",\"center\":{\"latitude\":10,\"longitude\":10}}"
expect "the operator creates a city" 200 POST /v1/admin/cities "$STAFF" "$CITY_BODY"
CITY_ID="$(body_field 'd["city"]["id"]')"
check "active, with its time zone" "True Asia/Baghdad" "$(body_field 'str(d["city"]["active"]) + " " + d["city"]["timeZone"]')"
expect "the same name again" 409 POST /v1/admin/cities "$STAFF" "${CITY_BODY/$CITY/${CITY^^}}"
expect "a rider lists cities" 200 GET /v1/cities "$RIDER"
check "the city is listed" True "$(body_field "any(c['id'] == '$CITY_ID' for c in d['cities'])")"
expect "the operator renames it in English" 200 PATCH "/v1/admin/cities/$CITY_ID" "$STAFF" "${CITY_BODY/\"ku\"/\"en\":\"Test City\",\"ku\"}"
check "the English name is there" "Test City" "$(body_field 'd["city"]["names"].get("en", "")')"

echo "==> [2/5] zones in the city"
expect "a zone in a city that does not exist" 404 POST /v1/admin/zones "$STAFF" "{\"cityId\":\"$(uuid)\",\"name\":\"X\",\"boundary\":[{\"latitude\":10,\"longitude\":10},{\"latitude\":10,\"longitude\":10.1},{\"latitude\":10.1,\"longitude\":10.05}]}"
expect "the operator creates a zone" 200 POST /v1/admin/zones "$STAFF" "{\"cityId\":\"$CITY_ID\",\"name\":\"E2E Center\",\"boundary\":[{\"latitude\":10,\"longitude\":10},{\"latitude\":10,\"longitude\":10.1},{\"latitude\":10.1,\"longitude\":10.1},{\"latitude\":10.1,\"longitude\":10}]}"
check "the zone names its city" "$CITY_ID $CITY" "$(body_field 'd["zone"]["cityId"] + " " + d["zone"]["city"]')"
expect "a point inside" 200 GET "/v1/zones:check?coordinates.latitude=10.05&coordinates.longitude=10.05" "$RIDER"
check "served, in the city, with its time zone" "True $CITY_ID Asia/Baghdad" "$(body_field 'str(d["served"]) + " " + d.get("cityId", "") + " " + d.get("timeZone", "")')"
expect "the zones of the city" 200 GET "/v1/zones?city_id=$CITY_ID" "$RIDER"
check "one zone" 1 "$(body_field 'len(d["zones"])')"
expect "the operator switches the city off" 200 POST "/v1/admin/cities/$CITY_ID:setActive" "$STAFF" '{"active":false}'
expect "the point again" 200 GET "/v1/zones:check?coordinates.latitude=10.05&coordinates.longitude=10.05" "$RIDER"
check "no longer served" False "$(body_field 'str(d["served"])')"
expect "a rider reads the inactive city" 404 GET "/v1/cities/$CITY_ID" "$RIDER"
expect "the admin list still has it" 200 GET /v1/admin/cities "$STAFF"
check "listed, inactive" False "$(body_field "[c for c in d['cities'] if c['id'] == '$CITY_ID'][0]['active']")"
expect "the operator switches it on" 200 POST "/v1/admin/cities/$CITY_ID:setActive" "$STAFF" '{"active":true}'

echo "==> [3/5] curated places"
PLACE_BODY="{\"cityId\":\"$CITY_ID\",\"category\":\"PLACE_CATEGORY_AIRPORT\",\"name\":\"$AIRPORT\",\"names\":{\"ar\":\"مطار الاختبار $RUN\"},\"address\":\"Departures, gate 2\",\"coordinates\":{\"latitude\":10.02,\"longitude\":10.03},\"priority\":100}"
expect "a rider creates a place" 403 POST /v1/admin/places "$RIDER" "$PLACE_BODY"
expect "a place without a category" 400 POST /v1/admin/places "$STAFF" "${PLACE_BODY/PLACE_CATEGORY_AIRPORT/PLACE_CATEGORY_UNSPECIFIED}"
expect "a place in a city that does not exist" 404 POST /v1/admin/places "$STAFF" "${PLACE_BODY/$CITY_ID/$(uuid)}"
expect "the operator adds an airport" 200 POST /v1/admin/places "$STAFF" "$PLACE_BODY"
AIRPORT_ID="$(body_field 'd["place"]["id"]')"
expect "the operator adds a mall" 200 POST /v1/admin/places "$STAFF" "{\"cityId\":\"$CITY_ID\",\"category\":\"PLACE_CATEGORY_MALL\",\"name\":\"$MALL\",\"coordinates\":{\"latitude\":10.06,\"longitude\":10.06},\"priority\":10}"
MALL_ID="$(body_field 'd["place"]["id"]')"
expect "a rider lists the city's places" 200 GET "/v1/places?city_id=$CITY_ID" "$RIDER"
check "the airport first (priority)" "$AIRPORT_ID,$MALL_ID" "$(body_field '",".join(p["id"] for p in d["places"])')"
expect "a page of one" 200 GET "/v1/places?city_id=$CITY_ID&page_size=1" "$RIDER"
TOKEN="$(body_field 'd.get("nextPageToken", "")')"
expect "the next page" 200 GET "/v1/places?city_id=$CITY_ID&page_size=1&page_token=$TOKEN" "$RIDER"
check "the mall" "$MALL_ID" "$(body_field '",".join(p["id"] for p in d["places"])')"
expect "only malls" 200 GET "/v1/places?city_id=$CITY_ID&category=PLACE_CATEGORY_MALL" "$RIDER"
check "one mall" "$MALL_ID" "$(body_field '",".join(p["id"] for p in d["places"])')"
expect "a bad page token" 400 GET "/v1/places?page_token=nonsense" "$RIDER"
expect "a rider reads the airport" 200 GET "/v1/places/$AIRPORT_ID" "$RIDER"
check "with its exact point" "10.02 10.03" "$(body_field 'str(d["place"]["coordinates"]["latitude"]) + " " + str(d["place"]["coordinates"]["longitude"])')"

echo "==> [4/5] search"
expect "search by the English name" 200 GET "/v1/places:search?query=$(urlencode "$AIRPORT")&language=en" "$RIDER"
check "the curated airport first" "curated/$AIRPORT_ID $AIRPORT_ID airport" "$(body_field 'd["places"][0]["id"] + " " + d["places"][0].get("curatedPlaceId", "") + " " + d["places"][0]["category"]')"
expect "search in Arabic" 200 GET "/v1/places:search?query=$(urlencode "مطار الاختبار $RUN")&language=ar" "$RIDER"
check "named in Arabic" "مطار الاختبار $RUN" "$(body_field 'd["places"][0]["name"]')"
check "with the address and the city" True "$(body_field "'Departures, gate 2' in d['places'][0]['displayName']")"

echo "==> [5/5] switching a place off, and the audit log"
expect "the operator switches the mall off" 200 POST "/v1/admin/places/$MALL_ID:setActive" "$STAFF" '{"active":false}'
expect "a rider reads it" 404 GET "/v1/places/$MALL_ID" "$RIDER"
expect "search this run's places" 200 GET "/v1/places:search?query=$RUN" "$RIDER"
check "the airport is found" True "$(body_field "any(p.get('curatedPlaceId') == '$AIRPORT_ID' for p in d.get('places', []))")"
check "the mall is not" False "$(body_field "any(p.get('curatedPlaceId') == '$MALL_ID' for p in d.get('places', []))")"
expect "the admin list still has it" 200 GET "/v1/admin/places?city_id=$CITY_ID" "$STAFF"
check "both places" 2 "$(body_field 'len(d["places"])')"
expect "a rider reads the admin list" 403 GET "/v1/admin/places" "$RIDER"
check "place changes are audited" True "$(sql ride-staff-postgres "select count(*) >= 3 from audit_entries where actor_staff_id = '$STAFF_ID' and permission = 'places.manage' and decision = 'allowed';" | sed 's/^t$/True/; s/^f$/False/')"
check "city changes are audited" True "$(sql ride-staff-postgres "select count(*) >= 4 from audit_entries where actor_staff_id = '$STAFF_ID' and permission = 'zones.manage' and decision = 'allowed';" | sed 's/^t$/True/; s/^f$/False/')"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: cities, their zones and curated places are managed by staff and read by everyone"
