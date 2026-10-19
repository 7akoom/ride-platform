#!/usr/bin/env bash
# Issues test vouchers on a STAGING instance, so the Rider app's "use a voucher" can be
# tried with a real code. Run it on the server, from the repo root:
#   bash scripts/dev/make-voucher.sh [amount] [count]
# Example: bash scripts/dev/make-voucher.sh 10000 3
#
# Vouchers are staff business (vouchers.manage). The first run makes a test staff member:
# it signs in as SIM_STAFF_PHONE (+9647700000991 by default) with the code
# identity-service writes to its log on staging, and gives that identity the owner role
# straight in the staff database. instance/sim-staff.env keeps it for the next runs.
# Then it issues a batch through the gateway (seller STAGING, valid 30 days), exports it
# so the codes work, and prints them.
# It refuses to run unless instance/instance.env says IDENTITY_APP_ENV=test.
set -Euo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
[ "$(value IDENTITY_APP_ENV)" = test ] ||
  die "this instance is not staging (IDENTITY_APP_ENV=test); test vouchers have no place on a real one"
command -v python3 > /dev/null || die "python3 is needed"

AMOUNT="${1:-10000}"; COUNT="${2:-1}"; PHONE="${SIM_STAFF_PHONE:-+9647700000991}"
echo "$AMOUNT" | grep -Eq '^[1-9][0-9]*$' || die "the amount is a whole number, for example 10000"
echo "$COUNT" | grep -Eq '^[1-9][0-9]?$' && [ "$COUNT" -le 20 ] || die "the count is 1 to 20"

# The test staff member, signed in, with the owner role (the one that holds vouchers.manage).
# shellcheck source=scripts/dev/sim-staff-lib.sh
source "$(dirname "$0")/sim-staff-lib.sh"

RUN="$(date +%s)"
EXPIRES="$(python3 -c 'import datetime as t; print((t.datetime.now(t.timezone.utc) + t.timedelta(days=30)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
call POST /v1/admin/voucher-batches \
  "{\"label\":\"STAGING test $RUN\",\"seller\":\"STAGING\",\"amount\":\"$AMOUNT\",\"quantity\":$COUNT,\"expiresAt\":\"$EXPIRES\",\"idempotencyKey\":\"sim-staff-$RUN\"}"
[ "$STATUS" = 200 ] || die "the batch was refused ($STATUS): $BODY"
BATCH="$(echo "$BODY" | field batch.id)"
[ -n "$BATCH" ] || die "no batch id in: $BODY"

call POST "/v1/admin/voucher-batches/$BATCH:export" '{}'
[ "$STATUS" = 200 ] || die "the batch could not be exported ($STATUS): $BODY"

echo "==> $COUNT voucher(s) of $AMOUNT, valid until $EXPIRES:"
echo "$BODY" | python3 -c '
import json, sys
for v in json.load(sys.stdin).get("vouchers", []):
    print("  " + v["code"] + "   (" + v["serial"] + ")")'
