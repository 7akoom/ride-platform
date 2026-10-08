#!/usr/bin/env bash
# Checks a server is ready for (another) deploy. Run from the repo root:
#   bash scripts/deploy/preflight.sh
# FAIL lines stop a deploy; WARN lines do not.
set -uo pipefail

fails=0
ok()   { printf '  ok    %s\n' "$1"; }
warn() { printf '  WARN  %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; fails=$((fails + 1)); }

ENV_FILE=instance/instance.env
value() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -1; }

echo "==> the instance"
if [ ! -f "$ENV_FILE" ] || [ ! -f instance/shared.env ]; then
  fail "instance/ is missing: bash scripts/deploy/init-instance.sh --api <domain> --files <domain> [--staging]"
  echo; echo "FAIL: $fails"; exit 1
fi

for key in INTERNAL_SERVICE_TOKEN OTP_HASH_SECRET VOUCHER_CODE_KEY VALKEY_PASSWORD LOCATION_VALKEY_PASSWORD \
           S3_SECRET_KEY POSTGRES_ADMIN_PASSWORD SOS_WEBHOOK_SECRET; do
  v="$(value "$key")"
  case "$v" in
    ""|*change-me*|dev-*) fail "$key is empty or a development placeholder" ;;
    *) [ "${#v}" -ge 32 ] && ok "$key is set" || fail "$key is shorter than 32 characters" ;;
  esac
done

for key in $(grep -oE '^[A-Z]+_DB_PASSWORD' "$ENV_FILE"); do
  v="$(value "$key")"
  [ "${#v}" -ge 32 ] || fail "$key is shorter than 32 characters"
done
ok "every database has its own password"

shared_token="$(sed -n 's/^INTERNAL_SERVICE_TOKEN=//p' instance/shared.env | tail -1)"
[ "$shared_token" = "$(value INTERNAL_SERVICE_TOKEN)" ] && ok "shared.env carries the same internal token" \
  || fail "INTERNAL_SERVICE_TOKEN differs between instance.env and shared.env (every service must hold the same one)"

[ -s instance/keys/access_token_private.pem ] && [ -s instance/keys/access_token_public.pem ] \
  && ok "the access-token key pair is there" || fail "instance/keys has no key pair"

identity() { sed -n "s/^$1=//p" instance/identity-service.env 2> /dev/null | tail -1; }
if [ "$(value IDENTITY_APP_ENV)" = test ]; then
  warn "STAGING: login codes are written to identity-service's log, not sent. Never launch like this."
else
  case "$(identity OTP_PHONE_DEFAULT_CHANNEL)" in
    whatsapp)
      provider="$(identity WHATSAPP_DEFAULT_PROVIDER)"
      [ -n "$provider" ] && ok "phone codes go by WhatsApp ($provider)" \
        || fail "OTP_PHONE_DEFAULT_CHANNEL=whatsapp but no WHATSAPP_DEFAULT_PROVIDER in instance/identity-service.env" ;;
    ""|sms)
      provider="$(identity SMS_DEFAULT_PROVIDER)"
      [ -n "$provider" ] && ok "phone codes go by SMS ($provider)" \
        || fail "IDENTITY_APP_ENV=production but no SMS_DEFAULT_PROVIDER in instance/identity-service.env: nobody could sign in" ;;
    *) fail "OTP_PHONE_DEFAULT_CHANNEL must be sms or whatsapp" ;;
  esac
  if [ "$(identity WHATSAPP_DEFAULT_PROVIDER)" = bulksmsiraq ] || [ "$(identity SMS_DEFAULT_PROVIDER)" = bulksmsiraq ]; then
    for key in BULKSMSIRAQ_ENDPOINT BULKSMSIRAQ_OTP_ENDPOINT BULKSMSIRAQ_API_KEY BULKSMSIRAQ_SENDER_ID; do
      [ -n "$(identity "$key")" ] || fail "$key is empty in instance/identity-service.env"
    done
  fi
  if [ -n "$(identity RESEND_API_KEY)" ] && [ -n "$(identity RESEND_FROM)" ]; then ok "email codes go by Resend"
  else fail "RESEND_API_KEY and RESEND_FROM are needed in instance/identity-service.env (staff sign in by email)"; fi
fi

fcm="$(sed -n 's/^FCM_CREDENTIALS_FILE=//p' instance/notification-service.env 2> /dev/null | tail -1)"
if [ -z "$fcm" ]; then
  warn "no FCM_CREDENTIALS_FILE: push notifications are off (in-app notifications still work)"
elif [ "$fcm" != /app/providers/fcm-service-account.json ]; then
  fail "FCM_CREDENTIALS_FILE must be /app/providers/fcm-service-account.json (the file goes in instance/providers/)"
elif [ ! -s instance/providers/fcm-service-account.json ]; then
  fail "instance/providers/fcm-service-account.json is missing"
elif ! python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if d.get("type")=="service_account" and d.get("private_key") else 1)' \
       instance/providers/fcm-service-account.json 2> /dev/null; then
  fail "instance/providers/fcm-service-account.json is not a Firebase service-account key"
elif [ "$(stat -c %a instance/providers/fcm-service-account.json)" != 644 ]; then
  fail "chmod 644 instance/providers/fcm-service-account.json (the container user must read it; instance/ itself stays private)"
else
  ok "push by Firebase project $(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["project_id"])' instance/providers/fcm-service-account.json)"
fi

if [ -s instance/monitoring.env ]; then
  monitoring_ok=1
  for key in GRAFANA_CLOUD_OTLP_ENDPOINT GRAFANA_CLOUD_INSTANCE_ID GRAFANA_CLOUD_API_TOKEN; do
    grep -q "^$key=." instance/monitoring.env || { fail "$key is empty in instance/monitoring.env (bash scripts/deploy/enable-monitoring.sh)"; monitoring_ok=0; }
  done
  [ "$(value OTEL_EXPORTER_OTLP_ENDPOINT)" = http://alloy:4317 ] \
    || { fail "monitoring is set up but OTEL_EXPORTER_OTLP_ENDPOINT is not http://alloy:4317 in instance.env"; monitoring_ok=0; }
  [ "$monitoring_ok" = 1 ] && ok "monitoring goes to Grafana Cloud ($(sed -n 's/^GRAFANA_CLOUD_OTLP_ENDPOINT=//p' instance/monitoring.env))"
else
  warn "monitoring is off (bash scripts/deploy/enable-monitoring.sh); trace.sh still finds requests in the logs"
fi

echo "==> the server"
command -v docker > /dev/null && docker compose version > /dev/null 2>&1 && ok "docker and compose" \
  || fail "docker with the compose plugin is not installed (curl -fsSL https://get.docker.com | sudo sh)"
docker info > /dev/null 2>&1 && ok "docker can be used by $(whoami)" \
  || fail "$(whoami) cannot use docker (sudo usermod -aG docker $(whoami), then log in again)"

available_mb="$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo)"
running="$(docker ps --filter name=ride-postgres -q 2> /dev/null)"
if [ -n "$running" ]; then
  ok "running already (memory now available: ${available_mb} MB)"
elif [ "$available_mb" -ge 3500 ]; then
  ok "${available_mb} MB of memory available (about 3.5 GB needed)"
else
  fail "only ${available_mb} MB of memory available; about 3.5 GB is needed"
fi

free_gb="$(df -BG --output=avail . | tail -1 | tr -dc 0-9)"
[ "$free_gb" -ge 10 ] && ok "${free_gb} GB of disk free" || fail "only ${free_gb} GB of disk free (10 GB at least)"

for key in GATEWAY_PORT FILES_PORT; do
  port="$(value "$key")"
  owner="$(ss -Htlnp "sport = :$port" 2> /dev/null)"
  # Without root, ss cannot name docker-proxy; ask docker whether one of our
  # containers publishes the port (a re-deploy finds it taken by us).
  ours="$(docker ps --filter name=ride- --format '{{.Names}} {{.Ports}}' 2> /dev/null | grep -F "127.0.0.1:$port->" | cut -d' ' -f1)"
  if [ -z "$owner" ]; then ok "port $port is free for us"
  elif [ -n "$ours" ]; then ok "port $port is ours ($ours)"
  else fail "port $port is taken by something else: $owner"; fi
done

dataset="$(value OSRM_DATASET_NAME)"
ls infrastructure/osrm/data/"$dataset".osrm* > /dev/null 2>&1 && ok "road data $dataset is prepared" \
  || warn "road data $dataset is not prepared yet (deploy.sh prepares it; about 2 GB of memory for a few minutes)"

echo "==> names"
if [ -e /etc/nginx/sites-enabled/ride-platform ]; then ok "the nginx site is enabled"
else warn "no nginx site yet: sudo bash scripts/deploy/nginx-site.sh (otherwise the domains reach another site)"; fi
tiles_dir="$(value TILES_DIR)"
if [ -z "$(value TILES_DOMAIN)" ]; then
  warn "no map server yet (sudo bash scripts/deploy/nginx-tiles-site.sh --domain ride-tiles.<domain>, then prepare-tiles.sh)"
elif [ -s "$tiles_dir/basemap.pmtiles" ]; then
  ok "the map is prepared ($(sed -n 's/^build=//p' "$tiles_dir/VERSION" 2> /dev/null))"
else
  warn "no basemap in $tiles_dir yet: bash scripts/deploy/prepare-tiles.sh"
fi
for key in API_DOMAIN FILES_DOMAIN TILES_DOMAIN; do
  domain="$(value "$key")"
  [ -n "$domain" ] || continue
  if getent hosts "$domain" > /dev/null; then ok "$domain resolves"; else warn "$domain does not resolve yet (add it in DNS)"; fi
done

echo
if [ "$fails" -gt 0 ]; then echo "FAIL: $fails problem(s)"; exit 1; fi
echo "ready"
