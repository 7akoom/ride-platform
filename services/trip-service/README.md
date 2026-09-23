# Trip Service

Owns the trip lifecycle — the record of one ride from request through to
completion or cancellation. This is the service that ties Rider, Driver,
and Location together into an actual ride.

## The state machine

```
requested -> accepted -> in_progress -> completed
    |            |             |
    v            v             v
cancelled    cancelled     cancelled
```

- `completed` and `cancelled` are terminal — nothing transitions out of them.
- The allowed moves live in exactly one place: `Status.CanTransitionTo` in
  `internal/application/trip/trip.go`. Every use case (accept/start/
  complete/cancel) goes through the repository's `transition()` helper,
  which checks this before touching the database — so a new use case can
  never accidentally skip the check.
- `AcceptTrip` additionally enforces "one active trip per driver" the same
  way `RequestTrip` enforces "one active trip per rider" — both check via
  `FindActiveByRiderID`/`FindActiveByDriverID` before proceeding.

## Manual dispatch for now

`AcceptTrip` takes a `driver_id` directly — there's no matching logic yet.
That's intentional: Dispatch service (Phase 2) is what will listen for
`trip.requested` events and pick a driver automatically via Location's
`FindNearby`. Until then, you accept trips by hand (or from a script) to
exercise the full lifecycle, which is exactly the "one full ride, driven
manually" milestone this was built for.

## Outbox events emitted

`trip.requested`, `trip.accepted`, `trip.driver_arrived`, `trip.started`,
`trip.completed`, `trip.cancelled` (with `cancelled_by` and `rider_no_show`) —
every one written in the same transaction as its change.

## Arrival, waiting and cancelling

- `MarkDriverArrived` (`:arrived`): the driver of an accepted trip, within
  200 m of the pickup by their last reported position (location-service,
  30 s). It sets `arrived_at` once and emits `trip.driver_arrived` (the rider
  is told). The trip stays `accepted`: arrival is a moment, not a state.
- The wait from `arrived_at` to `started_at` beyond the rate card's free
  minutes is charged by pricing-service when the trip completes.
- `CancelTrip` records who cancelled: the rider or the driver of the trip
  (told apart from the caller's identity), or `system` for the internal token
  (dispatch, staff). `rider_no_show` is only the driver's, only after arriving
  and waiting `TRIP_NO_SHOW_WAIT` (5 minutes, 1 to 30).
- pricing-service decides the fee of a cancelled trip from these and the rate
  card, and wallet-service takes it from the rider's wallet.

## Addresses, the pickup photo and recent destinations

- A trip keeps the pickup and dropoff addresses as the rider picked them.
- `pickup_saved_address_id` / `dropoff_saved_address_id` name one of the
  rider's saved addresses (read from rider-service with the internal token):
  its point and address are used, and for the pickup its details, note for
  the captain and photo are copied into the trip, so a later edit of the
  address does not change the trip. Only a saved address brings a note or a
  photo.
- `GetPickupPhoto` gives the rider or the driver of the trip a short-lived
  link to that photo (asked of media-service with the internal token), only
  while the trip is accepted or in progress. The offer a driver sees has the
  addresses but not the note or the photo.
- `ListRecentDestinations` lists where the rider's completed trips ended,
  newest first, each place once (drop-offs within about 10 m are one).

## Fare quotes

A trip may be requested with a `quote_id` from pricing-service
(`POST /v1/fare-quotes`). trip-service claims it (`ClaimQuote`, internal
token, `PRICING_SERVICE_ADDRESS`) before creating the trip: the quote must be
the rider's, not expired and not used, and the trip's pickup and dropoff
(after any saved address) within 50 m of the quoted ones; a named vehicle
class must be the quoted one, and an empty one becomes it. The trip keeps
`quote_id`, `quoted_fare` and `currency_code`, and the captain's offer shows
the fare. If the trip cannot be created after the claim, the quote is
released. When the trip completes, pricing charges exactly the quoted fare.

A trip without a quote is priced when it completes.

pricing-service calls this service (`GetTrip`) and this one calls pricing,
so compose starts pricing after trip-service and trip-service connects to
pricing lazily.

## What's intentionally NOT done yet

Same list as the other services (observability, auth interceptor, tests,
NATS worker) — see rider-service's README for the fuller explanation.
Additionally, specific to Trip:

- **No fare/pricing** — that's Pricing service's job (Phase 2). A trip has
  no fare_amount column yet.
- **No concurrent-request race handling beyond `SELECT ... FOR UPDATE`** —
  that lock does prevent two simultaneous `AcceptTrip` calls from both
  succeeding on the same trip, but there's no idempotency key on
  `RequestTrip` (a rider double-tapping "request" fast enough could
  theoretically create two trips before the first one's row exists to be
  found by `FindActiveByRiderID`). Worth revisiting once a real client
  exists to observe whether this happens in practice.

## Running locally

```bash
cd services/trip-service
cp .env.example .env   # adjust DATABASE_URL
go mod tidy
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/trip-service
```

## Regenerating proto code

```bash
buf generate   # from the repo root
```

## Trying the full lifecycle manually

With rider-service, driver-service, and trip-service all running, and a
real rider_id / driver_id from those services:

```bash
# 1. Request a trip
grpcurl -plaintext -import-path ./proto -proto ride/trip/v1/trip.proto \
  -d '{"rider_id": "<rider-id>", "pickup": {"latitude": 36.19, "longitude": 44.01}, "dropoff": {"latitude": 36.20, "longitude": 44.05}}' \
  localhost:50055 ride.trip.v1.TripService/RequestTrip

# 2. Accept it (use the trip_id from step 1's response)
grpcurl -plaintext -import-path ./proto -proto ride/trip/v1/trip.proto \
  -d '{"trip_id": "<trip-id>", "driver_id": "<driver-id>"}' \
  localhost:50055 ride.trip.v1.TripService/AcceptTrip

# 3. Start it
grpcurl -plaintext -import-path ./proto -proto ride/trip/v1/trip.proto \
  -d '{"trip_id": "<trip-id>"}' \
  localhost:50055 ride.trip.v1.TripService/StartTrip

# 4. Complete it
grpcurl -plaintext -import-path ./proto -proto ride/trip/v1/trip.proto \
  -d '{"trip_id": "<trip-id>"}' \
  localhost:50055 ride.trip.v1.TripService/CompleteTrip
```
