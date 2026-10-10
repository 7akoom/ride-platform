"""Imports places for this instance's own service zones from Overture Maps into
the location database (imported_places), where place search finds them after
curated places and before the map's own results.

Nothing in it names a city or a country: it imports what lies inside the
instance's active zones, so a new city, governorate or country only needs its
zones, then another run. Every run replaces the previous import whole.

    python import_places.py [--release 2026-09-23.1] [--settings FILE] [--dry-run]

The database: DATABASE_URL, or LOCATION_DB_NAME / LOCATION_DB_USER /
LOCATION_DB_PASSWORD on DB_HOST (default postgres). Settings: defaults.json,
with the keys the instance file sets (instance/places-import.json) on top.
"""
import argparse
import os
import sys
import time
import urllib.parse

import overture
import settings as settings_module
import store

# The database's column sizes.
LIMITS = {"id": 100, "name": 300, "category": 100, "address": 300}


def database_url():
    url = os.environ.get("DATABASE_URL")
    if url:
        return url

    try:
        name, user, password = (os.environ[f"LOCATION_DB_{k}"] for k in ("NAME", "USER", "PASSWORD"))
    except KeyError as missing:
        raise SystemExit(f"set DATABASE_URL, or {missing.args[0]} (with the other LOCATION_DB_ values)")

    host = os.environ.get("DB_HOST", "postgres")

    return f"postgresql://{urllib.parse.quote(user)}:{urllib.parse.quote(password)}@{host}:5432/{name}"


def prepared(raw, settings):
    """A dataset place as a row of imported_places, or None when it is not kept."""
    if not settings_module.keeps(raw, settings):
        return None

    kind = settings_module.kind_of(raw["category"], settings)
    address = ", ".join(part for part in (raw["street"], raw["locality"]) if part)
    alt_names = " ".join(part for part in (raw["other_names"], raw["brand"]) if part)

    return {
        "id": raw["id"][:LIMITS["id"]],
        "name": raw["name"].strip()[:LIMITS["name"]],
        "alt_names": alt_names,
        "kind": kind,
        "category": raw["category"][:LIMITS["category"]],
        "address": address[:LIMITS["address"]],
        "longitude": raw["longitude"],
        "latitude": raw["latitude"],
        "weight": settings_module.weight_of(raw["category"], kind, settings),
        "confidence": raw["confidence"],
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--release", help="an Overture release; the latest by default")
    parser.add_argument("--source", help="read these Parquet files instead of the release (tests)")
    parser.add_argument("--settings", default=os.environ.get("PLACES_IMPORT_SETTINGS", ""),
                        help="the instance's settings file")
    parser.add_argument("--dry-run", action="store_true", help="read and count, change nothing")
    args = parser.parse_args(argv)

    settings = settings_module.load(args.settings)
    conn = store.connect(database_url())

    boxes = store.zone_boxes(conn)
    if not boxes:
        raise SystemExit("no active service zone in an active city: add the instance's cities and zones first")

    release = args.release or ("local" if args.source else overture.latest_release())
    source = args.source or overture.PLACES.format(release=release)
    print(f"==> reading places in {len(boxes)} zone(s) from Overture {release}", flush=True)

    started = time.monotonic()
    raw = overture.read_places(source, boxes)
    places = [p for p in (prepared(r, settings) for r in raw) if p]
    print(f"  {len(raw)} in the zones' boxes, {len(places)} worth searching for "
          f"({time.monotonic() - started:.0f}s)")

    if args.dry_run:
        print("dry run: nothing changed")
        return 0

    counts = store.replace_places(conn, places, release, settings["dedupe_meters"], settings["dedupe_similarity"])
    print(f"==> imported: {counts['kept']} places "
          f"({counts['outside_zones']} outside the zones, {counts['duplicates']} duplicates left out)")

    for kind, count in store.kinds_count(conn):
        print(f"  {kind:<12} {count}")

    return 0


if __name__ == "__main__":
    sys.exit(main())
