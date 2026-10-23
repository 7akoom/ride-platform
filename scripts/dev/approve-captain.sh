#!/usr/bin/env bash
# Reviews a captain on a STAGING instance the way the operator will from the admin web,
# so the Captain app's review screens (approved, rejected) can be tried before it exists.
# Run it on the server, from the repo root:
#   bash scripts/dev/approve-captain.sh 07700000995
#   bash scripts/dev/approve-captain.sh 07700000995 --reject "رقم اللوحة لا يطابق السنوية"
# Approving accepts every document waiting for review (as handed in), then the account
# (its first car goes with it). Rejecting turns the account down with the reason, which
# the app shows. It acts as the test staff member of make-voucher.sh (same session,
# owner role) and refuses to run unless instance/instance.env says IDENTITY_APP_ENV=test.
set -Euo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
[ "$(value IDENTITY_APP_ENV)" = test ] || die "this instance is not staging (IDENTITY_APP_ENV=test)"
command -v python3 > /dev/null || die "python3 is needed"

CAPTAIN=""; REASON=""; REJECT=false
while [ $# -gt 0 ]; do
  case "$1" in
    --reject) REJECT=true; shift; REASON="${1:-}" ;;
    *) CAPTAIN="$1" ;;
  esac
  shift
done
[ -n "$CAPTAIN" ] || die 'usage: approve-captain.sh <phone> [--reject "reason"]'
if $REJECT && [ -z "$REASON" ]; then die "--reject needs the reason the captain will read"; fi

# The captain's phone as identity-service keeps it: +964 and ten digits starting with 7.
DIGITS="$(echo "$CAPTAIN" | tr -cd '0-9')"
DIGITS="${DIGITS#00}"; DIGITS="${DIGITS#964}"; DIGITS="${DIGITS#0}"
echo "$DIGITS" | grep -Eq '^7[0-9]{9}$' || die "not an Iraqi mobile number: $CAPTAIN"
CAPTAIN="+964$DIGITS"

PHONE="${SIM_STAFF_PHONE:-+9647700000991}"
[ "$CAPTAIN" != "$PHONE" ] || die "$CAPTAIN is the test staff member, not a captain"

# shellcheck source=scripts/dev/sim-staff-lib.sh
source "$(dirname "$0")/sim-staff-lib.sh"

db() { # <database variable> <sql>: one value per line
  docker exec ride-postgres sh -c "psql -U ride_admin -d \"\$$1\" -t -A -q -v ON_ERROR_STOP=1 -c \"$2\""
}

IDENTITY_OF_CAPTAIN="$(db IDENTITY_DB_NAME "select identity_id from identity_identifiers where normalized_value = '$CAPTAIN'")"
[ -n "$IDENTITY_OF_CAPTAIN" ] || die "nobody signed in with $CAPTAIN"
DRIVER="$(db DRIVER_DB_NAME "select id from drivers where identity_id = '$IDENTITY_OF_CAPTAIN'")"
[ -n "$DRIVER" ] || die "$CAPTAIN has not registered as a captain"

if $REJECT; then
  REASON_JSON="$(python3 -c 'import json, sys; print(json.dumps(sys.argv[1]))' "$REASON")"
  call POST "/v1/admin/drivers/$DRIVER:reject" "{\"reason\":$REASON_JSON}"
  [ "$STATUS" = 200 ] || die "the account could not be rejected ($STATUS): $BODY"
  echo "==> $CAPTAIN rejected: $REASON"
  exit 0
fi

# Every document waiting for review, the captain's own and the car's.
call GET "/v1/drivers/$DRIVER/documents"
[ "$STATUS" = 200 ] || die "the documents could not be read ($STATUS): $BODY"
PENDING="$(echo "$BODY" | python3 -c '
import json, sys
for r in json.load(sys.stdin).get("requirements", []):
    doc = r.get("pending") or {}
    if doc.get("id"):
        print(doc["id"] + " " + r.get("type", {}).get("code", ""))')"

while read -r doc code; do
  [ -n "$doc" ] || continue
  call POST "/v1/admin/driver-documents/$doc:approve" '{}'
  [ "$STATUS" = 200 ] || die "the document $code could not be approved ($STATUS): $BODY"
  echo "  approved $code"
done <<< "$PENDING"

call POST "/v1/admin/drivers/$DRIVER:approve" '{}'
[ "$STATUS" = 200 ] || die "the account could not be approved ($STATUS): $BODY"
echo "==> $CAPTAIN approved: $(echo "$BODY" | field driver.displayName)"
