#!/usr/bin/env bash
# Writes the nginx site for the map (a site of its own, next to the API one)
# and gets its TLS certificate. Run on the server, from the repo root, once:
#   sudo bash scripts/deploy/nginx-tiles-site.sh --domain ride-tiles.example.com [--no-certbot]
# It records TILES_DOMAIN and TILES_DIR in instance/instance.env, makes the
# folder nginx serves (owned by you, so prepare-tiles.sh needs no sudo), and
# never touches the other sites; nginx is tested before it is reloaded.
set -Eeuo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }
[ "$(id -u)" = 0 ] || die "run it with sudo"
OWNER="${SUDO_USER:-}"
[ -n "$OWNER" ] && [ "$OWNER" != root ] || die "run it with sudo from your own user (the folder will be yours)"
[ -f instance/instance.env ] || die "run scripts/deploy/init-instance.sh first"
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

DOMAIN="$(value TILES_DOMAIN)"; CERTBOT=1
while [ $# -gt 0 ]; do
  case "$1" in
    --domain) DOMAIN="$2"; shift 2 ;;
    --no-certbot) CERTBOT=0; shift ;;
    *) die "unknown option $1" ;;
  esac
done
[ -n "$DOMAIN" ] || die "--domain ride-tiles.<your domain> is needed the first time"
echo "$DOMAIN" | grep -Eq '^[a-z0-9.-]+$' || die "$DOMAIN is not a domain name"
DIR="$(value TILES_DIR)"; DIR="${DIR:-/var/www/ride-tiles}"

if [ -z "$(value TILES_DOMAIN)" ]; then
  printf '\nTILES_DOMAIN=%s\nTILES_DIR=%s\n' "$DOMAIN" "$DIR" >> instance/instance.env
  echo "recorded TILES_DOMAIN=$DOMAIN and TILES_DIR=$DIR in instance/instance.env"
elif [ "$(value TILES_DOMAIN)" != "$DOMAIN" ]; then
  die "instance.env says TILES_DOMAIN=$(value TILES_DOMAIN); change it there first if that is intended"
fi

mkdir -p "$DIR"
chown "$OWNER": "$DIR"
chmod 755 "$DIR"

SITE=/etc/nginx/sites-available/ride-platform-tiles
if [ -f "$SITE" ] && grep -q "managed by Certbot" "$SITE"; then
  echo "$SITE already has a certificate from certbot; leaving it as it is (remove it to start over)."
else
  sed -e "s#__TILES_DOMAIN__#$DOMAIN#g" -e "s#__TILES_DIR__#$DIR#g" \
    infrastructure/deploy/nginx/ride-tiles.conf.template > "$SITE"
  ln -sf "$SITE" /etc/nginx/sites-enabled/ride-platform-tiles
  nginx -t
  systemctl reload nginx
  echo "nginx serves $DOMAIN over http from $DIR."
fi

if [ "$CERTBOT" = 1 ] && ! grep -q "managed by Certbot" "$SITE"; then
  command -v certbot > /dev/null || die "certbot is not installed: apt install certbot python3-certbot-nginx"
  certbot --nginx --non-interactive --agree-tos --redirect --register-unsafely-without-email -d "$DOMAIN"
  nginx -t && systemctl reload nginx
  echo "TLS is on for $DOMAIN."
fi
echo "next: bash scripts/deploy/prepare-tiles.sh   (without sudo)"
