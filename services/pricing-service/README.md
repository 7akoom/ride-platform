# Pricing Service

Calculates what a trip costs: quotes for every vehicle class before the
rider books, a single-class estimate, and the final, durable fare when a
trip completes. Staff set the prices here too: rate cards, surge rules and
surges on one zone.

## The pricing pipeline

`QuoteTrip`, `EstimateFare` and `CalculateFare` run the exact same pipeline
(`service_fare.go`), so a quote can never quietly disagree with what the
rider is actually charged:

```
rate card -> distance & duration -> minimum fare -> surge -> discount -> total
```

What does not depend on the vehicle class is gathered once per request, at
the same time: whether the pickup is served (its zone, city and the city's
time zone), the OSRM route, the free drivers nearby, the weather, the
demand and the staff surge.

## Quotes: the price is fixed before the trip

`POST /v1/fare-quotes` (the rider, for their own profile) prices the trip
for every class at once, cheapest first. Each quote has:

- the full fare breakdown and its `quote_id`;
- `expires_at` (`QUOTE_TTL`, 5 minutes by default);
- whether a free driver of that class is near the pickup and how many
  minutes away by road the nearest one is (`pickup_eta_minutes`).

A trip requested with a `quote_id` (trip-service `RequestTrip`) claims it:
trip-service calls `ClaimQuote` with the internal token. A quote is claimed
once, only by its rider and only before it expires; the trip must start and
end within 50 m of the quoted points and be for the quoted class. If the
quote used a coupon that has since ended or been used up, the claim is
refused and the rider asks for a new quote. When the trip completes, its
fare is exactly the quoted one (with the quote's coupon redemption).

A trip requested without a quote is priced when it completes, as before.

Quotes nobody claimed are removed a day after they expire.

## Rate cards

A card applies to a **zone**, a **city**, or **everywhere**, for **one
vehicle class** or **all of them**. The most specific card prices a trip:

```
zone+class, zone, city+class, city, everywhere+class, everywhere
```

A card holds base fare, per km, per minute, minimum fare, the waiting,
cancellation and no-show fees (charged by the trip lifecycle), the most
surge may add (`max_surge_percent`, 0 turns surge off), and whether demand
and weather count.

Cards are **versioned**: setting one inserts a new row and the newest one
for a place and class is in force, so what past fares were priced with
stays on file. Retiring a card inserts a retired version, and trips there
fall back to the next card. The card for every class everywhere always
exists and cannot be retired. The currency, and the average speed and road
correction used when OSRM is down, come from that card: one deployment, one
currency.

Every fare records the rate card version (`config_id`) and, when there is
one, the quote it came from.

## Waiting, cancelling and not showing up

- **Waiting:** when a trip completes, every whole minute the driver waited
  at the pickup (from the driver's arrival to the start) beyond the card's
  free minutes is charged at the card's rate, on top of the fare (quoted or
  not) and never discounted: `waiting_minutes`, `waiting_fare`.
- **Cancellation fee:** pricing consumes `trip.cancelled`. A rider who
  cancels after a driver accepted pays the card's cancellation fee, unless
  it is within the card's grace minutes, or the driver has still not arrived
  15 minutes after accepting (the driver is late).
- **No-show fee:** the driver cancelled because the rider did not come
  (trip-service only allows it after arriving and waiting).
- A fee is a fare of its own `kind` (`cancellation`, `no_show`): it does not
  count as a completed trip nor use a coupon, and `fare.calculated` carries
  the kind so wallet-service settles it as a fee. A quoted trip's fees and
  waiting come from the card it was quoted with; otherwise from the card in
  force at the pickup.

## Surge

```
staff  = the larger of the hour's rule and the zone's surge
total  = staff + demand + weather, capped by the rate card
```

The two staff sources never add up: a zone surge for a concert during rush
hour means "this much", not "this much more". The breakdown is returned
with every price (with `label`, what staff called the rule or surge), so an
app can show "+25% Evening rush" and support can answer "why was my fare
higher".

| Source | How it's determined |
|---|---|
| Hour | `surge_time_rules`, for everywhere, a city or a zone, read in the **local time of the pickup's city**. Overlapping rules take the highest match. |
| Zone surge | Staff put a percent on one zone for a while (up to a day, starting up to a week ahead), with a reason riders see. |
| Demand | Riders who asked for a quote in the zone in the last 10 minutes against the free drivers within 5 km: no driver +50%, 3 riders per driver +50%, 2 +30%, 1.5 +15%. |
| Weather | Open-Meteo current conditions, mapped from WMO weather codes to tiers (thunderstorm > snow > rain > drizzle/fog), plus a wind bump. |

Demand and weather **fail open**: if driver-service or Open-Meteo cannot be
reached, that part adds 0% rather than failing the price (an unreachable
driver-service never looks like an empty street).

### On Open-Meteo and the no-paid-services rule

Open-Meteo is free, requires no API key and no account. It is, however,
an **external service** rather than something self-hosted — the only one
in the project. Two things worth knowing:

1. Their free tier is for non-commercial use. Before selling a
   deployment commercially, check their current terms — they offer a
   paid commercial tier, and they also publish their API as open-source
   software you can self-host if you'd rather keep the zero-dependency
   posture.
2. Because it fails open, and a rate card can turn it off
   (`weather_surge`), everything else keeps working without it.

## Distance: real road routing

Distance and duration come from **OSRM**, a self-hosted open-source
routing engine. OSRM needs a one-time dataset build for your region: see
`infrastructure/osrm/README.md`.

**If OSRM is unreachable**, pricing falls back to a Haversine estimate
(straight-line distance × the card's correction factor, duration from its
average speed) and flags the `Route` as estimated. A quote's driver ETA
falls back the same way (at 25 km/h).

## Discounts: best one wins, never stacked

Three sources are evaluated — the rider's coupon code, the first-ride
discount, and the loyalty discount (every Nth completed trip) — and only
the **single largest** is applied (a coupon wins a tie). Discounts apply to
the **surged** amount, never to the waiting fee. An invalid or unknown code
never fails the request: the rider still gets a price, and every breakdown
says what became of the code (`coupon_status`: applied, not found, ended,
not started, expired, used up, already used by this rider, not in this
area, not for this class, new riders only, below the minimum fare, or a
better discount applied instead).

### Coupons

A coupon is a percentage (with an optional cap on the amount) or a fixed
amount off, valid between two moments, with an optional total number of
uses, a number of uses per rider (1 by default), a minimum fare, and
optionally a city or a zone the pickup must be in, the vehicle classes it is
for, and "new riders only" (riders who never completed a trip). Codes are
3-40 letters, digits, `-` and `_`, stored and matched upper-case.

Riders enter a code when asking for quotes; each class's quote says whether
it applied. Uses are held, not just counted at the end:

```
quote (priced with the coupon)
  -> trip requested with it: ClaimQuote reserves one use (under a lock on
     the coupon: two trips never both get the last one; no use left and
     the request is refused, the rider asks for a new quote)
  -> trip completed: the reservation becomes the use (redeemed)
  -> trip cancelled (trip.cancelled) or never created (ReleaseQuote):
     the use is released for anyone again
```

`redemption_count` counts the uses held (reserved and redeemed). Every hold is
kept in `coupon_redemptions` with its status: the coupon's use log.

Staff change a coupon's description, end, limits, minimum fare and whether it
is on; the code, the discount and where it applies never change (make a new
coupon instead), so the use log always means what it says.

### First ride and loyalty

`promotion_settings` (one row, staff-editable): the first-ride percent and
cap, every how many completed trips the loyalty discount comes and its
percent and cap. A percent of 0 (or `loyalty_every` 0) turns one off.
Defaults: 50% off the first ride, 20% off every 10th, no caps.

## Staff endpoints (`pricing.manage`)

| | |
|---|---|
| `GET /v1/admin/rate-cards` | The card in force for each place and class (filter by `city_id` or `zone_id`). |
| `POST /v1/admin/rate-cards` | Set a card (a whole card; `base_fare`, `per_km_rate`, `per_minute_rate` are required, empty `max_surge_percent` means 150). |
| `POST /v1/admin/rate-cards:retire` | Retire a zone's, city's or class's card. |
| `GET/POST /v1/admin/surge-rules`, `PATCH /v1/admin/surge-rules/{id}`, `POST …:setActive` | Surge by the hour ("HH:MM", local time; a window that ends before it starts runs past midnight). |
| `GET/POST /v1/admin/zone-surges`, `POST /v1/admin/zone-surges/{id}:end` | Surge on one zone for a while. |

| | **`promotions.manage`** |
| `GET/POST /v1/admin/coupons`, `GET/PATCH /v1/admin/coupons/{code}` | Coupons (list by state and code prefix, paged). |
| `GET /v1/admin/coupons/{code}/redemptions` | A coupon's use log, newest first. |
| `GET/PUT /v1/admin/promotion-settings` | The first-ride and loyalty discounts. |

Every call asks staff-service first (fail closed) and is audited there (a
coupon call with the code as its target). `pricing.manage` and
`promotions.manage` are not given to the operations role: only owners hold
them until they give them to a role of their own.

## Idempotency

`CalculateFare` writes one immutable `fares` row per trip. Calling it
again for the same `trip_id` returns the original fare instead of
recalculating; when two calls race, the second returns the first one's
fare.

The write is one transaction covering: the fare row, the rider's
completed-trip counter, the coupon redemption (if any), and the
`fare.calculated` outbox event (which carries the `quote_id`).

## Rider trip counter — why it's local

Pricing keeps its own `rider_trip_stats` counter rather than asking
trip-service "how many rides has this rider completed". It's incremented
exactly once per `CalculateFare`, which happens exactly once per completed
trip.

## Configuration

| Variable | |
|---|---|
| `LOCATION_SERVICE_ADDRESS` | Service zones, cities, nearby drivers. |
| `DRIVER_SERVICE_ADDRESS` | Which nearby drivers are free, and their class. |
| `STAFF_SERVICE_ADDRESS` | Permissions for the staff endpoints. |
| `TRIP_SERVICE_ADDRESS` | Reading a completed trip to price it. |
| `RIDER_SERVICE_ADDRESS` | Ownership: a rider prices only for their own profile. |
| `QUOTE_TTL` | How long a quote holds its price (1m–30m, default 5m). |
| `FARE_ROUNDING_INCREMENT` | Every total is rounded once to a multiple of it (250 IQD). |

Migration `00008` seeds a usable IQD rate card and rush-hour rules.
Migration `00012` adds city cards, fees, quotes and zone surges; existing
cards keep working with no minimum and no fees until staff set them.
Migration `00014` adds coupon scopes, caps and the use log statuses, and
the promotion settings (seeded with the discounts that were built in).

## Tests

```bash
go test ./...
# Repository tests against an empty, throw-away database (psql on PATH):
PRICING_TEST_DATABASE_URL=postgres://.../empty_db \
  go test ./internal/infrastructure/persistence/postgres/
```

## Running locally

```bash
cd services/pricing-service
cp .env.example .env   # adjust DATABASE_URL
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/pricing-service
```

## Trying it

`scripts/e2e/test-fare-quotes.sh` walks through rate cards, surge, quotes
and a trip that pays its quote on the real stack;
`scripts/e2e/test-coupons.sh` through coupons, their uses and the automatic
discounts.
