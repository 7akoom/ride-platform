# Location Service

Owns where things are: the live positions of drivers and riders, the cities
the platform operates in and the zones it serves inside them, curated places
(airports, malls, hotels...), and the map features built on self-hosted
OpenStreetMap data (routes from OSRM, place search from Nominatim).

## Two stores, on purpose

- **Valkey** holds live positions. A driver's position from 30 seconds ago
  is useless and pings arrive every few seconds, so they never touch
  Postgres. `GEOADD` into one sorted set per entity type (`geo:driver`,
  `geo:rider`) makes `FindNearby` (`GEOSEARCH`) fast; a companion key per
  entity (`loc:driver:<id>`) carries a TTL (30 s by default) and is what
  "stale / offline" means, because sorted sets have no per-member TTL.
  `FindNearby` cross-checks candidates against those keys.
- **PostgreSQL + PostGIS** holds what changes rarely and must survive a
  restart: cities, zones (polygons, `GEOGRAPHY(POLYGON)`) and curated places
  (`GEOGRAPHY(POINT)`, with a trigram index over every name for search).

## Cities, zones and places

A deployment serves one country in one currency (the country is
`MAPS_COUNTRY_CODES`; the currency lives in pricing and wallet). Inside it:

- A **city** has a name, translations (`ar`, `ku`, `en`), an IANA time zone
  (checked against the embedded time zone database) and the point a map of it
  opens at. Switching a city off stops serving all its zones at once.
- A **zone** is a polygon inside a city. `CheckServiceZone` answers whether a
  point is served — an active zone of an active city covers it — and with
  which zone, city and time zone. Every trip request passes it.
- A **curated place** belongs to a city and has a category, translated names,
  a short address and the exact point to be picked up or dropped at. Search
  (`/v1/places:search`) lists matching curated places first, then map results
  (dropping a map result within 75 m of a curated one); if one source is down
  the other still answers.

Staff manage cities and zones with `zones.manage`, curated places with
`places.manage` (asked of staff-service before every change, and audited).
Users see only active cities and places.

## Running locally

```bash
cd services/location-service
cp .env.example .env
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/location-service
```

The Postgres store tests need a throw-away PostGIS database:

```bash
LOCATION_TEST_DATABASE_URL=postgres://.../empty_db \
  go test ./internal/infrastructure/persistence/postgres/
```

## Regenerating proto code

```bash
buf generate   # from the repo root
```
