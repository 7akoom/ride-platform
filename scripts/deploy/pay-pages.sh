#!/usr/bin/env bash
# Puts the pages a payment provider sends the customer back to on the API's domain
# (https://<api domain>/pay/done/ and /pay/failed/), and has wallet-service give
# ZainCash those addresses. Run on the server, from the repo root, after
# nginx-site.sh and prepare-tiles.sh, and after deploy.sh brought wallet-service
# with GetTopUpOptions (5c-1a):
#   bash scripts/deploy/pay-pages.sh
# It asks for sudo once, to add the pages to the nginx site. Run it again after a
# pull that changed web/pay/index.html, or after setting ZainCash's credentials in
# instance/wallet-service.env (it restarts wallet-service).
#
# ZainCash itself is on only with ZAINCASH_CLIENT_ID and ZAINCASH_CLIENT_SECRET in
# instance/wallet-service.env (the test ones from docs.zaincash.iq > Testing > Test
# Credentials on staging; the merchant's own on a real instance), and
# ZAINCASH_WEBHOOK_SECRET is ZainCash's API key. This script never writes those.
set -Eeuo pipefail
trap 'echo "FAIL: pay-pages stopped at line $LINENO" >&2' ERR

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }
wallet_value() { sed -n "s/^$1=//p" instance/wallet-service.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
API="$(value API_DOMAIN)"; DIR="$(value TILES_DIR)"
[ -n "$API" ] || die "no API_DOMAIN in instance/instance.env"
[ -n "$DIR" ] && [ -w "$DIR" ] || die "the map's folder comes first: scripts/deploy/prepare-tiles.sh"
SITE=/etc/nginx/sites-available/ride-platform
[ -f "$SITE" ] || die "no nginx site yet: sudo bash scripts/deploy/nginx-site.sh"
[ -f web/pay/index.html ] || die "no web/pay/index.html: git pull first"

echo "==> the page"
mkdir -p "$DIR/pay"
cp web/pay/index.html "$DIR/pay/index.html.next"
mv -f "$DIR/pay/index.html.next" "$DIR/pay/index.html"
chmod 755 "$DIR/pay"; chmod 644 "$DIR/pay/index.html"

echo "==> nginx: https://$API/pay/"
if grep -q "ride-pay-pages" "$SITE"; then
  echo "  the site has it already"
else
  sudo cp -p "$SITE" "$SITE.before-pay-pages"
  sudo python3 scripts/tools/add-pay-location.py "$SITE" "$API" "$DIR/pay"
  if ! sudo nginx -t; then
    sudo mv -f "$SITE.before-pay-pages" "$SITE"
    die "nginx refused the change; the site is back as it was"
  fi
  sudo systemctl reload nginx
fi

echo "==> wallet-service sends ZainCash's customers back to the pages"
touch instance/wallet-service.env
chmod 600 instance/wallet-service.env
OLD_BASE="https://test.zaincash.iq"
{
  grep -v -e '^ZAINCASH_SUCCESS_URL=' -e '^ZAINCASH_FAILURE_URL=' instance/wallet-service.env |
    sed "s#^ZAINCASH_BASE_URL=$OLD_BASE/*\$#ZAINCASH_BASE_URL=https://pg-api-uat.zaincash.iq#" || true
  echo "ZAINCASH_SUCCESS_URL=https://$API/pay/done/"
  echo "ZAINCASH_FAILURE_URL=https://$API/pay/failed/"
} > instance/wallet-service.env.next
chmod 600 instance/wallet-service.env.next
mv -f instance/wallet-service.env.next instance/wallet-service.env
echo "  ZainCash at $(wallet_value ZAINCASH_BASE_URL)"
if [ -z "$(wallet_value ZAINCASH_CLIENT_ID)" ] || [ -z "$(wallet_value ZAINCASH_CLIENT_SECRET)" ]; then
  echo "  NOTE  ZAINCASH_CLIENT_ID / ZAINCASH_CLIENT_SECRET are empty: top-ups through ZainCash stay off (vouchers work)."
fi
if [ -z "$(wallet_value ZAINCASH_WEBHOOK_SECRET)" ]; then
  echo "  NOTE  ZAINCASH_WEBHOOK_SECRET (ZainCash's API key) is empty: the pages cannot settle a top-up at once;"
  echo "        wallet-service still settles it by asking ZainCash (when the app asks, and every 2 minutes)."
fi
docker compose -f infrastructure/deploy/compose.vps.yaml --env-file instance/instance.env up -d --force-recreate wallet-service

echo "==> checks"
for page in done failed; do
  code="$(curl -s -o /dev/null -w '%{http_code}' "https://$API/pay/$page/" || true)"
  [ "$code" = 200 ] || die "https://$API/pay/$page/ answered $code, not 200"
done
curl -s "https://$API/pay/done/" | grep -q "/v1/wallet/zaincash/webhook" || die "https://$API/pay/done/ is not the payment page"
sleep 5
state="$(docker logs --since 2m ride-wallet-service 2>&1 | grep -o 'top-ups through ZainCash are [a-z]*' | tail -1 || true)"
[ -n "$state" ] || die "wallet-service did not say whether ZainCash is on (is it the 5c-1a build? run deploy.sh)"
echo "  wallet-service: $state"
echo "PASS: ZainCash sends customers back to https://$API/pay/done/ and /pay/failed/"
