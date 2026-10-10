"""Reading places from Overture Maps (https://overturemaps.org), an open places
dataset under the CDLA Permissive 2.0 and Apache 2.0 licences."""
import json
import urllib.request

import duckdb

CATALOG = "https://stac.overturemaps.org/catalog.json"
PLACES = "s3://overturemaps-us-west-2/release/{release}/theme=places/type=place/*"


def latest_release():
    """The newest release, as Overture's own catalog names it."""
    with urllib.request.urlopen(CATALOG, timeout=30) as answer:
        latest = json.load(answer).get("latest")

    if not latest:
        raise SystemExit(f"{CATALOG} names no latest release; pass --release")

    return latest


def read_places(source, boxes, memory_limit="768MB"):
    """Every place inside one of boxes ((west, south, east, north) in degrees),
    from source: the release's files on S3, or local Parquet files in the same
    layout (tests)."""
    db = duckdb.connect()
    db.sql(f"SET memory_limit = '{memory_limit}'")

    if source.startswith("s3://"):
        db.sql("INSTALL httpfs; LOAD httpfs; SET s3_region = 'us-west-2';")

    inside = " OR ".join(
        f"(bbox.xmin >= {w} AND bbox.xmax <= {e} AND bbox.ymin >= {s} AND bbox.ymax <= {n})"
        for w, s, e, n in boxes
    )

    rows = db.sql(f"""
        SELECT id,
               coalesce(names."primary", '') AS name,
               coalesce(array_to_string(map_values(names.common), ' '), '') AS other_names,
               coalesce(brand.names."primary", '') AS brand,
               coalesce(basic_category, '') AS category,
               coalesce(confidence, 0) AS confidence,
               coalesce(operating_status, '') AS status,
               coalesce(addresses[1].freeform, '') AS street,
               coalesce(addresses[1].locality, '') AS locality,
               bbox.xmin AS longitude,
               bbox.ymin AS latitude
        FROM read_parquet('{source}', hive_partitioning = true)
        WHERE {inside}
    """).fetchall()

    columns = ["id", "name", "other_names", "brand", "category", "confidence", "status",
               "street", "locality", "longitude", "latitude"]

    return [dict(zip(columns, row)) for row in rows]
