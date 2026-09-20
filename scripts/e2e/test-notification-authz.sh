#!/usr/bin/env bash
# End-to-end notification authorization test with REAL user tokens.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-notification-authz.sh
#
# Tokens are minted for local development only (scripts/tools/devtoken) and
# are never printed.
#
# What it proves:
#   1. a user registers devices, lists notifications and marks them read only
#      for their own rider or driver profile
#   2. a rider id is never accepted as a driver recipient (or the reverse), and
#      an unspecified type or empty id is refused
#   3. a device can be unregistered only by its owner; an unknown token looks
#      the same as someone else's; a refused attempt removes nothing
#   4. Send and UpsertTemplate are internal only
#   5. the internal token still reaches everything (the event consumers and
#      other services depend on it)
#
# It registers two throw-away device tokens and removes them again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

INTERNAL_TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"

DRIVER_A="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
RIDER_IDENTITY_1="a0000000-0000-4000-8000-0000000000a1"
RIDER_IDENTITY_2="a0000000-0000-4000-8000-0000000000a2"
MISSING_ID="00000000-0000-4000-8000-00000000dead"

TOKEN_DEVICE_R1="authz-test-device-rider-1"
TOKEN_DEVICE_DA="authz-test-device-driver-a"

compose_port() { # <compose service name>
  awk -v svc="$1" '
    $0 ~ ("^  " svc ":$") {inside=1; next}
    inside && /^  [A-Za-z0-9_-]+:$/ {exit}
    inside && /GRPC_ADDRESS/ {gsub(/[^0-9]/, ""); print; exit}
  ' infrastructure/compose/compose.yaml
}

RIDER_PORT="$(compose_port rider-service)"
DRIVER_PORT="$(compose_port driver-service)"
NOTIFICATION_PORT="$(compose_port notification-service)"
[ -n "$RIDER_PORT" ] && [ -n "$DRIVER_PORT" ] && [ -n "$NOTIFICATION_PORT" ] \
  || { echo "FAIL: could not read the rider/driver/notification GRPC_ADDRESS from compose.yaml" >&2; exit 1; }

RIDER_ADDR="localhost:$RIDER_PORT"
DRIVER_ADDR="localhost:$DRIVER_PORT"
NOTIFICATION_ADDR="localhost:$NOTIFICATION_PORT"
RIDER_SVC="ride.rider.v1.RiderService"
DRIVER_SVC="ride.driver.v1.DriverService"
NOTIFICATION_SVC="ride.notification.v1.NotificationService"

RDR="RECIPIENT_TYPE_RIDER"
DRV="RECIPIENT_TYPE_DRIVER"

FAILURES=0

mint() { go run scripts/tools/devtoken/main.go -sub "$1" "${@:2}"; }

internal_call() { # <address> <service/method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $INTERNAL_TOKEN" -d "$3" "$1" "$2"
}

json_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

call() { # <token> <method> <json>  -> the raw response (stdout) of an authorized call
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$3" "$NOTIFICATION_ADDR" "$NOTIFICATION_SVC/$2"
}

code_of() { # <token> <method> <json>  -> OK or the gRPC code name
  local output
  output="$(grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $1" -d "$3" "$NOTIFICATION_ADDR" "$NOTIFICATION_SVC/$2" 2>&1 || true)"

  if grep -q '^ERROR:' <<<"$output"; then
    awk '/Code:/ {print $2; exit}' <<<"$output"
  else
    echo OK
  fi
}

expect() { # <label> <expected> <token> <method> <json>
  local actual
  actual="$(code_of "$3" "$4" "$5")"

  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "${actual:-no response}"
    FAILURES=$((FAILURES + 1))
  fi
}

# Only that the authorization layer let the caller through; what the business
# logic answered (no such template...) is not this test's concern.
expect_authorized() { # <label> <token> <method> <json>
  local actual
  actual="$(code_of "$2" "$3" "$4")"

  if [ -n "$actual" ] && [ "$actual" != "PermissionDenied" ] && [ "$actual" != "Unauthenticated" ]; then
    printf '  ok    %s -> authorized (%s)\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected to pass authorization, got %s\n' "$1" "${actual:-no response}"
    FAILURES=$((FAILURES + 1))
  fi
}

rider_id_for_identity() { # <identity>
  internal_call "$RIDER_ADDR" "$RIDER_SVC/GetRiderByIdentity" "{\"identity_id\":\"$1\"}" 2>/dev/null \
    | json_field 'd["rider"]["id"]' 2>/dev/null || true
}

ensure_rider() { # <identity> <name>
  local id
  id="$(rider_id_for_identity "$1")"

  if [ -z "$id" ]; then
    internal_call "$RIDER_ADDR" "$RIDER_SVC/CreateRider" "{\"identity_id\":\"$1\",\"display_name\":\"$2\"}" > /dev/null
    id="$(rider_id_for_identity "$1")"
  fi

  [ -n "$id" ] || { echo "FAIL: could not find or create rider $2" >&2; exit 1; }
  echo "$id"
}

register() { # <type> <recipient id> <device token>
  echo "{\"recipient_type\":\"$1\",\"recipient_id\":\"$2\",\"device_token\":\"$3\",\"platform\":\"PLATFORM_ANDROID\",\"locale\":\"en\"}"
}

unregister() { # <device token>
  echo "{\"device_token\":\"$1\"}"
}

recipient() { # <type> <recipient id>
  echo "{\"recipient_type\":\"$1\",\"recipient_id\":\"$2\"}"
}

inbox() { # <type> <recipient id>
  echo "{\"recipient_type\":\"$1\",\"recipient_id\":\"$2\",\"limit\":10}"
}

cleanup_tokens() {
  internal_call "$NOTIFICATION_ADDR" "$NOTIFICATION_SVC/UnregisterDevice" "$(unregister "$TOKEN_DEVICE_R1")" > /dev/null 2>&1 || true
  internal_call "$NOTIFICATION_ADDR" "$NOTIFICATION_SVC/UnregisterDevice" "$(unregister "$TOKEN_DEVICE_DA")" > /dev/null 2>&1 || true
}

echo "==> [1/6] preparing: protoset, two riders, a driver, tokens"
buf build -o "$PROTOSET"

RIDER_1="$(ensure_rider "$RIDER_IDENTITY_1" "Authz Test Rider")"
RIDER_2="$(ensure_rider "$RIDER_IDENTITY_2" "Authz Test Rider Two")"

IDENTITY_DRIVER_A="$(internal_call "$DRIVER_ADDR" "$DRIVER_SVC/GetDriver" "{\"driver_id\":\"$DRIVER_A\"}" | json_field 'd["driver"]["identityId"]')"
[ -n "$IDENTITY_DRIVER_A" ] || { echo "FAIL: the test driver has no identity" >&2; exit 1; }

TOKEN_RIDER_1="$(mint "$RIDER_IDENTITY_1")"
TOKEN_RIDER_2="$(mint "$RIDER_IDENTITY_2")"
TOKEN_DRIVER_A="$(mint "$IDENTITY_DRIVER_A")"
TOKEN_EXPIRED="$(mint "$RIDER_IDENTITY_1" -ttl=-1h)"

cleanup_tokens

echo "==> [2/6] a user registers devices only for their own profile"
expect "rider 1 registers their own device"              OK               "$TOKEN_RIDER_1"  RegisterDevice "$(register $RDR "$RIDER_1" "$TOKEN_DEVICE_R1")"
expect "driver A registers their own device"             OK               "$TOKEN_DRIVER_A" RegisterDevice "$(register $DRV "$DRIVER_A" "$TOKEN_DEVICE_DA")"
expect "rider 2 registers a device for rider 1"          PermissionDenied "$TOKEN_RIDER_2"  RegisterDevice "$(register $RDR "$RIDER_1" "authz-test-hijack")"
expect "rider 1 registers a device for driver A"         PermissionDenied "$TOKEN_RIDER_1"  RegisterDevice "$(register $DRV "$DRIVER_A" "authz-test-hijack")"
expect "rider 1 registers under the driver type"         PermissionDenied "$TOKEN_RIDER_1"  RegisterDevice "$(register $DRV "$RIDER_1" "authz-test-hijack")"
expect "driver A registers under the rider type"         PermissionDenied "$TOKEN_DRIVER_A" RegisterDevice "$(register $RDR "$DRIVER_A" "authz-test-hijack")"
expect "an unspecified recipient type"                   PermissionDenied "$TOKEN_RIDER_1"  RegisterDevice "$(register RECIPIENT_TYPE_UNSPECIFIED "$RIDER_1" "authz-test-hijack")"
expect "an empty recipient id"                           PermissionDenied "$TOKEN_RIDER_1"  RegisterDevice "$(register $RDR "" "authz-test-hijack")"

echo "==> [3/6] a device is unregistered only by its owner"
expect "rider 2 unregisters rider 1's device"            PermissionDenied "$TOKEN_RIDER_2"  UnregisterDevice "$(unregister "$TOKEN_DEVICE_R1")"
expect "driver A unregisters rider 1's device"           PermissionDenied "$TOKEN_DRIVER_A" UnregisterDevice "$(unregister "$TOKEN_DEVICE_R1")"
expect "rider 1 unregisters driver A's device"           PermissionDenied "$TOKEN_RIDER_1"  UnregisterDevice "$(unregister "$TOKEN_DEVICE_DA")"
expect "a token nobody registered"                       PermissionDenied "$TOKEN_RIDER_1"  UnregisterDevice "$(unregister "authz-test-unknown")"
expect "an empty token"                                  PermissionDenied "$TOKEN_RIDER_1"  UnregisterDevice "$(unregister "")"

REMOVED="$(call "$TOKEN_RIDER_1" UnregisterDevice "$(unregister "$TOKEN_DEVICE_R1")" | json_field 'd.get("removed", False)')"
if [ "$REMOVED" = "True" ]; then
  printf '  ok    rider 1 removes their own device, and it was still there (the refused attempts removed nothing)\n'
else
  printf '  FAIL  rider 1 could not remove their own device (removed=%s): a refused attempt may have removed it\n' "$REMOVED"
  FAILURES=$((FAILURES + 1))
fi

expect "rider 1 unregisters the same device again"       PermissionDenied "$TOKEN_RIDER_1"  UnregisterDevice "$(unregister "$TOKEN_DEVICE_R1")"
expect "driver A unregisters their own device"           OK               "$TOKEN_DRIVER_A" UnregisterDevice "$(unregister "$TOKEN_DEVICE_DA")"

echo "==> [4/6] a user reads and marks only their own inbox"
expect "rider 1 lists their own notifications"           OK               "$TOKEN_RIDER_1"  ListNotifications "$(inbox $RDR "$RIDER_1")"
expect "driver A lists their own notifications"          OK               "$TOKEN_DRIVER_A" ListNotifications "$(inbox $DRV "$DRIVER_A")"
expect "rider 2 lists rider 1's notifications"           PermissionDenied "$TOKEN_RIDER_2"  ListNotifications "$(inbox $RDR "$RIDER_1")"
expect "driver A lists rider 1's notifications"          PermissionDenied "$TOKEN_DRIVER_A" ListNotifications "$(inbox $RDR "$RIDER_1")"
expect "rider 1 lists under the driver type"             PermissionDenied "$TOKEN_RIDER_1"  ListNotifications "$(inbox $DRV "$RIDER_1")"
expect_authorized "rider 1 marks one of their notifications read" "$TOKEN_RIDER_1" MarkAsRead "{\"recipient_type\":\"$RDR\",\"recipient_id\":\"$RIDER_1\",\"notification_ids\":[\"$MISSING_ID\"]}"
expect "rider 2 marks rider 1's notifications read"      PermissionDenied "$TOKEN_RIDER_2"  MarkAsRead "{\"recipient_type\":\"$RDR\",\"recipient_id\":\"$RIDER_1\"}"
expect "driver A marks rider 1's notifications read"     PermissionDenied "$TOKEN_DRIVER_A" MarkAsRead "{\"recipient_type\":\"$RDR\",\"recipient_id\":\"$RIDER_1\"}"

echo "==> [5/6] Send and UpsertTemplate are internal only"
SEND="{\"recipient_type\":\"$RDR\",\"recipient_id\":\"$RIDER_1\",\"event_key\":\"authz.test.does_not_exist\"}"
TEMPLATE='{"event_key":"authz.test.does_not_exist","translations":[{"locale":"en","title":"x","body":"x"}]}'
expect "rider 1 sends a notification"                    PermissionDenied "$TOKEN_RIDER_1"  Send           "$SEND"
expect "driver A sends a notification"                   PermissionDenied "$TOKEN_DRIVER_A" Send           "$SEND"
expect "rider 1 writes a template"                       PermissionDenied "$TOKEN_RIDER_1"  UpsertTemplate "$TEMPLATE"
expect "driver A writes a template"                      PermissionDenied "$TOKEN_DRIVER_A" UpsertTemplate "$TEMPLATE"
expect_authorized "the internal token sends (no such template, so nothing is sent)" "$INTERNAL_TOKEN" Send "$SEND"

echo "==> [6/6] bad credentials and the internal path"
expect "an expired token"                                Unauthenticated  "$TOKEN_EXPIRED" ListNotifications "$(inbox $RDR "$RIDER_1")"
expect "a malformed token"                               Unauthenticated  "not-a-token"    ListNotifications "$(inbox $RDR "$RIDER_1")"
expect "the internal token lists any rider's notifications" OK            "$INTERNAL_TOKEN" ListNotifications "$(inbox $RDR "$RIDER_2")"
expect "the internal token lists any driver's notifications" OK           "$INTERNAL_TOKEN" ListNotifications "$(inbox $DRV "$DRIVER_A")"
expect "the internal token unregisters a device"         OK               "$INTERNAL_TOKEN" UnregisterDevice "$(unregister "authz-test-unknown")"

cleanup_tokens

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: users manage only their own devices and inbox, devices are removed only by their owner, Send is internal, and the internal path is intact"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
