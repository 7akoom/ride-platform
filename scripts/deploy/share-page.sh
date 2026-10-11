#!/usr/bin/env bash
# Puts the page a shared-trip link opens on the API's domain, and has trip-service
# hand out links to it: https://<api domain>/t/#<token> (the token stays in the
# fragment, out of every server log). Run on the server, from the repo root, after
# nginx-site.sh and prepare-tiles.sh (the page draws the instance's own map):
#   bash scripts/deploy/share-page.sh
# It asks for sudo once, to add the page to the nginx site. Run it again after a pull
# that changed web/share/index.html; nothing else changes then.
set -Eeuo pipefail
trap 'echo "FAIL: share-page stopped at line $LINENO" >&2' ERR

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
API="$(value API_DOMAIN)"; TILES="$(value TILES_DOMAIN)"; DIR="$(value TILES_DIR)"
[ -n "$API" ] || die "no API_DOMAIN in instance/instance.env"
[ -n "$TILES" ] && [ -w "$DIR" ] || die "the map comes first: scripts/deploy/prepare-tiles.sh"
SITE=/etc/nginx/sites-available/ride-platform
[ -f "$SITE" ] || die "no nginx site yet: sudo bash scripts/deploy/nginx-site.sh"

echo "==> the page"
mkdir -p "$DIR/share"
sed "s#__TILES_ORIGIN__#https://$TILES#g" web/share/index.html > "$DIR/share/index.html.next"
mv -f "$DIR/share/index.html.next" "$DIR/share/index.html"
chmod 755 "$DIR/share"; chmod 644 "$DIR/share/index.html"

echo "==> nginx: https://$API/t/"
if grep -q "ride-share-page" "$SITE"; then
  echo "  the site has it already"
else
  sudo cp -p "$SITE" "$SITE.before-share-page"
  sudo python3 scripts/tools/add-share-location.py "$SITE" "$API" "$DIR/share"
  if ! sudo nginx -t; then
    sudo mv -f "$SITE.before-share-page" "$SITE"
    die "nginx refused the change; the site is back as it was"
  fi
  sudo systemctl reload nginx
fi

echo "==> trip-service hands out links to the page"
BASE="https://$API/t/#"
touch instance/trip-service.env
{ grep -v '^TRIP_SHARE_URL_BASE=' instance/trip-service.env || true; echo "TRIP_SHARE_URL_BASE=$BASE"; } > instance/trip-service.env.next
mv -f instance/trip-service.env.next instance/trip-service.env
docker compose -f infrastructure/deploy/compose.vps.yaml --env-file instance/instance.env up -d trip-service

echo "==> checks"
code="$(curl -s -o /dev/null -w '%{http_code}' "https://$API/t/" || true)"
[ "$code" = 200 ] || die "https://$API/t/ answered $code, not 200"
curl -s "https://$API/t/" | grep -q "https://$TILES/styles/" || die "the page does not point at the map server"
echo "PASS: shared trips open at $BASE<token>"
