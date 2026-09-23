# Rider Service

Owns the rider domain: rider profiles linked to an `identity_id` from
`identity-service`, their ratings (kept in step with `trip.rated` events),
and their saved addresses. No driver or trip data lives here.

## Rider profiles

- `CreateRider`, `GetRider`, `GetRiderByIdentity`, `UpdateRiderProfile`.
- Postgres with a transactional outbox: `rider.created` is written in the
  same transaction as the rider row and published to NATS JetStream by the
  outbox worker.
- Every call is authenticated (access tokens from identity-service, or the
  internal token for other services) and a rider reaches only their own
  profile.

## Saved addresses

A rider keeps up to 20 addresses: at most one home and one work, and others
with a label. Each has the exact point, the address as shown, details
(building, floor), a note for the captain and optionally a photo of the
entrance.

- The limit is checked under a lock on the rider's row, so two requests at
  once cannot both take the last place; one home and one work are enforced
  by a unique index.
- A photo is an upload of the rider's in media-service with purpose
  `ADDRESS_PHOTO`. Saving it asks media-service (with the internal token,
  `MEDIA_SERVICE_ADDRESS`) to hold the file for this identity and purpose,
  which it refuses for a file that is not a ready address photo of the
  rider. From then on the rider cannot delete the file on its own; the
  address deletes it when the photo is replaced or removed or the address is
  deleted. A save that fails lets the photo go again.
- trip-service reads an address (internal token) when a trip is requested
  from it, and copies what it needs into the trip.

## Running locally

```bash
cd services/rider-service
cp .env.example .env   # adjust DATABASE_URL
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/rider-service
```

The address store tests need a throw-away database:

```bash
RIDER_TEST_DATABASE_URL=postgres://.../empty_db \
  go test ./internal/infrastructure/persistence/postgres/
```

## Regenerating proto code

```bash
buf generate   # from the repo root
```
