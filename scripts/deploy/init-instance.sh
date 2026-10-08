#!/usr/bin/env bash
# Makes instance/ for one deployment: every secret, the access-token key pair
# and the per-instance settings. Run once on the server, from the repo root:
#   bash scripts/deploy/init-instance.sh --api ride-api.example.com --files ride-files.example.com \
#        --owner-email you@example.com [--admin-origin https://ride-admin.example.com] \
#        [--time-zone Asia/Baghdad] [--staging]
#
# --owner-email: staff-service invites this address as the first owner (only
# while there is no staff at all); sign in, link that email, accept.
#
# --staging: login codes are written to identity-service's log instead of
# being sent (until the SMS provider is set up, P13). Never on a real launch.
#
# It refuses to overwrite an existing instance/ (that would lock everyone out
# and make every voucher code unusable). instance/ is git-ignored: back it up
# somewhere safe, it is the only copy of these secrets.
set -Eeuo pipefail

API=""; FILES=""; ADMIN=""; OWNER=""; TZ_NAME="Asia/Baghdad"; STAGING=0
while [ $# -gt 0 ]; do
  case "$1" in
    --api) API="$2"; shift 2 ;;
    --files) FILES="$2"; shift 2 ;;
    --admin-origin) ADMIN="$2"; shift 2 ;;
    --owner-email) OWNER="$2"; shift 2 ;;
    --time-zone) TZ_NAME="$2"; shift 2 ;;
    --staging) STAGING=1; shift ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done

[ -n "$API" ] && [ -n "$FILES" ] && [ -n "$OWNER" ] || { echo "usage: $0 --api <domain> --files <domain> --owner-email <email> [--admin-origin <url>] [--time-zone <IANA>] [--staging]" >&2; exit 2; }
case "$OWNER" in *@*.*) ;; *) echo "--owner-email is not an email address" >&2; exit 2 ;; esac
[ -f go.work ] && [ -f infrastructure/deploy/compose.vps.yaml ] || { echo "run this from the repo root" >&2; exit 2; }
[ ! -e instance ] || { echo "instance/ exists already; refusing to replace its secrets" >&2; exit 1; }
[ -f "/usr/share/zoneinfo/$TZ_NAME" ] || { echo "unknown time zone $TZ_NAME" >&2; exit 2; }
command -v openssl > /dev/null || { echo "openssl is required" >&2; exit 2; }

secret() { openssl rand -hex 32; }

umask 077
mkdir -p instance/keys
openssl genpkey -algorithm ed25519 -out instance/keys/access_token_private.pem 2> /dev/null
openssl pkey -in instance/keys/access_token_private.pem -pubout -out instance/keys/access_token_public.pem
# The containers run as a non-root user; they need to read the keys.
chmod 644 instance/keys/access_token_public.pem
chmod 644 instance/keys/access_token_private.pem
chmod 755 instance/keys
# Provider files the containers read (the Firebase service account, P13).
mkdir -p instance/providers
chmod 755 instance/providers

{
  echo "# Ride Platform instance, made $(date -u +%FT%TZ) by scripts/deploy/init-instance.sh."
  echo "# SECRET. The only copy: back it up off this server."
  echo
  echo "API_DOMAIN=$API"
  echo "FILES_DOMAIN=$FILES"
  echo "S3_PUBLIC_ENDPOINT=https://$FILES"
  echo "ALLOWED_ORIGINS=$ADMIN"
  echo "GATEWAY_PORT=18080"
  echo "FILES_PORT=18333"
  echo
  echo "ANALYTICS_TIME_ZONE=$TZ_NAME"
  echo "WALLET_TIME_ZONE=$TZ_NAME"
  echo "DRIVER_DOCUMENTS_TIME_ZONE=$TZ_NAME"
  echo "DISPATCH_OFFER_TTL=15s"
  echo "OSRM_DATASET_NAME=iraq-latest"
  if [ "$STAGING" = 1 ]; then
    echo "IDENTITY_APP_ENV=test"
  else
    echo "IDENTITY_APP_ENV=production"
  fi
  echo
  echo "INTERNAL_SERVICE_TOKEN=$(secret)"
  echo "OTP_HASH_SECRET=$(secret)"
  echo "VOUCHER_CODE_KEY=$(secret)"
  echo "SOS_WEBHOOK_SECRET=$(secret)"
  echo "VALKEY_PASSWORD=$(secret)"
  echo "LOCATION_VALKEY_PASSWORD=$(secret)"
  echo "S3_ACCESS_KEY=ride$(openssl rand -hex 8)"
  echo "S3_SECRET_KEY=$(secret)"
  echo "ACCESS_TOKEN_KEY_ID=ride-$(date -u +%Y%m%d)"
  echo "ACCESS_TOKEN_PUBLIC_KEY_PATH=.local/keys/access_token_public.pem"
  echo
  echo "POSTGRES_ADMIN_PASSWORD=$(secret)"
  for prefix in ANALYTICS DRIVER IDENTITY LOCATION MEDIA NOTIFICATION PRICING RIDER STAFF SUPPORT TRIP WALLET; do
    lower="$(echo "$prefix" | tr 'A-Z' 'a-z')"
    echo "${prefix}_DB_NAME=${lower}_db"
    echo "${prefix}_DB_USER=${lower}_user"
    echo "${prefix}_DB_PASSWORD=$(secret)"
  done
} > instance/instance.env

# What every service reads (no service's own secret is in here).
grep -E '^(INTERNAL_SERVICE_TOKEN|ACCESS_TOKEN_KEY_ID|ACCESS_TOKEN_PUBLIC_KEY_PATH|ANALYTICS_TIME_ZONE|WALLET_TIME_ZONE|DRIVER_DOCUMENTS_TIME_ZONE)=' \
  instance/instance.env > instance/shared.env
echo "ACCESS_TOKEN_PRIVATE_KEY_PATH=.local/keys/access_token_private.pem" >> instance/shared.env

# Provider credentials, one file per service that needs them (P13).
cat > instance/identity-service.env <<'IDENTITY'
# Login codes (see services/identity-service/.env.example). They only go out
# with IDENTITY_APP_ENV=production in instance.env; "test" writes them to the log.
#
# Phone codes: OTP_PHONE_DEFAULT_CHANNEL=whatsapp sends them by WhatsApp and
# needs WHATSAPP_DEFAULT_PROVIDER; "sms" (or empty) needs SMS_DEFAULT_PROVIDER.
OTP_PHONE_DEFAULT_CHANNEL=whatsapp
WHATSAPP_DEFAULT_PROVIDER=bulksmsiraq
SMS_DEFAULT_PROVIDER=
SMS_ROUTES=
BULKSMSIRAQ_ENDPOINT=
BULKSMSIRAQ_OTP_ENDPOINT=
BULKSMSIRAQ_API_KEY=
BULKSMSIRAQ_SENDER_ID=
#
# Email codes (staff sign in by email): Resend, with a verified domain.
RESEND_API_KEY=
RESEND_FROM=
IDENTITY
cat > instance/wallet-service.env <<'WALLET'
# ZainCash (see services/wallet-service/.env.example).
ZAINCASH_BASE_URL=https://test.zaincash.iq
ZAINCASH_CLIENT_ID=
ZAINCASH_CLIENT_SECRET=
ZAINCASH_WEBHOOK_SECRET=
WALLET
echo "STAFF_BOOTSTRAP_OWNER_EMAIL=$OWNER" > instance/staff-service.env

cat > instance/notification-service.env <<'NOTIFY'
# Who is called when a rider or driver presses SOS (comma separated, E.164).
SOS_OPERATOR_PHONES=
# Push: put the Firebase service-account JSON at
# instance/providers/fcm-service-account.json (chmod 644), then uncomment.
#FCM_CREDENTIALS_FILE=/app/providers/fcm-service-account.json
NOTIFY

echo "made instance/ (instance.env, shared.env, provider files, keys). Back it up now, for example:"
echo "  tar czf ride-instance-\$(date +%F).tgz instance && scp it somewhere off this server"
