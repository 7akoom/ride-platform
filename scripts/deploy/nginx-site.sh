#!/usr/bin/env bash
# Writes the nginx site for this instance and gets its TLS certificates.
# Run on the server, from the repo root, after init-instance.sh:
#   sudo bash scripts/deploy/nginx-site.sh [--no-certbot]
# It never touches the other sites; nginx is tested before it is reloaded.
set -Eeuo pipefail

[ -f instance/instance.env ] || { echo "run scripts/deploy/init-instance.sh first" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

API="$(value API_DOMAIN)"; FILES="$(value FILES_DOMAIN)"
GATEWAY_PORT="$(value GATEWAY_PORT)"; FILES_PORT="$(value FILES_PORT)"
SITE=/etc/nginx/sites-available/ride-platform

sed -e "s/__API_DOMAIN__/$API/g" -e "s/__FILES_DOMAIN__/$FILES/g" \
    -e "s/__GATEWAY_PORT__/${GATEWAY_PORT:-18080}/g" -e "s/__FILES_PORT__/${FILES_PORT:-18333}/g" \
    infrastructure/deploy/nginx/ride-platform.conf.template > "$SITE.new"

if [ -f "$SITE" ] && grep -q "managed by Certbot" "$SITE"; then
  echo "$SITE already has certificates from certbot; leaving it as it is (remove it to start over)."
  rm -f "$SITE.new"
else
  mv "$SITE.new" "$SITE"
  ln -sf "$SITE" /etc/nginx/sites-enabled/ride-platform
  nginx -t
  systemctl reload nginx
  echo "nginx serves $API and $FILES over http."
fi

if [ "${1:-}" != "--no-certbot" ] && ! grep -q "managed by Certbot" "$SITE"; then
  command -v certbot > /dev/null || { echo "certbot is not installed: apt install certbot python3-certbot-nginx" >&2; exit 1; }
  certbot --nginx --non-interactive --agree-tos --redirect --register-unsafely-without-email -d "$API" -d "$FILES"
  nginx -t && systemctl reload nginx
  echo "TLS is on for $API and $FILES."
fi
