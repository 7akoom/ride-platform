#!/usr/bin/env bash
# Imports places for this instance's service zones from Overture Maps (open
# places data) into place search: after curated places, before the map's own
# results. Run on the server from the repo root once the instance's cities and
# zones exist, again whenever zones change, and monthly for fresh data:
#   bash scripts/deploy/import-places.sh              # the latest release
#   bash scripts/deploy/import-places.sh --dry-run    # read and count only
#   bash scripts/deploy/import-places.sh --release 2026-09-23.1
# Settings (optional): instance/places-import.json, any keys of
# scripts/tools/import-places/defaults.json. Every run replaces the last import.
set -Eeuo pipefail
trap 'echo "FAIL: place import stopped at line $LINENO" >&2' ERR

COMPOSE=(docker compose -f infrastructure/deploy/compose.vps.yaml --env-file instance/instance.env --profile tools)

# No settings file means the defaults; an empty object is the same.
[ -f instance/places-import.json ] || echo '{}' > instance/places-import.json

"${COMPOSE[@]}" build -q places-import
"${COMPOSE[@]}" run --rm places-import "$@"
