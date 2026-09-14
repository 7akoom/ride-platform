#!/usr/bin/env bash
#
# Prepares an OSRM routing dataset for a region.
#
# Run this ONCE per deployment (and again whenever you want fresher map
# data). It downloads an OpenStreetMap extract and preprocesses it into
# the files the OSRM server loads at startup.
#
# The region is deliberately configurable — each deployment serves a
# different country/city, so this is set per instance rather than
# hardcoded.
#
# Usage:
#   ./prepare-osrm-data.sh
#   OSRM_REGION_URL=https://download.geofabrik.de/asia/iraq-latest.osm.pbf ./prepare-osrm-data.sh
#
# Find your region's URL at https://download.geofabrik.de
#
# Expect this to take 10-40 minutes and several GB of RAM for a
# country-sized extract. It is CPU and memory heavy; it is also a
# one-time cost.

set -euo pipefail

REGION_URL="${OSRM_REGION_URL:-https://download.geofabrik.de/asia/iraq-latest.osm.pbf}"
DATA_DIR="${OSRM_DATA_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/data}"
OSRM_IMAGE="${OSRM_IMAGE:-ghcr.io/project-osrm/osrm-backend:v5.27.1}"

PBF_NAME="$(basename "$REGION_URL")"
BASE_NAME="${PBF_NAME%.osm.pbf}"

mkdir -p "$DATA_DIR"

echo "==> Region:    $REGION_URL"
echo "==> Data dir:  $DATA_DIR"
echo

if [ -f "$DATA_DIR/$PBF_NAME" ]; then
  echo "==> $PBF_NAME already downloaded, skipping download."
  echo "    (delete it and re-run to fetch fresher map data)"
else
  echo "==> Downloading map extract..."
  curl -L --fail --progress-bar -o "$DATA_DIR/$PBF_NAME" "$REGION_URL"
fi

echo
echo "==> Extracting road network (this is the slow, memory-hungry step)..."
docker run --rm -t \
  -v "$DATA_DIR:/data" \
  "$OSRM_IMAGE" \
  osrm-extract -p /opt/car.lua "/data/$PBF_NAME"

echo
echo "==> Partitioning..."
docker run --rm -t \
  -v "$DATA_DIR:/data" \
  "$OSRM_IMAGE" \
  osrm-partition "/data/$BASE_NAME.osrm"

echo
echo "==> Customizing..."
docker run --rm -t \
  -v "$DATA_DIR:/data" \
  "$OSRM_IMAGE" \
  osrm-customize "/data/$BASE_NAME.osrm"

echo
echo "==> Done."
echo
echo "Set this in infrastructure/compose/.env so the osrm service knows"
echo "which dataset to load:"
echo
echo "    OSRM_DATASET_NAME=$BASE_NAME"
echo
echo "Then: docker compose up -d osrm"
