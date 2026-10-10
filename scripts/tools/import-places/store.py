"""The instance's location database: its service zones, and imported_places."""
import psycopg

SOURCE = "overture"


def zone_boxes(conn):
    """The bounding box (west, south, east, north) of every active zone of an
    active city: where places are worth importing."""
    rows = conn.execute("""
        SELECT ST_XMin(b), ST_YMin(b), ST_XMax(b), ST_YMax(b)
        FROM zones z
        JOIN cities c ON c.id = z.city_id,
        LATERAL (SELECT z.boundary::geometry AS b) g
        WHERE z.active AND c.active
    """).fetchall()

    return [tuple(float(v) for v in row) for row in rows]


def replace_places(conn, places, release, dedupe_meters, dedupe_similarity):
    """Swaps imported_places for places in one transaction (searches never see
    it empty). Keeps only places inside an active zone, and one of each group
    of near places with the same or nearly the same name (the most confident).
    Returns how many were received, outside the zones, duplicates, and kept."""
    with conn.transaction():
        conn.execute("""
            CREATE TEMP TABLE incoming (
                id TEXT, name TEXT, alt_names TEXT, kind TEXT, category TEXT, address TEXT,
                longitude FLOAT8, latitude FLOAT8, weight REAL, confidence REAL,
                location GEOGRAPHY(POINT, 4326), search_name TEXT
            ) ON COMMIT DROP
        """)

        columns = ("id", "name", "alt_names", "kind", "category", "address",
                   "longitude", "latitude", "weight", "confidence")
        with conn.cursor().copy(f"COPY incoming ({', '.join(columns)}) FROM STDIN") as copy:
            for place in places:
                copy.write_row([place[column] for column in columns])

        conn.execute("""
            UPDATE incoming
            SET location = ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)::geography,
                search_name = place_search_text(name)
        """)
        conn.execute("CREATE INDEX ON incoming USING GIST (location)")
        conn.execute("ANALYZE incoming")

        outside = conn.execute("""
            DELETE FROM incoming i
            WHERE NOT EXISTS (
                SELECT 1 FROM zones z JOIN cities c ON c.id = z.city_id
                WHERE z.active AND c.active AND ST_Covers(z.boundary, i.location)
            )
        """).rowcount

        duplicates = conn.execute("""
            DELETE FROM incoming a
            USING incoming b
            WHERE a.id <> b.id
              AND (b.confidence > a.confidence OR (b.confidence = a.confidence AND b.id < a.id))
              AND ST_DWithin(a.location, b.location, %s)
              AND (a.search_name = b.search_name OR similarity(a.search_name, b.search_name) >= %s)
        """, (dedupe_meters, dedupe_similarity)).rowcount

        conn.execute("DELETE FROM imported_places")
        kept = conn.execute("""
            INSERT INTO imported_places
                (id, name, alt_names, kind, category, address, location, weight, confidence, source, release)
            SELECT id, name, alt_names, kind, category, address, location, weight, confidence, %s, %s
            FROM incoming
        """, (SOURCE, release)).rowcount

    return {"received": len(places), "outside_zones": outside, "duplicates": duplicates, "kept": kept}


def kinds_count(conn):
    return conn.execute(
        "SELECT kind, count(*) FROM imported_places GROUP BY kind ORDER BY 2 DESC"
    ).fetchall()


def connect(url):
    return psycopg.connect(url, autocommit=True)
