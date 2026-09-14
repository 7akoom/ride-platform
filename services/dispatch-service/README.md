# Dispatch Service

Automates what you've been doing by hand with `grpcurl`: given a
`requested` trip, find the nearest available driver and assign them.

## Why it has no database

Dispatch holds no state of its own — every fact it needs (the trip's
pickup point, which drivers are nearby, whether a given driver is
actually free) lives in Trip, Location, and Driver respectively. Dispatch
is purely an **orchestrator**: it calls those three services as a gRPC
client, and its only job is the decision logic that ties their answers
together. This is the first service in the project that's a client of
other services rather than just a server.

## How `DispatchTrip` works

1. Ask Trip service for the trip's pickup point and current status —
   bail out if it isn't `requested` (already being handled, or invalid).
2. Ask Location service for the nearest available drivers to that point
   (`FindNearby`, entity_type=DRIVER).
3. Walk the candidates **nearest first**. For each one:
   - Ask Driver service if it's actually `active` and `available` right
     now (Location only knows recent position, not availability — a
     driver mid-trip still pings their location).
   - If eligible, try `Trip.AcceptTrip`. If that fails (someone else
     grabbed the trip, or the driver, first), don't treat it as fatal —
     just move on to the next candidate.
   - On success, best-effort mark the driver `busy` and return.
4. If every candidate is exhausted, report that no driver could be
   assigned (distinct from "no drivers were nearby at all").

This "keep trying the next candidate" approach is deliberate: dispatch
happening automatically means multiple trips could be racing for the
same pool of nearby drivers, and a naive "take the first result and fail
if it doesn't work" implementation would make dispatch unreliable
exactly when it matters most (busy periods).

## What's intentionally NOT done yet

Same deferred list as every other service (observability, auth
interceptor, tests) — see `/areas/ride-platform.md`'s consolidated list.
Specific to Dispatch:

- **Not event-driven yet.** The real design is for Dispatch to listen for
  `trip.requested` outbox events from Trip service and dispatch
  automatically the moment a trip is requested. Right now `DispatchTrip`
  is a plain RPC you call manually (same "prove the core logic first"
  reasoning as the rest of Phase 1) — wiring it to consume NATS is part
  of the same future "outbox worker" pass mentioned for every service.
- **No retry/backoff or circuit breaking** on the calls to Trip/Location/
  Driver — a transient failure in any of them right now just fails that
  one dispatch attempt.
- **Fixed search radius default (5km)** — no logic yet to expand the
  search radius if nothing is found nearby.

## Running locally

Trip, Location, and Driver services must already be running (Dispatch
dials them on startup).

```bash
cd services/dispatch-service
cp .env.example .env   # adjust the three *_SERVICE_ADDRESS vars if needed
go mod tidy
go run ./cmd/dispatch-service
```

## Trying it

```bash
# With a trip already in "requested" status (from Trip service):
grpcurl -plaintext -import-path ./proto -proto ride/dispatch/v1/dispatch.proto \
  -d '{"trip_id": "<trip-id>"}' \
  localhost:50056 ride.dispatch.v1.DispatchService/DispatchTrip
```

If it works, `Trip.GetTrip` on that trip ID should now show status
`ACCEPTED` with a `driver_id` filled in — automatically, without calling
`AcceptTrip` yourself.
