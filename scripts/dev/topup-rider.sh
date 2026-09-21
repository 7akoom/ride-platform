#!/usr/bin/env bash
# Adds test money to a rider's wallet, so paying from the wallet can be tried in the Rider app
# (the backend has no way yet for a rider to add money to their own wallet).
# Run from the ride-platform repo root:
#   bash scripts/dev/topup-rider.sh "<the name you gave in the app>" [amount]
# Example: bash scripts/dev/topup-rider.sh "احمد كريم" 20000
set -uo pipefail
export LC_ALL=C.UTF-8

TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"
WALLET_ADDR="localhost:50058"

NAME="${1:-}"
AMOUNT="${2:-20000}"

if [ -z "$NAME" ]; then
  echo "usage: bash scripts/dev/topup-rider.sh \"<rider name>\" [amount]" >&2
  echo "the riders that exist:" >&2
  echo "select display_name from riders order by created_at desc limit 20;" \
    | docker exec -i ride-rider-postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A' >&2
  exit 1
fi

case "$AMOUNT" in
  '' | *[!0-9]*) echo "the amount must be a whole number, for example 20000" >&2; exit 1 ;;
esac

NAME_SQL="${NAME//\'/\'\'}"

RIDER_ID="$(echo "select id from riders where display_name = '$NAME_SQL' order by created_at desc limit 1;" \
  | docker exec -i ride-rider-postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A')"

if [ -z "$RIDER_ID" ]; then
  echo "no rider is called \"$NAME\". The riders that exist:" >&2
  echo "select display_name from riders order by created_at desc limit 20;" \
    | docker exec -i ride-rider-postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A' >&2
  exit 1
fi

buf build -o "$PROTOSET" || { echo "buf build failed: run this from the repo root" >&2; exit 1; }

grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $TOKEN" \
  -d "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER_ID\",\"amount\":\"$AMOUNT\",\"idempotency_key\":\"dev-topup-$(date +%s)\",\"description\":\"test money\"}" \
  "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp > /dev/null || { echo "the top-up failed" >&2; exit 1; }

echo "added $AMOUNT to the wallet of \"$NAME\" ($RIDER_ID). Balance now:"
grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $TOKEN" \
  -d "{\"owner_type\":\"OWNER_TYPE_RIDER\",\"owner_id\":\"$RIDER_ID\"}" \
  "$WALLET_ADDR" ride.wallet.v1.WalletService/GetWallet | grep balance
