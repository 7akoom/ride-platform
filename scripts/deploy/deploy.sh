#!/usr/bin/env bash
# Deploys (or updates) this server's instance from the checked-out code.
# Run on the server, from the repo root:
#   git pull && bash scripts/deploy/deploy.sh
# Steps: preflight, road data (first time), build, databases and NATS,
# migrations, every service, then a smoke test through the public domain.
set -Eeuo pipefail
trap 'echo "FAIL: deploy stopped at line $LINENO" >&2' ERR

COMPOSE=(docker compose -f infrastructure/deploy/compose.vps.yaml --env-file instance/instance.env)
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

bash scripts/deploy/preflight.sh

# Monitoring (the Alloy agent) runs once enable-monitoring.sh wrote its credentials.
if [ -s instance/monitoring.env ]; then
  COMPOSE+=(--profile monitoring)
  echo "==> monitoring is on (Grafana Cloud)"
fi

# Address search (Nominatim) runs when the instance turns it on: MAPS_SEARCH=on in
# instance/instance.env. Its first start imports the country's map (about an hour).
if [ "$(value MAPS_SEARCH)" = "on" ]; then
  COMPOSE+=(--profile maps)
  echo "==> address search is on (Nominatim)"
fi

# Instances made before P13 have no providers folder; notification mounts it.
[ -d instance/providers ] || { mkdir -p instance/providers && chmod 755 instance/providers; }

dataset="$(value OSRM_DATASET_NAME)"
if ! ls infrastructure/osrm/data/"$dataset".osrm* > /dev/null 2>&1; then
  echo "==> road data (first time only)"
  bash infrastructure/osrm/prepare-osrm-data.sh
fi

echo "==> building ($(git rev-parse --short HEAD))"
"${COMPOSE[@]}" build
"${COMPOSE[@]}" --profile tools build migrate

echo "==> databases, cache, messaging, files"
"${COMPOSE[@]}" up -d --wait postgres identity-valkey location-valkey nats seaweedfs
# The stream descriptions are long; keep them in a file unless something fails.
if ! "${COMPOSE[@]}" up nats-bootstrap driver-nats-bootstrap pricing-nats-bootstrap rider-nats-bootstrap \
     support-nats-bootstrap trip-nats-bootstrap wallet-nats-bootstrap > /tmp/ride-nats-bootstrap.log 2>&1; then
  tail -40 /tmp/ride-nats-bootstrap.log >&2
  false
fi
echo "  streams ready (details: /tmp/ride-nats-bootstrap.log)"

echo "==> migrations"
"${COMPOSE[@]}" --profile tools run --rm migrate

echo "==> services"
"${COMPOSE[@]}" up -d --remove-orphans
sleep 10
"${COMPOSE[@]}" ps --format 'table {{.Name}}\t{{.Status}}'

stopped="$("${COMPOSE[@]}" ps -a --format '{{.Name}} {{.State}}' | grep -v bootstrap | grep -v ' running' || true)"
if [ -n "$stopped" ]; then
  echo "FAIL: not running:" >&2; echo "$stopped" >&2
  echo "see: docker logs <name>" >&2
  exit 1
fi

if [ "$(value MAPS_SEARCH)" = "on" ] && ! docker inspect -f '{{.State.Health.Status}}' ride-nominatim 2> /dev/null | grep -q healthy; then
  echo "NOTE: address search is still importing the map (first time: about an hour)."
  echo "      Searches answer \"unavailable\" until then. Follow it: docker logs -f ride-nominatim"
fi

bash scripts/deploy/smoke.sh
