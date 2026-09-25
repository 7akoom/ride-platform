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
`trip.completed`, `trip.cancelled` (with `cancelled_by` and `rider_no_show`),
`trip.schedule_failed` (a booking that could not become a trip),
`trip.stop_reached` (with the stop's `position`) — every one written in the
same transaction as its change.

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

## Scheduled trips and rides for someone else

A rider books a trip ahead (`ScheduleTrip`): 30 minutes to 7 days away, at
most 3 upcoming, only where a service zone serves the pickup. The booking
keeps the zone's time zone so the app shows the local time. Saved addresses
are copied into the booking when it is made.

A scheduler in this service (every `TRIP_SCHEDULE_POLL_INTERVAL`, 15 s)
claims due bookings with `FOR UPDATE SKIP LOCKED` and a one-minute lease, so
several replicas never request the same one twice. At `scheduled_at` minus
`TRIP_SCHEDULE_DISPATCH_LEAD` (10 minutes) it requests the trip through the
normal `RequestTrip` path with the booking's id as the trip's id: a retry
after a crash finds the trip instead of making a second one. A failed
request is retried every 30 s until `TRIP_SCHEDULE_GRACE` (10 minutes) past
the booked time, then the booking fails with a reason the rider can read
and a `trip.schedule_failed` event. A booking cancelled while its trip was
being requested gets that trip cancelled by the system.

Any trip, booked or not, can be for someone else: `passenger_name` and
`passenger_phone` (E.164), both or neither. The phone is shown only while
the trip is live.

| Variable | Default | |
|---|---|---|
| `TRIP_SCHEDULE_MIN_AHEAD` | `30m` | earliest booking |
| `TRIP_SCHEDULE_MAX_AHEAD` | `168h` | latest booking |
| `TRIP_SCHEDULE_MAX_UPCOMING` | `3` | per rider |
| `TRIP_SCHEDULE_DISPATCH_LEAD` | `10m` | shorter than the minimum ahead |
| `TRIP_SCHEDULE_GRACE` | `10m` | retries past the booked time |
| `TRIP_SCHEDULE_POLL_INTERVAL` | `15s` | |

## Stops on the way

A trip (or a booking) may stop up to 2 times between pickup and dropoff
(`stops`, in order, kept as JSON on the row). pricing-service routes and
prices through them: a quote keeps its stops and a trip requested with it
must have the same ones (each within 50 m); a trip without a quote is priced
through its stops when it completes. The driver marks a stop with
`ReachStop` while the trip is in progress, within 200 m of it by their last
reported position; stops may be reached in any order or skipped, and a trip
completes whether or not every stop was reached. Waiting at a stop is not
charged separately: it is part of the ride.

## Sharing a trip

The rider of a trip under way makes a link (`ShareTrip`) and sends it to
people they trust. The link's token is 256 random bits, returned once; only
its SHA-256 is stored (`trip_shares`). At most 5 live links per trip;
`StopSharingTrip` ends them all. `GetSharedTrip` is the one RPC that needs no
credential: authentication and authorization let it through, the handler
checks the token, and the rate limit counts such calls by the address they
come from (the last `x-forwarded-for` hop, which the gateway adds). It shows
the trip's route and progress, the driver's name and car, and their position
while the trip is under way, never the rider, the passenger, the price or a
phone number. A link stops at `TRIP_SHARE_MAX_AGE` (12h), or
`TRIP_SHARE_AFTER_END` (30m) after the trip ends, whichever is first; every
failure is the same 404. `TRIP_SHARE_URL_BASE`, when set, makes the full URL
(base + token); the page that shows a link belongs to the web apps.

## Unpaid fees

A cancellation or no-show fee the rider's wallet could not cover stays owed
(wallet-service collects it from the next money that reaches the wallet).
Before creating a trip, trip-service asks wallet-service (`GetRiderDues`,
internal token, `WALLET_SERVICE_ADDRESS`) whether the rider may request
trips: when the deployment blocks trips until fees are paid
(`wallet_configs.block_trips_with_dues`, on by default) and the rider owes
any, `RequestTrip` fails with `FAILED_PRECONDITION` and the amount owed. While
wallet-service cannot be reached the request goes through (a warning is
logged): the fee is still collected later. wallet-service also calls this
service, so the connection is lazy and compose does not order them.

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
