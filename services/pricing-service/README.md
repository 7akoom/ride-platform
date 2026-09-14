# Pricing Service

Calculates what a trip costs — both as an upfront estimate and as the
final, durable fare when a trip completes.

## The pricing pipeline

Both `EstimateFare` and `CalculateFare` run the exact same pipeline
(shared in `service_fare.go`) so an estimate can never quietly disagree
with what the rider is actually charged:

```
rate card -> distance & duration -> surge -> discount -> total
```

## Distance: real road routing

Distance and duration come from **OSRM**, a self-hosted open-source
routing engine — real road-network values, not straight-line guesses.
No API keys, no usage limits, no per-request cost.

OSRM needs a one-time dataset build for your region before it'll run:
see `infrastructure/osrm/README.md`. The region is configurable per
deployment.

**If OSRM is unreachable**, pricing falls back to a Haversine estimate
(straight-line distance × a correction factor, duration from an assumed
average speed) and flags the `Route` as estimated. A rider should never
be unable to get a price because the routing engine is down — they get a
slightly less accurate one instead.

Because duration now comes from real routing, the `average_speed_kmh`
and `distance_correction_factor` columns in `pricing_configs` only
affect the fallback path.

## Surge: three independent sources

Each contributes an **additive percentage**, summed then capped at +150%
total. The breakdown is returned to the caller (not just the final
multiplier) so an app can show "+25% evening rush" and support can
answer "why was my fare higher".

| Source | How it's determined |
|---|---|
| Time of day | Configurable rules in `surge_time_rules` (seeded with rush hours + late night). Overlapping rules take the **highest** match, never the sum. |
| Demand | Scarcity of available drivers near the pickup point, via location-service. Fewer drivers → higher surge. |
| Weather | Open-Meteo current conditions, mapped from WMO weather codes to tiers (thunderstorm > snow > rain > drizzle/fog), plus a wind bump. |

Both demand and weather **fail open**: if location-service or Open-Meteo
is unreachable, that component contributes 0% rather than failing the
whole fare. A rider should never be unable to get a price because a
weather API had a bad day.

### On Open-Meteo and the no-paid-services rule

Open-Meteo is free, requires no API key and no account. It is, however,
an **external service** rather than something self-hosted — the only one
in the project. Two things worth knowing:

1. Their free tier is for non-commercial use. Before selling a
   deployment commercially, check their current terms — they offer a
   paid commercial tier, and they also publish their API as open-source
   software you can self-host if you'd rather keep the zero-dependency
   posture.
2. Because it fails open, you can simply not configure it and everything
   else keeps working — weather surge just stays at 0%.

## Discounts: best one wins, never stacked

Three sources are evaluated — coupon code, first-ride, and loyalty
(every 10th ride) — and only the **single largest** is applied.
Stacking is deliberately not allowed: it would let a promo-hunting rider
combine a coupon with a first-ride discount for a near-free trip.

Discounts apply to the **surged** amount, not the pre-surge subtotal, so
a percentage coupon isn't worth less exactly when the rider is paying
most.

An invalid or unknown coupon code doesn't fail the request — the rider
still gets a valid price, just without the discount.

## Idempotency

`CalculateFare` writes one immutable `fares` row per trip. Calling it
again for the same `trip_id` returns the original fare instead of
recalculating — a trip's price must not change after the fact just
because surge conditions differ on a retry.

The write is one transaction covering: the fare row, the rider's
completed-trip counter, the coupon redemption (if any), and the
`fare.calculated` outbox event.

## Rider trip counter — why it's local

Pricing keeps its own `rider_trip_stats` counter rather than asking
trip-service "how many rides has this rider completed". That avoids
adding a cross-service dependency purely for a discount-eligibility
check. It's incremented exactly once per `CalculateFare`, which happens
exactly once per completed trip.

## Configuration

`pricing_configs` is **versioned** — changing rates means inserting a new
row, and the newest wins. Old rows stay as an audit trail of what past
fares were calculated from. Currency is per-deployment (one row, one
`currency_code`), matching the "sell a separate instance per client"
model.

Migration `00008` seeds a usable IQD rate card and rush-hour rules so the
service works immediately. Replace them for a real deployment.

## What's intentionally NOT done yet

Beyond the shared deferred list (observability, auth, tests, NATS
worker):

- **Money is `float64`**, matching the codebase's existing convention.
  A payments-critical system would normally use fixed-point or integer
  minor units to avoid rounding drift. Worth revisiting before real
  money moves through Wallet service.
- **Demand surge uses driver scarcity only** — it doesn't yet weigh how
  many riders are requesting trips in the area, which would need a new
  trip-service endpoint.
- **No admin RPCs for rates or surge rules** — change them with SQL for
  now.
- **No search radius expansion** if no drivers are found nearby.

## Running locally

location-service should be running (pricing dials it for demand surge —
though it fails open if absent).

```bash
cd services/pricing-service
cp .env.example .env   # adjust DATABASE_URL
go mod tidy
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/pricing-service
```

## Trying it

```bash
# Estimate a fare
grpcurl -plaintext -import-path ./proto -proto ride/pricing/v1/pricing.proto \
  -d '{"rider_id": "<rider-id>", "pickup": {"latitude": 36.19, "longitude": 44.01}, "dropoff": {"latitude": 36.20, "longitude": 44.05}}' \
  localhost:50057 ride.pricing.v1.PricingService/EstimateFare

# Create a coupon
grpcurl -plaintext -import-path ./proto -proto ride/pricing/v1/pricing.proto \
  -d '{"code": "WELCOME20", "discount_type": "DISCOUNT_TYPE_PERCENTAGE", "discount_value": 20, "valid_from": "2026-01-01T00:00:00Z", "valid_until": "2027-01-01T00:00:00Z", "per_rider_limit": 1}' \
  localhost:50057 ride.pricing.v1.PricingService/CreateCoupon
```
