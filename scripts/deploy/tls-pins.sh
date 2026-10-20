#!/usr/bin/env bash
# Certificate pinning for the apps: prints the API_PINS line their release builds take.
# Run on the server, from the repo root, after nginx-site.sh got the certificate:
#   sudo bash scripts/deploy/tls-pins.sh            # set up (once), then print the pins
#   sudo bash scripts/deploy/tls-pins.sh --rotate   # move the server to the backup key
#
# The apps trust only the server's key (its SHA-256 pin), not the certificate
# authorities. So:
#   - certbot must renew with the same key: this sets reuse_key in its renewal file;
#   - a backup key waits in instance/tls-backup-key.pem (back it up with instance/);
#     its pin ships in the apps too, so the server can move to it (--rotate) if the
#     current key leaks, without breaking the apps already installed.
# After --rotate, build the apps again with the new line (the new current key and a
# new backup) before any further rotation.
set -Eeuo pipefail

[ -f instance/instance.env ] || { echo "run scripts/deploy/init-instance.sh first" >&2; exit 1; }
[ "$(id -u)" = 0 ] || { echo "run it with sudo (certbot's files belong to root)" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

API="$(value API_DOMAIN)"; FILES="$(value FILES_DOMAIN)"
RENEWAL="/etc/letsencrypt/renewal/$API.conf"
LIVE="/etc/letsencrypt/live/$API"
BACKUP=instance/tls-backup-key.pem
OWNER="${SUDO_USER:-root}"

[ -f "$RENEWAL" ] && [ -f "$LIVE/cert.pem" ] || {
  echo "no certbot certificate named $API: run scripts/deploy/nginx-site.sh first" >&2; exit 1; }

pin_of_cert() { openssl x509 -pubkey -noout | openssl pkey -pubin -outform der | openssl dgst -sha256 -binary | base64; }
pin_of_key() { openssl pkey -in "$1" -pubout -outform der | openssl dgst -sha256 -binary | base64; }
live_pin() {
  openssl s_client -connect "$1:443" -servername "$1" < /dev/null 2> /dev/null | pin_of_cert 2> /dev/null || true
}
new_backup() {
  openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$BACKUP"
  chmod 600 "$BACKUP"; chown "$OWNER" "$BACKUP"
  echo "new backup key: $BACKUP (copy instance/ off the server again)"
}

# Renewals keep the key (and so the pin).
if ! grep -q '^reuse_key *= *True' "$RENEWAL"; then
  sed -i '/^reuse_key *=/d' "$RENEWAL"
  sed -i 's/^\[renewalparams\]$/[renewalparams]\nreuse_key = True/' "$RENEWAL"
  grep -q '^reuse_key = True' "$RENEWAL" || { echo "could not set reuse_key in $RENEWAL" >&2; exit 1; }
  echo "certbot renews $API with the same key from now on."
fi

[ -f "$BACKUP" ] || new_backup

if [ "${1:-}" = "--rotate" ]; then
  KEY="$(readlink -f "$LIVE/privkey.pem")"
  cp -p "$KEY" "instance/tls-old-key-$(date +%Y%m%d%H%M%S).pem"
  # certbot reuses the lineage's current key: put the backup there and renew now.
  install -m 600 "$BACKUP" "$KEY"
  certbot renew --cert-name "$API" --force-renewal --reuse-key
  systemctl reload nginx
  new_backup
fi

CURRENT="$(pin_of_cert < "$LIVE/cert.pem")"
SPARE="$(pin_of_key "$BACKUP")"

# What a phone sees: a proxy in front (Cloudflare's orange cloud) shows its own key.
for host in "$API" "$FILES"; do
  seen="$(live_pin "$host")"
  if [ "$seen" != "$CURRENT" ]; then
    echo "https://$host shows another key (${seen:-no answer}), not this server's ($CURRENT)." >&2
    echo "Behind a proxy (Cloudflare: set the record to DNS only) the apps could not connect." >&2
    exit 1
  fi
done

echo
echo "Build the apps with:"
echo "  --dart-define=API_PINS=$CURRENT,$SPARE"
echo "API_PINS=$CURRENT,$SPARE" > instance/app-pins.env
chown "$OWNER" instance/app-pins.env
