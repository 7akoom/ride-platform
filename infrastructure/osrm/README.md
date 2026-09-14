# OSRM routing engine

Self-hosted road routing for pricing-service. Open-source (BSD), no API
keys, no usage limits, no per-request cost — it just needs the map data
for your region preprocessed once.

## One-time setup

```bash
cd infrastructure/osrm

# Default region is Iraq. Override for another deployment:
OSRM_REGION_URL=https://download.geofabrik.de/asia/syria-latest.osm.pbf \
  ./prepare-osrm-data.sh
```

Find region URLs at https://download.geofabrik.de — pick the smallest
extract that covers your service area. A country extract is usually
right; a whole continent is overkill and will be slow.

**What to expect:** 10-40 minutes and several GB of RAM for a
country-sized extract. It's CPU and memory heavy while running, and a
one-time cost. The output lands in `infrastructure/osrm/data/`.

When it finishes it will print the `OSRM_DATASET_NAME` to add to
`infrastructure/compose/.env`, then:

```bash
cd ../compose
docker compose up -d osrm
```

Verify it's answering:

```bash
curl "http://localhost:5000/route/v1/driving/44.0093,36.1911;44.05,36.20?overview=false"
```

You should get `{"code":"Ok",...}` with a `distance` in meters and a
`duration` in seconds.

## Per-deployment region

Each deployment serves a different area, so the region is configuration,
not code. To change it: re-run `prepare-osrm-data.sh` with a different
`OSRM_REGION_URL`, update `OSRM_DATASET_NAME` in the compose `.env`, and
restart the `osrm` container.

## Keeping map data fresh

OpenStreetMap changes constantly. Re-running the prepare script (after
deleting the old `.osm.pbf`) pulls a current extract and rebuilds. Every
few months is plenty for most areas.

## If OSRM is down

pricing-service **falls back to a Haversine estimate** rather than
failing — riders still get a price, it's just less accurate, and the
`Route` is flagged as estimated. So a broken or missing OSRM degrades
pricing accuracy; it doesn't take the service down.

## Memory note

The `osrm-routed` server holds the prepared dataset in memory. Budget
roughly 1-2 GB for a country-sized extract. If you're memory-constrained,
use a smaller extract (a single governorate/city rather than a whole
country) — Geofabrik publishes sub-regions.
