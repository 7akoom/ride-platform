#!/usr/bin/env bash
# End-to-end test of staff accounts, permissions and the audit log, on the real
# services, through the gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-staff-access.sh
#
# Needs STAFF_BOOTSTRAP_OWNER_EMAIL in services/staff-service/.env, and
# identity-service in development mode (the OTP codes are read from its log).
# The owner logs in with an email OTP; identity allows one code per address a
# minute, so wait a minute between two runs.
#
# What it proves:
#   1. the first owner signs in and accepts the invitation; a user who is not
#      staff gets 404 on /v1/staff/me and 403 on every admin route
#   2. the owner invites an operator; the operator signs in and accepts, and
#      holds exactly the operations permissions
#   3. the operator reviews drivers: the pending queue, reject (a reason is
#      required and shown to the driver), approve; a driver cannot approve
#   4. the operator changes a zone; the operator cannot manage staff or read
#      the audit log
#   5. every attempt is in the audit log, allowed or denied, with its outcome
#   6. suspension takes effect at once, nobody suspends themselves, an
#      invitation can be revoked and the address invited again
#   7. AuthorizeStaffAction refuses a user token (internal only)
#
# It creates throw-away staff (e2e-*@ride.test), one driver and one zone, and
# removes them again. Audit entries stay: the log is append-only by design.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
PROTOSET="/tmp/ride.binpb"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
RUN="$(date +%s)"
OPS_EMAIL="e2e-ops-$RUN@ride.test"
REVOKE_EMAIL="e2e-revoke-$RUN@ride.test"
DRIVER_IDENTITY="$(python3 -c 'import uuid; print(uuid.uuid4())')"
OUTSIDER_IDENTITY="$(python3 -c 'import uuid; print(uuid.uuid4())')"
PLATE="E2E-STAFF-$RUN"
ZONE_CITY="E2E Staff City"

OWNER_EMAIL="$(sed -n 's/^STAFF_BOOTSTRAP_OWNER_EMAIL=//p' services/staff-service/.env | tr -d '"' | tr '[:upper:]' '[:lower:]')"
if [ -z "$OWNER_EMAIL" ]; then
  echo "ABORT: set STAFF_BOOTSTRAP_OWNER_EMAIL in services/staff-service/.env" >&2
  exit 2
fi

FAILURES=0
BODY_FILE="$(mktemp)"

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -f "$BODY_FILE"
  sql ride-staff-postgres "delete from staff_members where email like 'e2e-%@ride.test';" > /dev/null 2>&1 || true
  sql ride-driver-postgres "delete from drivers where vehicle_plate_number = '$PLATE';" > /dev/null 2>&1 || true
  sql ride-location-postgres "delete from zones where city = '$ZONE_CITY';" > /dev/null 2>&1 || true
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

# login <email>: prints an access token, reading the code from identity's log.
login() {
  local email="$1" status challenge code
  status="$(http POST /v1/auth/otp:request "" "{\"identifier\":{\"type\":\"IDENTIFIER_TYPE_EMAIL\",\"value\":\"$email\"},\"deliveryChannel\":\"OTP_DELIVERY_CHANNEL_EMAIL\"}")"
  if [ "$status" != 200 ]; then
    echo "FAIL: OTP request for $email answered $status: $(head -c 300 "$BODY_FILE")" >&2
    echo "      (one code per address a minute: wait a minute and run again)" >&2
    exit 1
  fi
  challenge="$(body_field 'd["challengeId"]')"

  code=""
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    code="$(docker logs --since 2m ride-identity-service 2>&1 | python3 -c '
import json, sys
email, code = sys.argv[1], ""
for line in sys.stdin:
    try:
        entry = json.loads(line)
    except ValueError:
        continue
    if entry.get("otp_identifier") == email and entry.get("otp_code"):
        code = entry["otp_code"]
print(code)
' "$email")"
    [ -n "$code" ] && break
    sleep 1
  done

  if [ -z "$code" ]; then
    echo "FAIL: no OTP code for $email in identity-service's log (is it in development mode?)" >&2
    exit 1
  fi

  status="$(http POST /v1/auth/otp:verify "" "{\"challengeId\":\"$challenge\",\"code\":\"$code\",\"clientId\":\"e2e\",\"deviceId\":\"e2e-staff\",\"deviceName\":\"E2E staff\",\"platform\":\"web\",\"appVersion\":\"1.0.0\"}")"
  if [ "$status" != 200 ]; then
    echo "FAIL: OTP verify for $email answered $status: $(head -c 300 "$BODY_FILE")" >&2
    exit 1
  fi

  body_field 'd["accessToken"]'
}

grpc_code() { # <token> <service/method> <address> <json>
  local out
  out="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$4" "$3" "$2" 2>&1)" && {
    echo OK
    return 0
  }

  echo "$out" | sed -n 's/^ *Code: *//p' | head -1
}

echo "==> [1/7] the first owner, and a user who is not staff"
buf build -o "$PROTOSET"
OWNER="$(login "$OWNER_EMAIL")"
OUTSIDER="$(mint "$OUTSIDER_IDENTITY")"

status="$(http GET /v1/staff/me "$OWNER")"
if [ "$status" = 404 ]; then
  expect "the owner accepts the first invitation" 200 POST /v1/staff/me:accept "$OWNER" '{}'
fi
expect "the owner reads their staff profile" 200 GET /v1/staff/me "$OWNER"
check "the owner is active" STAFF_STATUS_ACTIVE "$(body_field 'd["staffMember"]["status"]')"
check "the owner holds staff.manage" True "$(body_field '"staff.manage" in d.get("permissions", [])')"

expect "a user who is not staff has no staff profile" 404 GET /v1/staff/me "$OUTSIDER"
expect "a user who is not staff lists staff" 403 GET /v1/admin/staff "$OUTSIDER"
expect "a user who is not staff lists drivers" 403 GET /v1/admin/drivers "$OUTSIDER"
expect "a user who is not staff reads the audit log" 403 GET /v1/admin/audit "$OUTSIDER"
expect "a token identity never issued accepts an invitation" 401 POST /v1/staff/me:accept "$OUTSIDER" '{}'
expect "no token at all" 401 GET /v1/admin/staff ""

echo "==> [2/7] the owner invites an operator"
expect "invite the operator" 200 POST /v1/admin/staff "$OWNER" "{\"email\":\"$OPS_EMAIL\",\"displayName\":\"E2E Operator\",\"roleIds\":[\"$OPERATIONS_ROLE\"]}"
OPS_ID="$(body_field 'd["staffMember"]["id"]')"
check "the invitation waits" STAFF_STATUS_INVITED "$(body_field 'd["staffMember"]["status"]')"
expect "the same address twice" 409 POST /v1/admin/staff "$OWNER" "{\"email\":\"$OPS_EMAIL\",\"displayName\":\"Again\",\"roleIds\":[\"$OPERATIONS_ROLE\"]}"
expect "an invitation without a role" 400 POST /v1/admin/staff "$OWNER" "{\"email\":\"e2e-norole-$RUN@ride.test\",\"displayName\":\"X\",\"roleIds\":[]}"

OPS="$(login "$OPS_EMAIL")"
expect "the operator accepts" 200 POST /v1/staff/me:accept "$OPS" '{}'
check "the operator is active" STAFF_STATUS_ACTIVE "$(body_field 'd["staffMember"]["status"]')"
check "the operator holds the operations permissions" "drivers.approve,drivers.read,zones.manage" "$(body_field '",".join(d.get("permissions", []))')"
expect "accepting twice" 409 POST /v1/staff/me:accept "$OPS" '{}'

echo "==> [3/7] the operator reviews a driver"
DRIVER="$(mint "$DRIVER_IDENTITY")"
expect "a driver registers" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Staff Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\"}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"

expect "the operator lists the review queue" 200 GET "/v1/admin/drivers?status=DRIVER_STATUS_PENDING&page_size=100" "$OPS"
check "the new driver is in the queue" True "$(body_field "any(x['id'] == '$DRIVER_ID' for x in d.get('drivers', []))")"
expect "the operator reads the driver" 200 GET "/v1/drivers/$DRIVER_ID" "$OPS"
expect "a rejection needs a reason" 400 POST "/v1/admin/drivers/$DRIVER_ID:reject" "$OPS" '{}'
expect "the operator rejects with a reason" 200 POST "/v1/admin/drivers/$DRIVER_ID:reject" "$OPS" '{"reason":"The licence photo is not readable"}'
check "rejected" DRIVER_STATUS_REJECTED "$(body_field 'd["driver"]["status"]')"
expect "the driver reads their profile" 200 GET "/v1/drivers/$DRIVER_ID" "$DRIVER"
check "the driver sees the reason" "The licence photo is not readable" "$(body_field 'd["driver"].get("rejectionReason", "")')"
expect "the driver approves themselves" 403 POST "/v1/admin/drivers/$DRIVER_ID:approve" "$DRIVER" '{}'
expect "the operator approves" 200 POST "/v1/admin/drivers/$DRIVER_ID:approve" "$OPS" '{}'
check "active" DRIVER_STATUS_ACTIVE "$(body_field 'd["driver"]["status"]')"
check "the reason is cleared" "" "$(body_field 'd["driver"].get("rejectionReason", "")')"

echo "==> [4/7] zones, and what the operator may not do"
expect "the operator creates a zone" 200 POST /v1/admin/zones "$OPS" "{\"city\":\"$ZONE_CITY\",\"name\":\"E2E\",\"boundary\":[{\"latitude\":10,\"longitude\":10},{\"latitude\":10,\"longitude\":10.01},{\"latitude\":10.01,\"longitude\":10.01},{\"latitude\":10.01,\"longitude\":10}]}"
ZONE_ID="$(body_field 'd["zone"]["id"]')"
expect "the operator switches it off" 200 POST "/v1/admin/zones/$ZONE_ID:setActive" "$OPS" '{"active":false}'
expect "a rider creates a zone" 403 POST /v1/admin/zones "$OUTSIDER" "{\"city\":\"$ZONE_CITY\",\"name\":\"X\",\"boundary\":[{\"latitude\":1,\"longitude\":1},{\"latitude\":1,\"longitude\":2},{\"latitude\":2,\"longitude\":2}]}"
expect "the operator lists staff" 403 GET /v1/admin/staff "$OPS"
expect "the operator invites someone" 403 POST /v1/admin/staff "$OPS" "{\"email\":\"e2e-x-$RUN@ride.test\",\"displayName\":\"X\",\"roleIds\":[\"$OPERATIONS_ROLE\"]}"
expect "the operator reads the audit log" 403 GET /v1/admin/audit "$OPS"

echo "==> [5/7] the audit log"
expect "the owner reads the driver's audit trail" 200 GET "/v1/admin/audit?target_id=$DRIVER_ID&page_size=50" "$OWNER"
check "the approval is recorded as succeeded" True "$(body_field "any(e['method'].endswith('/ApproveDriver') and e['decision'] == 'AUDIT_DECISION_ALLOWED' and e.get('outcome') == 'AUDIT_OUTCOME_SUCCEEDED' and e.get('actorStaffId') == '$OPS_ID' for e in d.get('entries', []))")"
check "the rejection without a reason is recorded as failed" True "$(body_field "any(e['method'].endswith('/RejectDriver') and e.get('outcome') == 'AUDIT_OUTCOME_FAILED' and e.get('outcomeCode') == 'InvalidArgument' for e in d.get('entries', []))")"
check "the driver's own attempt is recorded as denied" True "$(body_field "any(e['method'].endswith('/ApproveDriver') and e['decision'] == 'AUDIT_DECISION_DENIED' and e['actorIdentityId'] == '$DRIVER_IDENTITY' for e in d.get('entries', []))")"
expect "the owner reads the audit log by permission" 200 GET "/v1/admin/audit?permission=zones.manage&page_size=5" "$OWNER"
check "zone changes are there" True "$(body_field "len(d.get('entries', [])) > 0")"

echo "==> [6/7] suspension, self-protection, revocation"
OWNER_ID="$(http GET /v1/staff/me "$OWNER" > /dev/null; body_field 'd["staffMember"]["id"]')"
expect "the owner suspends the operator" 200 POST "/v1/admin/staff/$OPS_ID:suspend" "$OWNER" '{}'
expect "the suspended operator lists drivers" 403 GET /v1/admin/drivers "$OPS"
expect "the suspended operator reads their profile" 200 GET /v1/staff/me "$OPS"
check "suspended, with no permission" "STAFF_STATUS_SUSPENDED 0" "$(body_field 'd["staffMember"]["status"] + " " + str(len(d.get("permissions", [])))')"
expect "the owner reactivates the operator" 200 POST "/v1/admin/staff/$OPS_ID:reactivate" "$OWNER" '{}'
expect "the operator works again" 200 GET /v1/admin/drivers "$OPS"
expect "the owner suspends themselves" 403 POST "/v1/admin/staff/$OWNER_ID:suspend" "$OWNER" '{}'
expect "the owner changes their own roles" 403 PUT "/v1/admin/staff/$OWNER_ID/roles" "$OWNER" "{\"roleIds\":[\"$OPERATIONS_ROLE\"]}"
expect "invite an address to revoke" 200 POST /v1/admin/staff "$OWNER" "{\"email\":\"$REVOKE_EMAIL\",\"displayName\":\"E2E Revoke\",\"roleIds\":[\"$OPERATIONS_ROLE\"]}"
REVOKE_ID="$(body_field 'd["staffMember"]["id"]')"
expect "revoke the invitation" 200 POST "/v1/admin/staff/$REVOKE_ID:revoke" "$OWNER" '{}'
check "revoked" STAFF_STATUS_REVOKED "$(body_field 'd["staffMember"]["status"]')"
expect "the address can be invited again" 200 POST /v1/admin/staff "$OWNER" "{\"email\":\"$REVOKE_EMAIL\",\"displayName\":\"E2E Revoke\",\"roleIds\":[\"$OPERATIONS_ROLE\"]}"
expect "the owner lists roles" 200 GET /v1/admin/roles "$OWNER"
check "owner and operations are there" True "$(body_field "{'owner','operations'} <= {r['key'] for r in d['roles']}")"

echo "==> [7/7] the internal RPCs"
STAFF_ADDR="localhost:50061"
check "AuthorizeStaffAction with a user token" PermissionDenied "$(grpc_code "$OPS" ride.staff.v1.StaffService/AuthorizeStaffAction "$STAFF_ADDR" "{\"identity_id\":\"$OUTSIDER_IDENTITY\",\"permission\":\"drivers.approve\",\"method\":\"/x/Y\"}")"
check "CompleteStaffAction with a user token" PermissionDenied "$(grpc_code "$OWNER" ride.staff.v1.StaffService/CompleteStaffAction "$STAFF_ADDR" '{"audit_entry_id":"00000000-0000-4000-8000-000000000001","outcome_code":"OK"}')"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: staff sign in, act only within their permissions, and every attempt is audited"
