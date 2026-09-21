#!/usr/bin/env bash
# End-to-end test of driver approval, on the real services.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-driver-approval.sh
#
# What it proves:
#   1. a driver who registers through the gateway starts PENDING
#   2. a pending driver can be offline but cannot go available or busy (HTTP 400)
#   3. a driver cannot approve or reject themselves: there is no HTTP route, and the
#      gRPC methods answer PermissionDenied to a user token
#   4. an operator (the internal token) approves: the driver becomes ACTIVE and can go
#      online; approving twice is harmless; an ACTIVE driver cannot be "rejected"
#   5. a rejected driver stays offline, and can be approved after a second review
#   6. approving an unknown driver answers NotFound
#
# It registers two throw-away drivers (plates E2E-APPROVAL-1 and -2) and removes them
# again, before and after the run. The five real test drivers are never touched.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"

IDENTITY_1="a0000000-0000-4000-8000-0000000000d1"
IDENTITY_2="a0000000-0000-4000-8000-0000000000d2"
PLATE_1="E2E-APPROVAL-1"
PLATE_2="E2E-APPROVAL-2"
MISSING_ID="00000000-0000-4000-8000-00000000dead"

compose_port() { # <compose service name>
  awk -v svc="$1" '
    $0 ~ ("^  " svc ":$") {inside=1; next}
    inside && /^  [A-Za-z0-9_-]+:$/ {exit}
    inside && /GRPC_ADDRESS/ {gsub(/[^0-9]/, ""); print; exit}
  ' infrastructure/compose/compose.yaml
}

DRIVER_ADDR="localhost:$(compose_port driver-service)"
DRIVER_SVC="ride.driver.v1.DriverService"

FAILURES=0
BODY_FILE="$(mktemp)"

sql() { # <container> <query>: runs with the container's own credentials
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

remove_test_drivers() {
  sql ride-driver-postgres "delete from drivers where vehicle_plate_number in ('$PLATE_1','$PLATE_2');" > /dev/null 2>&1 || true
}

cleanup() {
  rm -f "$BODY_FILE"
  remove_test_drivers
}
trap cleanup EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1" "${@:2}"; }

json_field() { # <python expression over d> (reads stdin)
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

body_field() { # <python expression over d>: from the last HTTP response
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
    printf '  FAIL  %s -> expected %s, got %s: %s\n' "$1" "$2" "$actual" "$(head -c 200 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

expect_no_route() { # <label> <method> <path> <token>
  local actual
  actual="$(http "$2" "$3" "$4")"

  case "$actual" in
    404 | 405 | 501) printf '  ok    %s -> no route (%s)\n' "$1" "$actual" ;;
    *)
      printf '  FAIL  %s -> expected no route (404/405/501), got %s\n' "$1" "$actual"
      FAILURES=$((FAILURES + 1))
      ;;
  esac
}

check() { # <label> <expected> <actual>
  if [ "$2" = "$3" ]; then
    printf '  ok    %s -> %s\n' "$1" "$3"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "$3"
    FAILURES=$((FAILURES + 1))
  fi
}

grpc_code() { # <token> <method> <json>: prints OK or the gRPC code name
  local out
  out="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$3" "$DRIVER_ADDR" "$DRIVER_SVC/$2" 2>&1)" && {
    echo OK
    return 0
  }

  echo "$out" | sed -n 's/^ *Code: *//p' | head -1
}

operator() { # <method> <driver id>: the internal token, like another backend service
  grpc_code "$INTERNAL_TOKEN" "$1" "{\"driver_id\":\"$2\"}"
}

driver_status() { # <driver id>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" \
    -d "{\"driver_id\":\"$1\"}" "$DRIVER_ADDR" "$DRIVER_SVC/GetDriver" | json_field 'd["driver"]["status"]'
}

register() { # <identity> <plate> <name> <token>: prints the new driver id
  local status
  status="$(http POST /v1/drivers "$4" "{\"identityId\":\"$1\",\"displayName\":\"$3\",\"vehicle\":{\"make\":\"Toyota\",\"model\":\"Corolla\",\"color\":\"White\",\"plateNumber\":\"$2\",\"vehicleClass\":\"economy\"}}")"

  if [ "$status" != 200 ]; then
    echo "FAIL: registering $3 answered $status: $(head -c 200 "$BODY_FILE")" >&2
    exit 1
  fi

  body_field 'd["driver"]["id"]'
}

go_online() { echo '{"availabilityStatus":"AVAILABILITY_STATUS_AVAILABLE"}'; }
go_busy() { echo '{"availabilityStatus":"AVAILABILITY_STATUS_BUSY"}'; }
go_offline() { echo '{"availabilityStatus":"AVAILABILITY_STATUS_OFFLINE"}'; }

echo "==> [1/6] preparing: protoset, two throw-away driver identities"
buf build -o "$PROTOSET"
remove_test_drivers

TOKEN_1="$(mint "$IDENTITY_1")"
TOKEN_2="$(mint "$IDENTITY_2")"

echo "==> [2/6] a new driver starts pending and cannot go online"
DRIVER_1="$(register "$IDENTITY_1" "$PLATE_1" "Approval Test Driver One" "$TOKEN_1")"
[ -n "$DRIVER_1" ] || { echo "FAIL: no driver id came back" >&2; exit 1; }

expect "the driver reads their own profile" 200 GET "/v1/drivers/$DRIVER_1" "$TOKEN_1"
check "a new driver starts pending" DRIVER_STATUS_PENDING "$(body_field 'd["driver"]["status"]')"
expect "pending driver goes available" 400 PUT "/v1/drivers/$DRIVER_1/availability" "$TOKEN_1" "$(go_online)"
expect "pending driver goes busy" 400 PUT "/v1/drivers/$DRIVER_1/availability" "$TOKEN_1" "$(go_busy)"
expect "pending driver stays offline" 200 PUT "/v1/drivers/$DRIVER_1/availability" "$TOKEN_1" "$(go_offline)"
check "still pending" DRIVER_STATUS_PENDING "$(driver_status "$DRIVER_1")"

echo "==> [3/6] a driver cannot approve or reject themselves"
expect_no_route "approve over HTTP (colon form)" POST "/v1/drivers/$DRIVER_1:approve" "$TOKEN_1"
expect_no_route "approve over HTTP (path form)" POST "/v1/drivers/$DRIVER_1/approve" "$TOKEN_1"
expect_no_route "reject over HTTP" POST "/v1/drivers/$DRIVER_1/reject" "$TOKEN_1"
check "ApproveDriver with the driver's own token" PermissionDenied "$(grpc_code "$TOKEN_1" ApproveDriver "{\"driver_id\":\"$DRIVER_1\"}")"
check "RejectDriver with the driver's own token" PermissionDenied "$(grpc_code "$TOKEN_1" RejectDriver "{\"driver_id\":\"$DRIVER_1\"}")"
check "another driver approving driver 1" PermissionDenied "$(grpc_code "$TOKEN_2" ApproveDriver "{\"driver_id\":\"$DRIVER_1\"}")"
check "nobody approves without a token" Unauthenticated "$(grpc_code "" ApproveDriver "{\"driver_id\":\"$DRIVER_1\"}")"
check "still pending after every attempt" DRIVER_STATUS_PENDING "$(driver_status "$DRIVER_1")"

echo "==> [4/6] an operator approves"
check "operator approves" OK "$(operator ApproveDriver "$DRIVER_1")"
check "the driver is active" DRIVER_STATUS_ACTIVE "$(driver_status "$DRIVER_1")"
check "approving again is harmless" OK "$(operator ApproveDriver "$DRIVER_1")"
check "still active" DRIVER_STATUS_ACTIVE "$(driver_status "$DRIVER_1")"
expect "approved driver goes available" 200 PUT "/v1/drivers/$DRIVER_1/availability" "$TOKEN_1" "$(go_online)"
expect "approved driver goes offline" 200 PUT "/v1/drivers/$DRIVER_1/availability" "$TOKEN_1" "$(go_offline)"
check "an active driver cannot be rejected" FailedPrecondition "$(operator RejectDriver "$DRIVER_1")"
check "still active after the refused rejection" DRIVER_STATUS_ACTIVE "$(driver_status "$DRIVER_1")"

echo "==> [5/6] rejection, and a second review"
DRIVER_2="$(register "$IDENTITY_2" "$PLATE_2" "Approval Test Driver Two" "$TOKEN_2")"
[ -n "$DRIVER_2" ] || { echo "FAIL: no driver id came back" >&2; exit 1; }

check "operator rejects" OK "$(operator RejectDriver "$DRIVER_2")"
check "the driver is rejected" DRIVER_STATUS_REJECTED "$(driver_status "$DRIVER_2")"
check "rejecting again is harmless" OK "$(operator RejectDriver "$DRIVER_2")"
expect "rejected driver goes available" 400 PUT "/v1/drivers/$DRIVER_2/availability" "$TOKEN_2" "$(go_online)"
expect "rejected driver stays offline" 200 PUT "/v1/drivers/$DRIVER_2/availability" "$TOKEN_2" "$(go_offline)"
check "operator approves after a second review" OK "$(operator ApproveDriver "$DRIVER_2")"
check "the driver is active" DRIVER_STATUS_ACTIVE "$(driver_status "$DRIVER_2")"
expect "second-review driver goes available" 200 PUT "/v1/drivers/$DRIVER_2/availability" "$TOKEN_2" "$(go_online)"
expect "second-review driver goes offline" 200 PUT "/v1/drivers/$DRIVER_2/availability" "$TOKEN_2" "$(go_offline)"

echo "==> [6/6] unknown drivers"
check "approving an unknown driver" NotFound "$(operator ApproveDriver "$MISSING_ID")"
check "rejecting an unknown driver" NotFound "$(operator RejectDriver "$MISSING_ID")"
check "approving without an id" InvalidArgument "$(operator ApproveDriver "")"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: a driver starts pending, cannot go online or approve themselves, and an operator decides"
