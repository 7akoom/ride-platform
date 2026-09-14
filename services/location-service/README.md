# Location Service

Owns real-time position tracking for drivers and riders. Unlike every
other service so far, this one has **no Postgres database** — it's
backed entirely by Valkey, and that's a deliberate design choice, not a
shortcut.

## Why no durable database here

A driver's position from 30 seconds ago is operationally useless — the
only thing that matters is "where is this driver *right now*". Writing
every GPS ping (every 2-5 seconds, per active driver) to Postgres would
overwhelm it for no benefit. This is the same reasoning production
systems like Uber/Careem use: real-time location lives in an in-memory
store (Redis/Valkey) with automatic expiry; only meaningful trip
milestones (pickup point, dropoff point) get persisted durably, and
that's Trip service's job, not this one's.

## How it works

- **`GEOADD`** into a Valkey sorted set (one per entity type: `geo:driver`,
  `geo:rider`) — this is what makes `FindNearby` (`GEOSEARCH`) fast.
- **A companion "meta" key per entity** (`loc:driver:<id>`) storing the
  same coordinates as JSON, with a TTL (30s by default). This key
  expiring is what "this entity went stale/offline" means.

  This two-key design exists because Valkey sorted sets don't support a
  TTL per member — only on the whole key. A single shared TTL on
  `geo:driver` would wipe out every driver's position when it expired,
  which is wrong. So the meta key carries the actual freshness signal,
  and `FindNearby` cross-checks `GEOSEARCH` candidates against the meta
  keys (via `MGET`) before returning them, filtering out anything whose
  entry is stale but hasn't been cleaned out of the geo set yet.

- No transactional outbox here either — location pings are too
  high-frequency to be meaningful domain events. If a future need comes
  up (e.g. "notify when a driver enters a zone"), that's a different,
  much lower-frequency event and can be added deliberately then.

## What's intentionally NOT done yet

Same list as rider-service and driver-service:

- Observability (Prometheus metrics, structured request logging)
- Authentication interceptor
- Tests
- A background cleanup job for the geo sorted sets (currently handled
  lazily at query time in `FindNearby` — fine for now, but at high scale
  a periodic job that actually removes stale members from the sorted
  set, not just filters them out at read time, would be more efficient)

## Running locally

```bash
cd services/location-service
cp .env.example .env   # adjust VALKEY_ADDRESS / VALKEY_PASSWORD
go mod tidy
go run ./cmd/location-service
```

## Regenerating proto code

```bash
buf generate   # from the repo root
```
