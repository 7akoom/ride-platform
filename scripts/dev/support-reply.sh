#!/usr/bin/env bash
# Answers a support ticket as the support team on a STAGING instance, so the apps'
# tickets (a reply, its push, a resolved ticket and its rating) can be tried without
# the admin web. Run it on the server, from the repo root:
#   bash scripts/dev/support-reply.sh "message" [--resolve] [--ticket S-000123]
# Without --ticket it answers the ticket with the latest activity that is not resolved
# or closed. --resolve marks it resolved with the reply (the rider can then rate it).
# It acts as the test staff member of make-voucher.sh (same session, owner role).
# It refuses to run unless instance/instance.env says IDENTITY_APP_ENV=test.
set -Euo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
[ "$(value IDENTITY_APP_ENV)" = test ] || die "this instance is not staging (IDENTITY_APP_ENV=test)"
command -v python3 > /dev/null || die "python3 is needed"

MESSAGE=""; RESOLVE=false; NUMBER=""
while [ $# -gt 0 ]; do
  case "$1" in
    --resolve) RESOLVE=true ;;
    --ticket) shift; NUMBER="${1:-}" ;;
    *) MESSAGE="$1" ;;
  esac
  shift
done
[ -n "$MESSAGE" ] || die 'usage: support-reply.sh "message" [--resolve] [--ticket S-000123]'
PHONE="${SIM_STAFF_PHONE:-+9647700000991}"

# shellcheck source=scripts/dev/sim-staff-lib.sh
source "$(dirname "$0")/sim-staff-lib.sh"

# The open tickets of both queues: safety tickets (safety, accidents) have their own.
QUEUE="[]"
for path in /v1/admin/support/tickets /v1/admin/support/safety-tickets; do
  call GET "$path?pageSize=100"
  [ "$STATUS" = 200 ] || die "the queue $path could not be read ($STATUS): $BODY"
  QUEUE="$(printf '%s\n%s' "$QUEUE" "$BODY" | python3 -c '
import json, sys
first, second = sys.stdin.read().split("\n", 1)
print(json.dumps(json.loads(first) + json.loads(second).get("tickets", [])))')"
done
PICKED="$(echo "$QUEUE" | python3 -c '
import json, sys
number = sys.argv[1]
tickets = [t.get("ticket", {}) for t in json.load(sys.stdin)]
if number:
    tickets = [t for t in tickets if t.get("number") == number]
tickets.sort(key=lambda t: t.get("lastMessageAt") or t.get("createdAt") or "", reverse=True)
if tickets:
    print(tickets[0]["id"] + " " + tickets[0].get("number", ""))' "$NUMBER")"
[ -n "$PICKED" ] || die "no open ticket${NUMBER:+ numbered $NUMBER} in the queues (open one in the app first; resolved and closed ones are not listed)"
ID="${PICKED%% *}"; SHOWN="${PICKED#* }"

STATUS_AFTER="TICKET_STATUS_WAITING_USER"
if $RESOLVE; then STATUS_AFTER="TICKET_STATUS_RESOLVED"; fi
JSON="$(python3 -c 'import json, sys; print(json.dumps({"body": sys.argv[1], "setStatus": sys.argv[2]}))' "$MESSAGE" "$STATUS_AFTER")"

call POST "/v1/admin/support/tickets/$ID/messages" "$JSON"
[ "$STATUS" = 200 ] || die "the reply was refused ($STATUS): $BODY"
echo "==> answered $SHOWN ($STATUS_AFTER): the rider gets a push and sees the reply in the ticket"
