#!/usr/bin/env bash
# Sets the waiting and cancelling prices on every rate card of a STAGING instance,
# through the same staff API the admin web uses, so the Rider app's cancel sheet and
# "captain is here" card can be tried with real numbers. Run it on the server, from
# the repo root:
#   bash scripts/dev/set-trip-fees.sh [cancellation_fee] [no_show_fee] [waiting_per_minute] [no_show_wait_minutes]
# Defaults: 1000 2000 250 6. "bash scripts/dev/set-trip-fees.sh 0 0 0" takes them off.
# Every other price on the cards stays as it is (a new version of each card).
# It refuses to run unless instance/instance.env says IDENTITY_APP_ENV=test.
set -Euo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
[ "$(value IDENTITY_APP_ENV)" = test ] ||
  die "this instance is not staging (IDENTITY_APP_ENV=test); test prices have no place on a real one"
command -v python3 > /dev/null || die "python3 is needed"

CANCEL="${1:-1000}"; NO_SHOW="${2:-2000}"; PER_MINUTE="${3:-250}"; WAIT="${4:-6}"
PHONE="${SIM_STAFF_PHONE:-+9647700000991}"
for n in "$CANCEL" "$NO_SHOW" "$PER_MINUTE"; do
  echo "$n" | grep -Eq '^[0-9]+$' || die "amounts are whole numbers, for example 1000"
done
echo "$WAIT" | grep -Eq '^[0-9]+$' && [ "$WAIT" -ge 1 ] && [ "$WAIT" -le 30 ] || die "the no-show wait is 1 to 30 minutes"

# The test staff member, signed in, with the owner role (it holds pricing.manage).
# shellcheck source=scripts/dev/sim-staff-lib.sh
source "$(dirname "$0")/sim-staff-lib.sh"

call GET /v1/admin/rate-cards
[ "$STATUS" = 200 ] || die "could not list the rate cards ($STATUS): $BODY"

# Each card in force again, with the new fees and everything else unchanged.
BODIES="$(echo "$BODY" | python3 -c '
import json, sys
cancel, no_show, per_minute, wait = sys.argv[1:5]
keep = ["zoneId", "cityId", "vehicleClass", "baseFare", "perKmRate", "perMinuteRate", "minimumFare",
        "freeWaitingMinutes", "cancellationGraceMinutes", "maxSurgePercent", "demandSurge", "weatherSurge"]
for card in json.load(sys.stdin).get("rateCards", []):
    body = {k: card[k] for k in keep if k in card}
    body.update(cancellationFee=cancel, noShowFee=no_show, waitingPerMinute=per_minute, noShowWaitMinutes=int(wait))
    print(json.dumps(body))
' "$CANCEL" "$NO_SHOW" "$PER_MINUTE" "$WAIT")"
[ -n "$BODIES" ] || die "no rate cards in: $BODY"

while IFS= read -r body; do
  call POST /v1/admin/rate-cards "$body"
  [ "$STATUS" = 200 ] || die "a card was refused ($STATUS): $BODY"
  echo "ok    $(echo "$BODY" | field rateCard.vehicleClass | sed 's/^$/every class/'):" \
    "cancel $CANCEL, no-show $NO_SHOW after $WAIT min, waiting $PER_MINUTE/min"
done <<< "$BODIES"
