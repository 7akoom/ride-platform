#!/usr/bin/env bash
# End-to-end test of the support desk, on the real services, through the
# gateway. Run from the ride-platform repo root:
#   bash scripts/e2e/test-support.sh
#
# Needs the local identity signing key, grpcurl, buf, support-service with its
# migrations, staff-service migration 00004 (support roles), notification-
# service migration 00014, and wallet-service and identity-service built from
# this change (acting staff on refunds, suspend/reactivate).
#
# What it proves:
#   1. a rider opens a ticket about their trip, with a file; the same category
#      about the same trip lands in the open ticket; nobody else's trip, no
#      ticket without the trip a category needs
#   2. staff: the queue (support.read), claim, reply (the rider is told by
#      push), an internal note the rider never sees; the operations role has
#      no access; the rider answers and it reopens; resolved, then closed
#   3. a lost item brings the trip's driver into the ticket; the driver is
#      told and answers inside it
#   4. a safety ticket is out of the normal queue and needs support.safety
#   5. actions: a refund under the limit runs at once, as the agent; money over
#      the limit waits for someone else with support.approve; a fee waiver with
#      nothing to waive is refused; a lead suspends the trip's driver (sessions
#      gone, offline) and reactivates them
#   6. every staff call is in the audit log
#
# The SOS ticket is covered by the service's own tests, not here: pressing SOS
# alerts the operator phones for real.
#
# Everything it creates is removed again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
WALLET_ADDR="localhost:50058"
PROTOSET="/tmp/ride.binpb"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
OPERATIONS_ROLE="5e7a0000-0000-4000-8000-000000000002"
AGENT_ROLE="5e7a0000-0000-4000-8000-000000000003"
LEAD_ROLE="5e7a0000-0000-4000-8000-000000000004"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
STRANGER_IDENTITY="$(uuid)"
DRIVER_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
OPERATOR_IDENTITY="$(uuid)"
AGENT_IDENTITY="$(uuid)"
LEAD_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
OPERATOR_STAFF_ID="$(uuid)"
AGENT_STAFF_ID="$(uuid)"
LEAD_STAFF_ID="$(uuid)"
TRIP="$(uuid)"
CANCELLED_TRIP="$(uuid)"
UNKNOWN="$(uuid)"
PLATE="E2E-SUP-$RUN"

INTERNAL_TOKEN=""
for env_file in services/wallet-service/.env services/support-service/.env; do
  [ -z "$INTERNAL_TOKEN" ] && [ -f "$env_file" ] && INTERNAL_TOKEN="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' "$env_file" | head -1 | tr -d '"')"
done
INTERNAL_TOKEN="${INTERNAL_TOKEN:-dev-internal-service-token-change-me}"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""
STRANGER_ID=""
DRIVER_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  local identities="'$RIDER_IDENTITY', '$STRANGER_IDENTITY', '$DRIVER_IDENTITY'"
  local owners="'${RIDER_ID:-$UNKNOWN}', '${STRANGER_ID:-$UNKNOWN}', '${DRIVER_ID:-$UNKNOWN}'"
  sql ride-support-postgres "
    create temp table gone as select id from support_tickets where requester_identity_id in ($identities);
    delete from outbox_events where aggregate_id in (select id from gone);
    delete from support_actions where ticket_id in (select id from gone);
    delete from support_attachments where ticket_id in (select id from gone);
    delete from support_messages where ticket_id in (select id from gone);
    delete from support_tickets where id in (select id from gone);" > /dev/null 2>&1 || true
  sql ride-staff-postgres "delete from staff_members where id in ('$OWNER_STAFF_ID', '$OPERATOR_STAFF_ID', '$AGENT_STAFF_ID', '$LEAD_STAFF_ID');" > /dev/null 2>&1 || true
  sql ride-trip-postgres "delete from trips where id in ('$TRIP', '$CANCELLED_TRIP');" > /dev/null 2>&1 || true
  sql ride-wallet-postgres "
    delete from wallet_adjustments where owner_id in ($owners) or driver_id in ($owners);
    delete from trip_settlements where trip_id = '$TRIP';
    delete from wallet_transactions where wallet_id in (select id from wallets where owner_id in ($owners));
    delete from wallets where owner_id in ($owners);" > /dev/null 2>&1 || true
  sql ride-notification-postgres "delete from notifications where recipient_id in ($owners);" > /dev/null 2>&1 || true
  sql ride-media-postgres "delete from media_objects where owner_identity_id in ($identities);" > /dev/null 2>&1 || true
  sql ride-driver-postgres "delete from vehicles where plate_number = '$PLATE'; delete from drivers where vehicle_plate_number = '$PLATE';" > /dev/null 2>&1 || true
  sql ride-rider-postgres "delete from riders where identity_id in ($identities);" > /dev/null 2>&1 || true
  sql ride-identity-postgres "
    delete from outbox_events where aggregate_id in ($identities);
    delete from identities where id in ($identities);" > /dev/null 2>&1 || true
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

eventually() { # <label> <expected> <command...>: retries for 20 seconds
  local label="$1" expected="$2" actual=""
  shift 2

  for _ in $(seq 1 40); do
    actual="$("$@" 2>/dev/null || true)"
    [ "$actual" = "$expected" ] && break
    sleep 0.5
  done

  check "$label" "$expected" "$actual"
}

notified() { # <recipient id> <event key>: how many
  sql ride-notification-postgres "select count(*) from notifications where recipient_id = '$1' and event_key = '$2'"
}

internal_call() { # <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$2" "$WALLET_ADDR" "ride.wallet.v1.WalletService/$1" > /dev/null
}

add_staff() { # <staff id> <identity> <role>
  sql ride-staff-postgres "
    insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
    values ('$1', '$2', 'e2e-sup-$1@ride.test', 'E2E Support', 'active', now(), now());
    insert into staff_member_roles (staff_id, role_id) values ('$1', '$3');" > /dev/null
}

add_rider() { # <identity> <token> <name> -> rider id
  http POST /v1/riders "$2" "{\"identityId\":\"$1\",\"displayName\":\"$3\"}" > /dev/null
  body_field 'd["rider"]["id"]'
}

ticket() { # <audience> <category> <body> [trip] [attachment]
  local extra=""
  [ -n "${4:-}" ] && extra="$extra,\"tripId\":\"$4\""
  [ -n "${5:-}" ] && extra="$extra,\"attachmentMediaIds\":[\"$5\"]"
  echo "{\"audience\":\"$1\",\"categoryKey\":\"$2\",\"body\":\"$3\"$extra}"
}

action() { # <kind> <reason> [extra json]
  echo "{\"kind\":\"$1\",\"reason\":\"$2\"${3:-}}"
}

balance_of() { # <owner type> <owner id>: read by the owner staff member
  http GET "/v1/admin/wallets/$2?owner_type=$1" "$OWNER" > /dev/null
  body_field 'd["wallet"]["balance"]'
}

upload() { # <token> <file> -> media id of a READY support attachment
  local size url
  size="$(stat -c %s "$2")"
  http POST /v1/media:upload "$1" "{\"purpose\":\"MEDIA_PURPOSE_SUPPORT_ATTACHMENT\",\"contentType\":\"image/png\",\"sizeBytes\":$size}" > /dev/null
  cp "$BODY_FILE" "$WORK/upload.json"
  local id
  id="$(body_field 'd["media"]["id"]')"
  url="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["uploadUrl"])' "$WORK/upload.json")"

  local args=()
  while IFS= read -r header; do
    [ -n "$header" ] && args+=(-H "$header")
  done < <(python3 -c '
import json, sys
for name, value in json.load(open(sys.argv[1])).get("uploadHeaders", {}).items():
    if name.lower() not in ("content-type", "content-length"):
        print(f"{name}: {value}")
' "$WORK/upload.json")

  curl -sS -o /dev/null -X PUT "${args[@]}" -H "Content-Type: image/png" --data-binary "@$2" "$url"
  http POST "/v1/media/$id:complete" "$1" '{}' > /dev/null
  echo "$id"
}

[ "$(sql ride-support-postgres "select to_regclass('public.support_tickets') is not null" 2>/dev/null)" = t ] \
  || { echo "ABORT: the support tables do not exist: start support-postgres and apply support-service migrations (goose up)" >&2; exit 2; }
[ "$(sql ride-staff-postgres "select count(*) from roles where key in ('support_agent', 'support_lead')")" = 2 ] \
  || { echo "ABORT: the support roles do not exist: apply staff-service migration 00004 (goose up)" >&2; exit 2; }
[ "$(sql ride-notification-postgres "select count(*) from notification_templates where event_key like 'support.%'")" = 3 ] \
  || { echo "ABORT: the support push templates do not exist: apply notification-service migration 00014 (goose up)" >&2; exit 2; }

python3 - "$WORK" <<'PY'
import struct, sys, zlib

def png(width, height):
    raw = b"".join(b"\x00" + b"".join(bytes((x * 3 % 256, y * 5 % 256, 90)) for x in range(width)) for y in range(height))
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) + \
        chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")

open(f"{sys.argv[1]}/receipt.png", "wb").write(png(40, 30))
PY

buf build -o "$PROTOSET"
RIDER="$(mint "$RIDER_IDENTITY")"
STRANGER="$(mint "$STRANGER_IDENTITY")"
DRIVER="$(mint "$DRIVER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"
OPERATOR="$(mint "$OPERATOR_IDENTITY")"
AGENT="$(mint "$AGENT_IDENTITY")"
LEAD="$(mint "$LEAD_IDENTITY")"

echo "==> [0/6] a rider, a stranger, a driver, a trip between them, and four staff"
sql ride-identity-postgres "insert into identities (id) values ('$RIDER_IDENTITY'), ('$STRANGER_IDENTITY'), ('$DRIVER_IDENTITY');" > /dev/null
add_staff "$OWNER_STAFF_ID" "$OWNER_IDENTITY" "$OWNER_ROLE"
add_staff "$OPERATOR_STAFF_ID" "$OPERATOR_IDENTITY" "$OPERATIONS_ROLE"
add_staff "$AGENT_STAFF_ID" "$AGENT_IDENTITY" "$AGENT_ROLE"
add_staff "$LEAD_STAFF_ID" "$LEAD_IDENTITY" "$LEAD_ROLE"
RIDER_ID="$(add_rider "$RIDER_IDENTITY" "$RIDER" "E2E Support Rider")"
STRANGER_ID="$(add_rider "$STRANGER_IDENTITY" "$STRANGER" "E2E Support Stranger")"
expect "a driver" 200 POST /v1/drivers "$DRIVER" "{\"identityId\":\"$DRIVER_IDENTITY\",\"displayName\":\"E2E Support Driver\",\"vehicle\":{\"make\":\"Kia\",\"model\":\"Rio\",\"color\":\"Blue\",\"plateNumber\":\"$PLATE\",\"vehicleClass\":\"economy\",\"year\":2020}}"
DRIVER_ID="$(body_field 'd["driver"]["id"]')"
sql ride-driver-postgres "update drivers set status = 'active', availability_status = 'available' where id = '$DRIVER_ID';" > /dev/null
sql ride-trip-postgres "
  insert into trips (id, rider_id, driver_id, status, pickup_latitude, pickup_longitude, dropoff_latitude, dropoff_longitude,
                     vehicle_class, accepted_at, started_at, completed_at)
  values ('$TRIP', '$RIDER_ID', '$DRIVER_ID', 'completed', 36.19, 44.01, 36.2, 44.02, 'economy', now(), now(), now());
  insert into trips (id, rider_id, driver_id, status, pickup_latitude, pickup_longitude, dropoff_latitude, dropoff_longitude,
                     vehicle_class, cancelled_at, cancelled_by)
  values ('$CANCELLED_TRIP', '$RIDER_ID', '$DRIVER_ID', 'cancelled', 36.19, 44.01, 36.2, 44.02, 'economy', now(), 'rider');" > /dev/null
internal_call TopUp "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER_ID\",\"amount\":\"1000\",\"idempotency_key\":\"e2e-sup-$RUN\"}"
internal_call SettleTrip "{\"trip_id\":\"$TRIP\",\"rider_id\":\"$RIDER_ID\",\"driver_id\":\"$DRIVER_ID\",\"fare_amount\":\"8000\",\"payment_method\":\"PAYMENT_METHOD_CASH\"}"
check "the people exist" "2 1" "$(sql ride-rider-postgres "select count(*) from riders where id in ('$RIDER_ID', '$STRANGER_ID')") $(sql ride-driver-postgres "select count(*) from drivers where id = '$DRIVER_ID'")"

echo "==> [1/6] a rider opens tickets"
expect "the rider's categories" 200 GET "/v1/support/categories?audience=AUDIENCE_RIDER" "$RIDER"
check "no sos, no driver categories" "False False True" "$(body_field '" ".join(str(any(c["key"] == k for c in d["categories"])) for k in ("sos", "earnings_payout", "lost_item"))')"
expect "a fare ticket without its trip" 400 POST /v1/support/tickets "$RIDER" "$(ticket AUDIENCE_RIDER trip_fare "charged twice")"
expect "the stranger about the rider's trip" 400 POST /v1/support/tickets "$STRANGER" "$(ticket AUDIENCE_RIDER trip_fare "not mine" "$TRIP")"
expect "a driver ticket from someone with no driver profile" 400 POST /v1/support/tickets "$RIDER" "$(ticket AUDIENCE_DRIVER account "hi")"
ATTACHMENT="$(upload "$RIDER" "$WORK/receipt.png")"
expect "a fare ticket with a receipt" 200 POST /v1/support/tickets "$RIDER" "$(ticket AUDIENCE_RIDER trip_fare "I was charged twice" "$TRIP" "$ATTACHMENT")"
FARE="$(body_field 'd["ticket"]["id"]')"
check "open, normal, numbered, the receipt on it" "TICKET_STATUS_OPEN TICKET_PRIORITY_NORMAL True $ATTACHMENT" \
  "$(body_field '" ".join((d["ticket"]["status"], d["ticket"]["priority"], str(d["ticket"]["number"].startswith("S-")), d["messages"][0]["attachmentMediaIds"][0]))')"
expect "the same again" 200 POST /v1/support/tickets "$RIDER" "$(ticket AUDIENCE_RIDER trip_fare "any news?" "$TRIP")"
check "lands in the open ticket" "True $FARE 2" "$(body_field '" ".join((str(d.get("existing", False)), d["ticket"]["id"], str(len(d["messages"]))))')"
expect "the receipt, for the rider" 200 GET "/v1/support/tickets/$FARE/attachments/$ATTACHMENT" "$RIDER"
expect "the receipt, for the stranger" 404 GET "/v1/support/tickets/$FARE/attachments/$ATTACHMENT" "$STRANGER"
expect "the ticket, for the stranger" 404 GET "/v1/support/tickets/$FARE" "$STRANGER"
expect "the rider's tickets" 200 GET "/v1/support/tickets?audience=AUDIENCE_RIDER" "$RIDER"
check "one" "1 TICKET_ROLE_REQUESTER" "$(body_field '" ".join((str(len(d["tickets"])), d["tickets"][0]["role"]))')"

echo "==> [2/6] staff work it"
expect "the queue, operations role" 403 GET /v1/admin/support/tickets "$OPERATOR"
expect "the queue, agent" 200 GET "/v1/admin/support/tickets?unassigned_only=true" "$AGENT"
check "has the ticket" True "$(body_field "any(t['ticket']['id'] == '$FARE' for t in d.get('tickets', []))")"
expect "the receipt, for the agent" 200 GET "/v1/support/tickets/$FARE/attachments/$ATTACHMENT" "$AGENT"
expect "claim" 200 POST "/v1/admin/support/tickets/$FARE:claim" "$AGENT" '{}'
check "in progress, the agent's" "TICKET_STATUS_IN_PROGRESS $AGENT_STAFF_ID" "$(body_field '" ".join((d["ticket"]["ticket"]["status"], d["ticket"]["assignedStaffId"]))')"
expect "the lead claims it too" 400 POST "/v1/admin/support/tickets/$FARE:claim" "$LEAD" '{}'
expect "reply" 200 POST "/v1/admin/support/tickets/$FARE/messages" "$AGENT" '{"body":"We are checking the charge."}'
check "waiting for the rider" TICKET_STATUS_WAITING_USER "$(body_field 'd["ticket"]["ticket"]["status"]')"
expect "an internal note" 200 POST "/v1/admin/support/tickets/$FARE/messages" "$AGENT" '{"body":"looks like a duplicate settlement","internal":true}'
eventually "the rider is told of the reply" 1 notified "$RIDER_ID" support.reply_received
expect "the rider reads it" 200 GET "/v1/support/tickets/$FARE" "$RIDER"
check "3 messages, no internal note" "3 False" "$(body_field '" ".join((str(len(d["messages"])), str(any(m.get("internal") for m in d["messages"]))))')"
expect "the rider answers" 200 POST "/v1/support/tickets/$FARE/messages" "$RIDER" '{"body":"thank you"}'
check "back in progress" TICKET_STATUS_IN_PROGRESS "$(body_field 'd["ticket"]["status"]')"

echo "==> [3/6] a lost item"
expect "a lost item on the trip" 200 POST /v1/support/tickets "$RIDER" "$(ticket AUDIENCE_RIDER lost_item "I left my phone" "$TRIP")"
LOST="$(body_field 'd["ticket"]["id"]')"
check "high priority" TICKET_PRIORITY_HIGH "$(body_field 'd["ticket"]["priority"]')"
eventually "the driver is told" 1 notified "$DRIVER_ID" support.lost_item_reported
expect "the driver's tickets" 200 GET "/v1/support/tickets?audience=AUDIENCE_DRIVER" "$DRIVER"
check "has it, as participant" "TICKET_ROLE_PARTICIPANT" "$(body_field "next(t['role'] for t in d['tickets'] if t['id'] == '$LOST')")"
expect "the driver answers" 200 POST "/v1/support/tickets/$LOST/messages" "$DRIVER" '{"body":"Found it, I can bring it tonight."}'
eventually "the rider is told of the driver's answer" 2 notified "$RIDER_ID" support.reply_received
expect "the driver closes the rider's ticket" 403 POST "/v1/support/tickets/$LOST:close" "$DRIVER" '{}'
expect "the driver reads the fare ticket" 404 GET "/v1/support/tickets/$FARE" "$DRIVER"

echo "==> [4/6] safety"
expect "a safety ticket" 200 POST /v1/support/tickets "$RIDER" "$(ticket AUDIENCE_RIDER safety "The driver was driving dangerously" "$TRIP")"
SAFETY="$(body_field 'd["ticket"]["id"]')"
check "urgent, safety" "TICKET_PRIORITY_URGENT True" "$(body_field '" ".join((d["ticket"]["priority"], str(d["ticket"]["safety"])))')"
expect "the agent's queue" 200 GET /v1/admin/support/tickets "$AGENT"
check "does not have it" False "$(body_field "any(t['ticket']['id'] == '$SAFETY' for t in d.get('tickets', []))")"
expect "the agent opens it" 403 GET "/v1/admin/support/tickets/$SAFETY" "$AGENT"
expect "the agent's safety queue" 403 GET /v1/admin/support/safety-tickets "$AGENT"
expect "the lead's safety queue" 200 GET /v1/admin/support/safety-tickets "$LEAD"
check "has it" True "$(body_field "any(t['ticket']['id'] == '$SAFETY' for t in d.get('tickets', []))")"
expect "the lead opens it" 200 GET "/v1/admin/support/tickets/$SAFETY" "$LEAD"
expect "lowering its priority" 400 POST "/v1/admin/support/tickets/$SAFETY:set-priority" "$LEAD" '{"priority":"TICKET_PRIORITY_LOW"}'
expect "the driver reads it" 404 GET "/v1/support/tickets/$SAFETY" "$DRIVER"

echo "==> [5/6] actions"
RIDER_BEFORE="$(balance_of OWNER_TYPE_RIDER "$RIDER_ID")"
expect "a refund by the operations role" 403 POST "/v1/admin/support/tickets/$FARE/actions" "$OPERATOR" "$(action ACTION_KIND_REFUND "charged twice" ',"amount":"3000"')"
expect "a refund without a reason" 400 POST "/v1/admin/support/tickets/$FARE/actions" "$AGENT" "$(action ACTION_KIND_REFUND "" ',"amount":"3000"')"
expect "a refund of 3000, 1000 from the driver" 200 POST "/v1/admin/support/tickets/$FARE/actions" "$AGENT" "$(action ACTION_KIND_REFUND "charged twice" ',"amount":"3000","driverAmount":"1000"')"
check "completed" ACTION_STATUS_COMPLETED "$(body_field 'd["action"]["status"]')"
check "the rider got 3000" "$(python3 -c "print(round(float('$RIDER_BEFORE') + 3000, 3))")" "$(python3 -c "print(round(float('$(balance_of OWNER_TYPE_RIDER "$RIDER_ID")'), 3))")"
check "recorded as the agent's refund" "refund $AGENT_IDENTITY" "$(sql ride-wallet-postgres "select kind || ' ' || created_by from wallet_adjustments where trip_id = '$TRIP' order by created_at desc limit 1")"
expect "a 30000 credit, over the limit" 200 POST "/v1/admin/support/tickets/$FARE/actions" "$AGENT" "$(action ACTION_KIND_COMPENSATION "goodwill" ',"amount":"30000"')"
WAITING="$(body_field 'd["action"]["id"]')"
check "waits for approval" ACTION_STATUS_PENDING_APPROVAL "$(body_field 'd["action"]["status"]')"
expect "the approval queue, agent" 403 GET /v1/admin/support/actions "$AGENT"
expect "the approval queue, lead" 200 GET /v1/admin/support/actions "$LEAD"
check "has it" True "$(body_field "any(a['id'] == '$WAITING' for a in d.get('actions', []))")"
expect "the agent approves" 403 POST "/v1/admin/support/actions/$WAITING:approve" "$AGENT" '{}'
expect "the lead rejects without a reason" 400 POST "/v1/admin/support/actions/$WAITING:reject" "$LEAD" '{}'
expect "the lead approves" 200 POST "/v1/admin/support/actions/$WAITING:approve" "$LEAD" '{"reason":"ok"}'
check "completed, decided by the lead" "ACTION_STATUS_COMPLETED $LEAD_STAFF_ID" "$(body_field '" ".join((d["action"]["status"], d["action"]["decidedByStaffId"]))')"
check "recorded as the lead's credit" "adjustment $LEAD_IDENTITY 30000.000" "$(sql ride-wallet-postgres "select kind || ' ' || created_by || ' ' || amount from wallet_adjustments where owner_id = '$RIDER_ID' and kind = 'adjustment' order by created_at desc limit 1")"
expect "approved twice" 400 POST "/v1/admin/support/actions/$WAITING:approve" "$LEAD" '{}'
expect "a ticket about the cancelled trip" 200 POST /v1/support/tickets "$RIDER" "$(ticket AUDIENCE_RIDER trip_fare "unfair fee" "$CANCELLED_TRIP")"
FEE="$(body_field 'd["ticket"]["id"]')"
expect "waiving a fee never charged" 400 POST "/v1/admin/support/tickets/$FEE/actions" "$AGENT" "$(action ACTION_KIND_WAIVE_FEE "driver was late")"
expect "the agent suspends the driver" 403 POST "/v1/admin/support/tickets/$FARE/actions" "$AGENT" "$(action ACTION_KIND_SUSPEND_ACCOUNT "complaint" ',"target":"ACTION_TARGET_COUNTERPART"')"
expect "the lead suspends the driver" 200 POST "/v1/admin/support/tickets/$FARE/actions" "$LEAD" "$(action ACTION_KIND_SUSPEND_ACCOUNT "complaint under review" ',"target":"ACTION_TARGET_COUNTERPART"')"
check "completed, the driver's" "ACTION_STATUS_COMPLETED $DRIVER_ID" "$(body_field '" ".join((d["action"]["status"], d["action"]["targetProfileId"]))')"
check "the driver's identity is suspended" suspended "$(sql ride-identity-postgres "select status from identities where id = '$DRIVER_IDENTITY'")"
check "the driver is offline" offline "$(sql ride-driver-postgres "select availability_status from drivers where id = '$DRIVER_ID'")"
expect "the lead reactivates the driver" 200 POST "/v1/admin/support/tickets/$FARE/actions" "$LEAD" "$(action ACTION_KIND_REACTIVATE_ACCOUNT "cleared" ',"target":"ACTION_TARGET_COUNTERPART"')"
check "active again" active "$(sql ride-identity-postgres "select status from identities where id = '$DRIVER_IDENTITY'")"
expect "the ticket, for the lead" 200 GET "/v1/admin/support/tickets/$FARE" "$LEAD"
check "4 actions on it" 4 "$(body_field 'len(d["actions"])')"

expect "resolved" 200 POST "/v1/admin/support/tickets/$FARE:set-status" "$AGENT" '{"status":"TICKET_STATUS_RESOLVED"}'
eventually "the rider is told it is resolved" 1 notified "$RIDER_ID" support.ticket_resolved
expect "the rider closes it" 200 POST "/v1/support/tickets/$FARE:close" "$RIDER" '{}'
check "closed" TICKET_STATUS_CLOSED "$(body_field 'd["ticket"]["status"]')"
expect "writing in a closed ticket" 400 POST "/v1/support/tickets/$FARE/messages" "$RIDER" '{"body":"one more thing"}'

echo "==> [6/6] the audit log"
check "the agent's calls are audited" True "$(python3 -c "print(int('$(sql ride-staff-postgres "select count(*) from audit_entries where actor_staff_id = '$AGENT_STAFF_ID' and permission like 'support.%' and decision = 'allowed'")') >= 6)")"
check "denied calls too" True "$(python3 -c "print(int('$(sql ride-staff-postgres "select count(*) from audit_entries where actor_identity_id = '$OPERATOR_IDENTITY' and permission like 'support.%' and decision = 'denied'")') >= 2)")"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: the support desk works end to end"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
