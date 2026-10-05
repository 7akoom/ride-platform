#!/usr/bin/env bash
# End-to-end test of the business reports (analytics-service), on the real
# services, through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-analytics.sh
#
# Needs the local identity signing key, analytics-service with migration
# 00003, staff-service knowing analytics.read, trip-service writing the
# trip's city, zone and class on trip.requested, and the gateway routing
# /v1/analytics.
#
# What it proves:
#   1. only staff with analytics.read (the owner) read reports; an operator,
#      a rider and nobody at all are refused; every read is in the audit log
#   2. real trips reach the reports through the events: the trip's city,
#      zone, class and payment are kept, and only ids (no names, no places)
#   3. a report about a city counts that city's trips on its clock; zone and
#      class narrow it; cancellations say at what stage and by whom
#   4. bad ranges and unknown cities are refused
#
# It creates a throw-away city and zone (far from any real one), a rider and
# two staff members, and removes them again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OPS_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
OPS_STAFF_ID="$(uuid)"
CITY="E2E Analytics City $RUN"
UNKNOWN="$(uuid)"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
CITY_ID=""
ZONE_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  sql ride-staff-postgres "delete from staff_members where id in ('$OWNER_STAFF_ID', '$OPS_STAFF_ID');" > /dev/null 2>&1 || true
  if [ -n "$RIDER_ID" ]; then
    sql ride-trip-postgres "
      update trips set status = 'cancelled', cancelled_at = now()
       where rider_id = '$RIDER_ID' and status in ('requested', 'accepted', 'in_progress');" > /dev/null 2>&1 || true
    sql ride-pricing-postgres "delete from rider_trip_stats where rider_id = '$RIDER_ID';" > /dev/null 2>&1 || true
    sql ride-rider-postgres "delete from riders where id = '$RIDER_ID';" > /dev/null 2>&1 || true
    sql ride-analytics-postgres "
      delete from trip_facts where rider_id = '$RIDER_ID';
      delete from rider_signups where rider_id = '$RIDER_ID';" > /dev/null 2>&1 || true
  fi
  sql ride-location-postgres "
    delete from zones where city_id in (select id from cities where name like 'E2E Analytics City%');
    delete from cities where name like 'E2E Analytics City%';" > /dev/null 2>&1 || true
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

add_staff() { # <staff id> <identity> <role>
  sql ride-staff-postgres "
    insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
    values ('$1', '$2', 'e2e-analytics-$1@ride.test', 'E2E Analytics', 'active', now(), now());
    insert into staff_member_roles (staff_id, role_id) values ('$1', '$3');" > /dev/null
}

PICKUP='{"latitude":27.05,"longitude":27.05}'
DROPOFF='{"latitude":27.07,"longitude":27.07}'

sql ride-analytics-postgres "select to_regclass('public.trip_facts') is not null;" | grep -q t \
  || { echo "ABORT: the trip_facts table does not exist: apply analytics migration 00003 (goose up)" >&2; exit 2; }

RIDER="$(mint "$RIDER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"
OPS="$(mint "$OPS_IDENTITY")"

echo "==> [0/4] a served zone, a rider, the owner and an operator"
add_staff "$OWNER_STAFF_ID" "$OWNER_IDENTITY" "$OWNER_ROLE"
add_staff "$OPS_STAFF_ID" "$OPS_IDENTITY" "$OPERATIONS_ROLE"
expect "a city" 200 POST /v1/admin/cities "$OWNER" "{\"name\":\"$CITY\",\"timeZone\":\"Asia/Baghdad\",\"center\":$PICKUP}"
CITY_ID="$(body_field 'd["city"]["id"]')"
expect "a zone" 200 POST /v1/admin/zones "$OWNER" "{\"cityId\":\"$CITY_ID\",\"name\":\"E2E\",\"boundary\":[{\"latitude\":27,\"longitude\":27},{\"latitude\":27,\"longitude\":27.1},{\"latitude\":27.1,\"longitude\":27.1},{\"latitude\":27.1,\"longitude\":27}]}"
ZONE_ID="$(body_field 'd["zone"]["id"]')"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Analytics Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"

echo "==> [1/4] two trips, both cancelled by the rider before a driver came"
expect "an economy trip" 200 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"pickupAddress\":\"Home\",\"dropoffAddress\":\"Mall\"}"
ECONOMY="$(body_field 'd["trip"]["id"]')"
expect "the rider cancels it" 200 POST "/v1/trips/$ECONOMY:cancel" "$RIDER" '{"reason":"e2e"}'
expect "a comfort trip" 200 POST /v1/trips "$RIDER" "{\"riderId\":\"$RIDER_ID\",\"pickup\":$PICKUP,\"dropoff\":$DROPOFF,\"vehicleClass\":\"comfort\",\"paymentMethod\":\"wallet\"}"
COMFORT="$(body_field 'd["trip"]["id"]')"
expect "the rider cancels it" 200 POST "/v1/trips/$COMFORT:cancel" "$RIDER" '{"reason":"e2e"}'

FUNNEL="/v1/analytics/trip-funnel?scope.city_id=$CITY_ID"
for _ in $(seq 1 30); do
  if [ "$(http GET "$FUNNEL" "$OWNER")" = 200 ] && [ "$(body_field 'd["totals"].get("cancelledCount", "0")')" = 2 ]; then
    break
  fi
  sleep 1
done

echo "==> [2/4] who may read reports"
expect "the owner reads the funnel" 200 GET "$FUNNEL" "$OWNER"
expect "an operator" 403 GET "$FUNNEL" "$OPS"
expect "a rider" 403 GET "$FUNNEL" "$RIDER"
expect "nobody" 401 GET "$FUNNEL" ""
expect "an operator, revenue" 403 GET /v1/analytics/revenue "$OPS"
check "the owner's reads are audited" t \
  "$(sql ride-staff-postgres "select count(*) > 0 from audit_entries where actor_identity_id = '$OWNER_IDENTITY' and permission = 'analytics.read' and target_id = '$CITY_ID' and decision = 'allowed' and outcome = 'succeeded';")"
check "the operator's attempts too" t \
  "$(sql ride-staff-postgres "select count(*) > 0 from audit_entries where actor_identity_id = '$OPS_IDENTITY' and permission = 'analytics.read' and decision = 'denied';")"

echo "==> [3/4] the city's trips on its clock"
expect "the funnel" 200 GET "$FUNNEL" "$OWNER"
TODAY="$(TZ=Asia/Baghdad date +%F)"
check "two requested, two cancelled, nothing else" "2 2 0 0 0" \
  "$(body_field '" ".join(str(d["totals"].get(k, "0")) for k in ("requestedCount", "cancelledCount", "acceptedCount", "startedCount", "completedCount"))')"
check "Baghdad's clock, 30 days to today" "Asia/Baghdad 30 $TODAY" "$(body_field 'd["timeZone"] + " " + str(len(d["days"])) + " " + d["toDate"]')"
check "today holds both" 2 "$(body_field 'd["days"][-1].get("requestedCount", "0")')"
expect "comfort only" 200 GET "$FUNNEL&scope.vehicle_class=comfort" "$OWNER"
check "one comfort trip" 1 "$(body_field 'd["totals"].get("requestedCount", "0")')"
expect "the zone" 200 GET "$FUNNEL&scope.zone_id=$ZONE_ID" "$OWNER"
check "both in the zone" 2 "$(body_field 'd["totals"].get("requestedCount", "0")')"
expect "another zone" 200 GET "$FUNNEL&scope.zone_id=$UNKNOWN" "$OWNER"
check "none there" 0 "$(body_field 'd["totals"].get("requestedCount", "0")')"
expect "cancellations" 200 GET "/v1/analytics/cancellations?scope.city_id=$CITY_ID&range.from_date=$TODAY&range.to_date=$TODAY" "$OWNER"
check "all before a driver, by the rider, 100%" "2 2 requested:2 rider:2 100.00" \
  "$(body_field '" ".join([d["totalTrips"], d["totalCancellations"], ",".join(s["stage"] + ":" + s["count"] for s in d["byStage"] if s.get("count", "0") != "0"), ",".join(b["cancelledBy"] + ":" + b["count"] for b in d["byCancelledBy"]), d["cancellationRatePercent"]])')"
expect "revenue" 200 GET "/v1/analytics/revenue?scope.city_id=$CITY_ID" "$OWNER"
check "no fares for cancelled requests" "0 0" "$(body_field 'd["grossFareTotal"] + " " + str(d.get("totalTrips", "0"))')"
expect "rider retention" 200 GET /v1/analytics/retention/riders "$OWNER"
expect "driver retention" 200 GET "/v1/analytics/retention/drivers?cohort_weeks=4" "$OWNER"
check "what the trips left in analytics" "$CITY_ID|$ZONE_ID|comfort|wallet|f|requested|rider" \
  "$(sql ride-analytics-postgres "select concat_ws('|', city_id, zone_id, vehicle_class, payment_method, scheduled, cancel_stage, cancelled_by) from trip_facts where trip_id = '$COMFORT';")"
check "the rider's signup is counted" 1 "$(sql ride-analytics-postgres "select count(*) from rider_signups where rider_id = '$RIDER_ID';")"
check "no event payloads are kept" 0 \
  "$(sql ride-analytics-postgres "select count(*) from information_schema.columns where table_schema = 'public' and column_name = 'payload';")"

echo "==> [4/4] bad requests"
expect "not a date" 400 GET "/v1/analytics/trip-funnel?range.from_date=05-10-2026" "$OWNER"
expect "backwards" 400 GET "/v1/analytics/trip-funnel?range.from_date=2026-10-05&range.to_date=2026-10-01" "$OWNER"
expect "more than a year" 400 GET "/v1/analytics/revenue?range.from_date=2024-01-01&range.to_date=2026-01-01" "$OWNER"
expect "an unknown city" 400 GET "/v1/analytics/cancellations?scope.city_id=$UNKNOWN" "$OWNER"
expect "a city that is not an id" 400 GET "/v1/analytics/cancellations?scope.city_id=erbil" "$OWNER"
expect "too many cohort weeks" 400 GET "/v1/analytics/retention/riders?cohort_weeks=60" "$OWNER"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: only the owner reads reports (audited); real trips reach them with their city, zone and class and nothing personal; a city's report runs on its clock and narrows by zone and class; bad ranges and cities are refused"
