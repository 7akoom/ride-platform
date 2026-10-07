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

if [ "$(value IDENTITY_APP_ENV)" = test ]; then
  warn "STAGING: login codes are written to identity-service's log, not sent. Never launch like this."
else
  provider="$(sed -n 's/^SMS_DEFAULT_PROVIDER=//p' instance/identity-service.env 2> /dev/null | tail -1)"
  [ -n "$provider" ] && ok "login codes are sent by $provider" \
    || fail "IDENTITY_APP_ENV=production but no SMS_DEFAULT_PROVIDER in instance/identity-service.env: nobody could sign in"
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
  if [ -z "$owner" ] || echo "$owner" | grep -q docker; then ok "port $port is free for us"; else fail "port $port is taken: $owner"; fi
done

dataset="$(value OSRM_DATASET_NAME)"
ls infrastructure/osrm/data/"$dataset".osrm* > /dev/null 2>&1 && ok "road data $dataset is prepared" \
  || warn "road data $dataset is not prepared yet (deploy.sh prepares it; about 2 GB of memory for a few minutes)"

echo "==> names"
for key in API_DOMAIN FILES_DOMAIN; do
  domain="$(value "$key")"
  if getent hosts "$domain" > /dev/null; then ok "$domain resolves"; else warn "$domain does not resolve yet (add it in DNS)"; fi
done

echo
if [ "$fails" -gt 0 ]; then echo "FAIL: $fails problem(s)"; exit 1; fi
echo "ready"
