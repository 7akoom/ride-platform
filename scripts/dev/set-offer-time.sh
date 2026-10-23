#!/usr/bin/env bash
# Sets how long a captain has to accept a trip offer on a STAGING instance, the way the
# operator will from the admin web (dispatch settings), so it can be tried before that
# exists. Run it on the server, from the repo root:
#   bash scripts/dev/set-offer-time.sh 20     a captain has 20 seconds (5-60)
#   bash scripts/dev/set-offer-time.sh 0      back to the instance's DISPATCH_OFFER_TTL
#   bash scripts/dev/set-offer-time.sh        shows the settings as they are
# dispatch-service picks a change up within 30 seconds. The next-trip settings are kept.
# It acts as the test staff member of make-voucher.sh (same session, owner role) and
# refuses to run unless instance/instance.env says IDENTITY_APP_ENV=test.
set -Euo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
[ "$(value IDENTITY_APP_ENV)" = test ] || die "this instance is not staging (IDENTITY_APP_ENV=test)"
command -v python3 > /dev/null || die "python3 is needed"

SECONDS_WANTED="${1:-}"
if [ -n "$SECONDS_WANTED" ]; then
  echo "$SECONDS_WANTED" | grep -Eq '^[0-9]+$' || die "usage: set-offer-time.sh [seconds: 0, or 5-60]"
fi

PHONE="${SIM_STAFF_PHONE:-+9647700000991}"
# shellcheck source=scripts/dev/sim-staff-lib.sh
source "$(dirname "$0")/sim-staff-lib.sh"

call GET /v1/admin/dispatch-settings
[ "$STATUS" = 200 ] || die "the dispatch settings could not be read ($STATUS): $BODY"

show() {
  echo "$1" | python3 -c '
import json, sys
s = json.load(sys.stdin).get("settings", {})
offer = s.get("offerSeconds", 0)
print("  offer time:  " + (str(offer) + " s" if offer else "the instance default (DISPATCH_OFFER_TTL)"))
print("  next trips:  " + ("on" if s.get("nextTripsEnabled") else "off") + ", within " + str(s.get("nextTripRadiusMeters", 0)) + " m")'
}

if [ -z "$SECONDS_WANTED" ]; then
  show "$BODY"
  exit 0
fi

UPDATE="$(echo "$BODY" | python3 -c '
import json, sys
s = json.load(sys.stdin).get("settings", {})
print(json.dumps({
    "nextTripsEnabled": s.get("nextTripsEnabled", False),
    "nextTripRadiusMeters": s.get("nextTripRadiusMeters", 3000),
    "offerSeconds": int(sys.argv[1]),
}))' "$SECONDS_WANTED")"

call PUT /v1/admin/dispatch-settings "$UPDATE"
[ "$STATUS" = 200 ] || die "the settings were refused ($STATUS): $BODY"
echo "==> saved (dispatch uses it within 30 s):"
show "$BODY"
