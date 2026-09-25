# Client HTTP API

What the Rider app, the Driver app and the Admin web can call, through the API
gateway (`infrastructure/gateway`, port 8080; TLS terminates in front of it).
Every route below is a `google.api.http` annotation in `proto/`; an RPC without an
annotation is **not reachable** from outside (Send, FindNearby, CalculateFare,
ClaimQuote, dispatch, analytics, AuthorizeStaffAction...).

## Conventions

- **Credentials:** `Authorization: Bearer <access token>` on every request. The
  token is forwarded to the backend service, which decides what that user may do.
  Anything else in the header (for example the internal service token) is refused
  with 401 at the gateway, and `Grpc-Metadata-*` request headers are dropped.
- **JSON:** field names are lowerCamelCase (`driverId`); requests also accept the
  proto names (`driver_id`). Enums are strings (`AVAILABILITY_STATUS_AVAILABLE`).
  Zero values are present in responses.
- **Query parameters:** on GET, fields that are not in the path are query
  parameters, nested fields with dots (`?coordinates.latitude=36.19`).
- **Errors:** `{"code": <gRPC code>, "message": "...", "details": []}` with the
  HTTP status: 400 invalid argument or precondition failed, 401 no valid token,
  403 not yours, 404 not found, 409 already exists, 5xx server trouble.
- **Ownership:** a user reaches only their own rider or driver profile, wallet,
  devices, inbox and position. An id that is not yours and an id that does not
  exist both answer 403, on purpose.
- **Custom verbs** (`:accept`, `:read`, `:unregister`) are part of the path.

## Routes

### Login and sessions (identity)

| Method | Path | Who | Notes |
|---|---|---|---|
| POST | `/v1/auth/otp:request` | public | `identifier` (phone or email), optional `deliveryChannel`; returns `challengeId` |
| POST | `/v1/auth/otp:verify` | public | `challengeId`, `code`, and the device (`clientId`, `deviceId`, `deviceName`, `platform`, `appVersion`); returns the tokens |
| POST | `/v1/auth/token:refresh` | public | the refresh token in the body is the credential; returns a new pair |
| POST | `/v1/auth/logout` | public | the refresh token in the body ends that session |
| POST | `/v1/auth/logout-all` | public | ends every session of the identity |
| GET | `/v1/me` | the user | identity id, status, verified identifiers |
| GET | `/v1/me/sessions` | the user | device, address, last seen; `isCurrent` marks this one |
| DELETE | `/v1/me/sessions/{sessionId}` | the user | |
| POST | `/v1/me/identifiers/link-otp` | the user | start linking a phone or email |
| POST | `/v1/me/identifiers/link` | the user | finish linking with the code |
| POST | `/v1/me/identifiers/unlink-otp` | the user | start unlinking |
| POST | `/v1/me/identifiers/unlink` | the user | finish unlinking with the code |
| GET | `/v1/me/wallet-pin` | the user | `isSet`, `setAt`, `attemptsLeft`, and `lockedUntil` while wrong PINs keep it locked |
| PUT | `/v1/me/wallet-pin` | the user | `newPin` (4 or 6 digits, not all the same, not a run like 1234 or 654321); to change it also `currentPin` (a wrong one counts: 403). Forgotten PIN: sign in again with a code, then within 10 minutes of signing in `newPin` alone works and lifts a lock. 400 `FAILED_PRECONDITION` when `currentPin` is needed or the PIN is locked |

The public routes need no `Authorization` header. Send the user's language in
`Accept-Language` (`ar`, `ku` or `en`): it picks the language of the OTP message.
The user's address and device are recorded from the request itself.

**Deployment:** identity only trusts the forwarded address, device and language
when the connection comes from a network listed in `TRUSTED_PROXY_CIDRS` (the
gateway, and any proxy in front of it). The proxy in front of the gateway must
append the client's address to `X-Forwarded-For`; for nginx:
`proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`. Without it the
per-source OTP limit counts every user as one source.

### Trips (rider and driver)

| Method | Path | Who | Notes |
|---|---|---|---|
| POST | `/v1/trips` | rider | `riderId` must be the caller's rider profile. `pickupAddress` and `dropoffAddress` (at most 300 characters) are kept with the trip as the rider picked them. `pickupSavedAddressId` / `dropoffSavedAddressId` name one of the rider's saved addresses: its point and address replace `pickup` / `dropoff`, and for the pickup its details, note for the captain and photo are copied into the trip (404 for an address that is not the rider's). `quoteId` (from `/v1/fare-quotes`) fixes the price: the trip gets `quotedFare` and `currencyCode` and the quote's `vehicleClass`; 404 for another rider's or unknown quote, 400 `FAILED_PRECONDITION` when it expired, was used or its coupon ended (ask for a new quote), 400 when pickup or dropoff is more than 50 m from the quoted one or another class is named. A ride for someone else: `passengerName` (at most 80 characters) and `passengerPhone` (E.164, `+` and 8-15 digits), both or neither (400 otherwise); the driver sees them on the trip and calls that number. `stops`: up to 2 places on the way, in order, each `{coordinates, address}` (400 for a third); with a quote they must be the quote's stops, each within 50 m, or the request is refused like any other mismatch |
| GET | `/v1/trips/{tripId}` | rider or driver of the trip | poll for status; has `pickupAddress`, `dropoffAddress`, `pickupDetails`, `pickupNote`, `hasPickupPhoto`, for a quoted trip `quoteId`, `quotedFare`, `currencyCode`, and `arrivedAt`, `cancelledBy`, `riderNoShow`; `passengerName` and `passengerPhone` for a ride for someone else (the phone only while the trip is requested, accepted or in progress), and `scheduled: true` for a trip that was booked ahead; `stops`, each with `reachedAt` once the driver marked it |
| GET | `/v1/trips/{tripId}/pickup-photo` | rider or driver of the trip | a short-lived `url` to the photo of the saved pickup address, while the trip is accepted or in progress (400 otherwise, 404 without a photo) |
| GET | `/v1/riders/{riderId}/recent-destinations?limit=` | the rider | where their completed trips ended, newest first, each place once (points within about 10 m are one): `coordinates`, `address`, `lastTripAt`; `limit` 5 by default, at most 10 |
| POST | `/v1/trips/{tripId}:arrived` | driver of the trip | the driver is at the pickup: only while the trip is accepted and within 200 m of the pickup by their last reported position (400 `FAILED_PRECONDITION` when too far, or when no position was reported in the last 30 s). Sets `arrivedAt` and tells the rider; waiting time and a no-show count from here. Again once marked changes nothing |
| POST | `/v1/trips/{tripId}:reach-stop` | driver of the trip | body `{"position": 1}` (the stop's place in `stops`, from 1): the driver is at that stop, only while the trip is in progress and within 200 m of it by their last reported position (400 `FAILED_PRECONDITION` when too far or not in progress, 404 for a stop the trip does not have). Stops may be reached in any order, or skipped; marking a reached stop again changes nothing. Sets its `reachedAt`, with a `trip.stop_reached` event |
| POST | `/v1/trips/{tripId}:start` | driver of the trip | |
| POST | `/v1/trips/{tripId}:complete` | driver of the trip | |
| POST | `/v1/trips/{tripId}:cancel` | rider or driver of the trip | `reason`; the trip records `cancelledBy` (rider, driver, or system for dispatch or staff). `riderNoShow: true` is the driver cancelling because the rider did not come: only the driver, only after `:arrived` and waiting 5 minutes (`TRIP_NO_SHOW_WAIT`), else 400; the rider pays the rate card's no-show fee. A rider who cancels once a driver accepted pays the cancellation fee after the card's grace minutes, unless the driver has still not arrived 15 minutes after accepting |
| POST | `/v1/trips/{tripId}:sos` | rider or driver, as themselves | `triggeredBy` must match the caller's role |
| POST | `/v1/trips/{tripId}:waypoint` | driver of the trip | every 15-30 s; throttled server-side |
| GET | `/v1/trips/{tripId}/path` | rider or driver of the trip | |
| GET | `/v1/trips/{tripId}/driver-location` | **rider** of the trip | only while accepted or in progress; 404 means the driver has not reported for 30 s, keep polling |
| GET | `/v1/trips:active?rider_id=` or `?driver_id=` | the profile's owner | the requested, accepted or in-progress trip; 404 `no active trip` when there is none. Call it when the app opens, to resume a trip |
| GET | `/v1/trips?rider_id=` or `?driver_id=` | the profile's owner | history, newest first: `page_size` (1-50, default 20) and `page_token`; the response's `nextPageToken` is empty on the last page |
| GET | `/v1/drivers/{driverId}/offer` | the driver | the trip currently offered to them: `tripId`, `pickup`, `dropoff`, `pickupAddress`, `dropoffAddress`, `vehicleClass`, `paymentMethod`, `quotedFare` and `currencyCode` (for a quoted trip), `stops`, `offeredAt`, `expiresAt` (not who the rider is, nor the pickup note and photo, which come with the trip once accepted); 404 when there is none. Poll about every 2 s while online |
| POST | `/v1/trips/{tripId}:accept-offer` | the driver | body `{"driverId": ...}`; makes them the driver of the trip. 404: no live offer; 400: the offer expired, the trip was cancelled, or they are on another trip |
| POST | `/v1/trips/{tripId}:reject-offer` | the driver | body `{"driverId": ...}`; the trip goes on to the next driver and is not offered to them again |
| POST | `/v1/scheduled-trips` | rider | book a trip ahead: `riderId`, `scheduledAt` (RFC 3339, 30 minutes to 7 days from now: `TRIP_SCHEDULE_MIN_AHEAD` / `TRIP_SCHEDULE_MAX_AHEAD`), `idempotencyKey`, and as for `POST /v1/trips`: `pickup`, `dropoff`, the addresses or saved address ids, `vehicleClass`, `paymentMethod`, `passengerName`, `passengerPhone` (no quote: the fare is priced when the trip is requested). The pickup must be in a service zone, whose time zone the booking keeps (`timeZone`, and `scheduledLocal` like `2026-10-01 08:30` in it). At most 3 upcoming bookings (`TRIP_SCHEDULE_MAX_UPCOMING`, 400 `FAILED_PRECONDITION`). `stops` as for `POST /v1/trips`, handed to the trip. The same key again returns the same booking; with other details, 409 |
| GET | `/v1/riders/{riderId}/scheduled-trips?include_past=` | the rider | upcoming bookings, soonest first; with `include_past=true` every booking, newest first (at most 50). Each has `status`: `scheduled`, `dispatched` (with `tripId`: follow it with `GET /v1/trips/{tripId}`), `cancelled`, or `failed` (with `failureReason`) |
| POST | `/v1/scheduled-trips/{scheduledTripId}:cancel` | the rider | body `{"riderId": ...}`; only while `scheduled` (400 `FAILED_PRECONDITION` once dispatched: cancel the trip instead), no fee. 404 for another rider's |

Scheduled trips: 10 minutes before the booked time (`TRIP_SCHEDULE_DISPATCH_LEAD`)
the trip is requested for the rider, with the booking's id as the trip's id,
and dispatch looks for a driver as for any trip. When it cannot be requested
(the rider is on another trip, owes fees, the zone closed), it is tried again
every 30 seconds until 10 minutes after the booked time
(`TRIP_SCHEDULE_GRACE`), then the booking is `failed` with the reason and a
`trip.schedule_failed` event.

Offers: dispatch puts each trip to one driver at a time, who has 15 seconds to
accept or reject it; a trip goes on to the next driver until one accepts. (Until
dispatch is switched to offers, drivers are still assigned automatically.)

`:accept` has a route but is internal (dispatch assigns drivers): through the
gateway it always answers 403 to a user, and the internal token is refused with 401.

### Riders and drivers

| Method | Path | Who |
|---|---|---|
| POST | `/v1/riders` | the caller, for their own identity |
| GET | `/v1/riders/{riderId}` | the rider |
| PATCH | `/v1/riders/{riderId}` | the rider |
| GET | `/v1/identities/{identityId}/rider` | the identity's owner |
| POST | `/v1/drivers` | the caller, for their own identity |
| GET | `/v1/drivers/{driverId}` | the driver |
| PATCH | `/v1/drivers/{driverId}` | the driver |
| PUT | `/v1/drivers/{driverId}/availability` | the driver |
| GET | `/v1/identities/{identityId}/driver` | the identity's owner |

### Saved addresses (rider)

A rider keeps up to 20 addresses: at most one home and one work, and others
with a label. Each has the exact point, the address as shown, `details`
(building, floor: at most 200 characters), a `noteForDriver` (at most 300) and
optionally a photo of the entrance. To add a photo, upload it first
(`/v1/media:upload` with `MEDIA_PURPOSE_ADDRESS_PHOTO`, then `:complete`) and
pass its id as `photoMediaId`; the address then keeps it (the rider can no
longer delete it on its own) and deletes it when it is replaced, removed or the
address is deleted. The captain of a trip requested from the address sees the
note and the photo.

| Method | Path | Who | Notes |
|---|---|---|---|
| POST | `/v1/riders/{riderId}/addresses` | the rider | `kind` (`SAVED_ADDRESS_KIND_HOME`, `_WORK`, `_OTHER`), `label` (required for other, at most 60), `coordinates`, `address`, `details`, `noteForDriver`, `photoMediaId`. 409 for a second home or work, 429 past 20, 400 (precondition) when the photo is not a ready address photo of the rider |
| GET | `/v1/riders/{riderId}/addresses` | the rider | home, then work, then the others by label |
| GET | `/v1/riders/{riderId}/addresses/{addressId}` | the rider | 404 for an address that is not theirs |
| PATCH | `/v1/riders/{riderId}/addresses/{addressId}` | the rider | replaces everything but the photo; `photoMediaId` replaces the photo, `removePhoto: true` removes it |
| DELETE | `/v1/riders/{riderId}/addresses/{addressId}` | the rider | deletes its photo too |

### Positions, cities and zones

A deployment serves one country in one currency. It operates in cities; each
city has its own time zone and the zones (polygons) that are served. A point is
served when an active zone of an active city covers it.

| Method | Path | Who | Notes |
|---|---|---|---|
| PUT | `/v1/locations/{entityId}` | the entity itself | body has `entityType` and `coordinates`; driver app every few seconds |
| GET | `/v1/locations/{entityId}?entity_type=...` | the entity itself | riders follow their driver through `driver-location`, not this |
| GET | `/v1/cities` | any user | active cities: `id`, `name`, `names` (`ar`, `ku`, `en`), `timeZone`, `center` |
| GET | `/v1/cities/{cityId}` | any user | 404 for an inactive city |
| GET | `/v1/zones` | any user | `?city_id=` optional; each zone has `cityId` and `city` (its name) |
| GET | `/v1/zones/{zoneId}` | any user | |
| GET | `/v1/zones:check` | any user | `?coordinates.latitude=&coordinates.longitude=`; when served also `zoneId`, `cityId`, `city`, `timeZone` |

### Curated places

Places staff chose (airports, malls, hotels, hospitals...), named in every
language, each with the exact point to be picked up or dropped at (a gate, an
entrance). They also come first in `/v1/places:search`.

| Method | Path | Who | Notes |
|---|---|---|---|
| GET | `/v1/places?city_id=&category=&near.latitude=&near.longitude=&page_size=&page_token=` | any user | active places, higher `priority` first, then closer to `near`; `category` is `PLACE_CATEGORY_AIRPORT`, `_MALL`, `_HOTEL`, `_HOSPITAL`, `_UNIVERSITY`, `_LANDMARK`, `_STATION`, `_GOVERNMENT`, `_RESTAURANT` or `_OTHER`; `nextPageToken` is empty on the last page |
| GET | `/v1/places/{placeId}` | any user | 404 for an inactive place or one in an inactive city |

### Maps: routes and places

Routes come from a self-hosted OSRM and places from a self-hosted Nominatim, both built from OpenStreetMap. Any signed-in user may call them (rate-limited like every other route).

| Method | Path | What it does |
|---|---|---|
| POST | `/v1/routes:compute` | Best route by road between `origin` and `destination` (each `{latitude, longitude}`), through `via` in order when given (at most 5 points; draw a trip with stops with its stops as `via`): `distanceMeters`, `durationSeconds` and `polyline`, the whole path as a Google encoded polyline with 5 digits of precision, ready to decode and draw. `404` when there is no way between the points, `400` when a point is missing, is not a position on Earth, or is more than 1 km from any road, `503` when the routing engine is down. |
| GET | `/v1/places:search` | Places by name, best match first: curated places (see above) whose name in any language contains the query or is close to it, then places from the map (a map result within 75 m of a curated one is left out). Query: `query` (2 to 200 characters), `near.latitude` and `near.longitude` (optional, ranks close places first without excluding the rest), `limit` (5 by default, at most 10), `language` (`ar`, `ku` or `en`; Arabic first when empty). Each place has `id`, `name`, `displayName`, `category`, `type`, `coordinates` and `address` (road, neighbourhood, suburb, city, state, postcode...); a curated place has `id` `curated/<placeId>`, `curatedPlaceId`, `type` `curated` and its category (`airport`, `mall`...). If one of the two sources is down the other still answers. An empty `places` list means nothing was found. |
| GET | `/v1/places:reverse` | What is at a point. Query: `coordinates.latitude`, `coordinates.longitude`, `language`. `404` when there is nothing there. |

Searching is limited to the country set by `MAPS_COUNTRY_CODES` (Iraq by default).

### Fares

| Method | Path | Who | Notes |
|---|---|---|---|
| POST | `/v1/fare-quotes` | rider, for their own `riderId` | body `riderId`, `pickup`, `dropoff`, optional `couponCode` and `stops` (up to 2 `{latitude, longitude}` on the way, in order: the route and the price go through them; a trip requested with the quote must have the same stops). `quotes`: one per vehicle class, cheapest first, each with `quoteId`, `vehicleClass`, `fare` (the breakdown), `expiresAt` (5 minutes), `driversAvailable` and `pickupEtaMinutes` (the nearest free driver of that class by road; 0 when none). Request the trip with the `quoteId` to pay exactly that fare |
| POST | `/v1/fare-estimates` | rider, for their own `riderId` | one class, nothing held; `stops` as for a quote |

A fare breakdown's `surge` has `timeOfDayPercent`, `zonePercent`, `demandPercent`,
`weatherPercent`, `totalPercent`, `multiplier` and `label` (the name staff gave the
rush-hour rule or zone surge in force, to show the rider). `minimumFareAdjustment`
is what raised the trip to the minimum fare. Money is a decimal string.

A completed trip's fare adds `waitingFare` for the `waitingMinutes` the driver
waited at the pickup (from `:arrived` to `:start`) beyond the card's free minutes;
it is never discounted. A cancelled trip's fee is a fare of its own `kind`
(`cancellation` or `no_show`).

Every breakdown says what became of the `couponCode` in `couponStatus`:
`COUPON_STATUS_APPLIED`, or why it took nothing off (`NOT_FOUND`, `ENDED`,
`NOT_STARTED`, `EXPIRED`, `USED_UP`, `ALREADY_USED` by this rider, `NOT_IN_AREA`,
`NOT_FOR_CLASS`, `NEW_RIDERS_ONLY`, `BELOW_MINIMUM`, or `BETTER_DISCOUNT` when a
larger first-ride or loyalty discount applies instead: discounts never add up);
`COUPON_STATUS_UNSPECIFIED` when no code was entered. It is per quote, since a
coupon may be for some classes only. A trip requested with a quote that used a
coupon holds one of its uses until the trip completes (the use counts) or is
cancelled (it is freed); if the last use went meanwhile, the trip request is
refused with `FAILED_PRECONDITION` and the rider asks for a new quote.

### Push devices and inbox

| Method | Path | Who | Notes |
|---|---|---|---|
| POST | `/v1/devices` | the recipient | body: `recipientType`, `recipientId`, `deviceToken`, `platform`, `locale` |
| POST | `/v1/devices:unregister` | the device's owner | token in the body: push tokens contain `:` |
| GET | `/v1/notifications` | the recipient | `?recipient_type=&recipient_id=&limit=&unread_only=` |
| POST | `/v1/notifications:read` | the recipient | empty `notificationIds` marks everything read |

### Wallet

| Method | Path | Who | Notes |
|---|---|---|---|
| GET | `/v1/wallets/{ownerId}?owner_type=OWNER_TYPE_DRIVER` | the owner | `ownerId` is the rider or driver id; balances are decimal strings |
| GET | `/v1/wallets/{ownerId}/transactions?owner_type=&limit=` | the owner | signed decimal `amount`: negative means money left |
| GET | `/v1/wallets/{ownerId}/trips/{tripId}/settlement?owner_type=` | the rider or driver of the trip | how the trip's money moved: `kind` (`trip`, or `cancellation` / `no_show` for a cancelled trip's fee), `fareAmount`, `walletAmount`, `cashAmount`, `changeAmount`, `tipAmount`, and for a fee `dueAmount`: what the rider's wallet could not cover, and `duePaid`: how much of it was paid since. The driver also sees `commissionAmount` and `driverEarning` |
| GET | `/v1/wallets/{ownerId}/statement?owner_type=&from=&to=&direction=&types=&page_size=&page_token=` | the owner | the wallet over a period (`from`/`to` RFC 3339; default the last 30 days, at most 366): `openingBalance` (before `from`), `closingBalance` (at `to`), `totalIn`, `totalOut` (both positive, of the rows the filters keep), and `entries` newest first (50 a page, at most 200). `direction` is `in` or `out`; `types` repeats (`TRANSACTION_TYPE_TOP_UP`, …); 400 for anything else, a reversed or too long period, or a bad token |
| GET | `/v1/wallets/{riderId}/dues` | the rider | unpaid fees of cancelled trips: `outstanding`, `currencyCode`, `dues` (each `tripId`, `kind`, `amount`, `paid`, `outstanding`, `createdAt`, oldest first) and `canRequestTrips` (false while fees are owed and the deployment blocks trips until they are paid). The next money that reaches the wallet pays them, oldest first |
| POST | `/v1/wallets/{riderId}/transfers` | the rider | send money to another registered rider: `recipientPhone` (E.164, `+9647…`), `amount`, `note` (at most 140), `pin` (the sender's wallet PIN), `idempotencyKey` (required; the same key again returns the same transfer, with another amount or phone 409). 403 a wrong PIN (the message says how many attempts are left), 400 `FAILED_PRECONDITION` no PIN yet, PIN locked, not enough money, or the 24-hour limits; 404 when no rider signs in with that phone; 400 to oneself, or below/above the per-transfer limits. Returns `transfer` and the sender's `wallet`. The recipient gets a push |
| GET | `/v1/wallets/{riderId}/transfers?page_size=&page_token=` | the rider | sent and received, newest first: `direction` (`sent` / `received`), `counterpartPhone`, `amount`, `currencyCode`, `note`, `createdAt`; `nextPageToken` |
| POST | `/v1/wallets/{riderId}/money-requests` | the rider | ask for money: `payerPhone` (a registered rider, E.164) or empty for an open request anyone with its code may pay (a link or a QR code), `amount` (within the transfer limits), `note`, `expiresInHours` (1-168, default 72), `idempotencyKey` (required; the same key again returns the same request, with another amount or phone 409). Returns `moneyRequest` with its `code` (10 characters, no 0/O/1/I/L). The rider asked gets a push whose data carries `money_request_code` |
| GET | `/v1/wallets/{riderId}/money-requests?role=&status=&page_size=&page_token=` | the rider | `role` `outgoing` (asked by the rider) or `incoming` (for the rider, or paid by them); `status` `pending`, `paid`, `declined`, `cancelled` or `expired`; newest first |
| GET | `/v1/wallets/{riderId}/money-requests/{code}` | the rider | opens a request by its code (any case). A request for one rider is seen only by that rider and the requester (404 for anyone else); an open one by anyone, who sees `role` `viewer` and the requester's phone masked |
| POST | `/v1/wallets/{riderId}/money-requests/{code}:pay` | the rider paying | `pin`: a transfer from this rider to the requester, once. Paying again returns the same payment; 400 `FAILED_PRECONDITION` once it is paid (by anyone), declined, cancelled or expired, or for one's own request; the PIN, balance and limit errors of a transfer. Returns `moneyRequest`, `transfer` and the payer's `wallet` |
| POST | `/v1/wallets/{riderId}/money-requests/{code}:decline` | the rider asked | only a pending request for them (403 otherwise) |
| POST | `/v1/wallets/{riderId}/money-requests/{code}:cancel` | the requester | only their own pending request (403 otherwise) |
| POST | `/v1/wallets/{riderId}/vouchers:redeem` | the rider | `code` as printed (spaces, dashes and case do not matter): the voucher's amount reaches the wallet (paying unpaid fees first). Returns `serial`, `amount`, `currencyCode`, the `wallet` and the `transaction` (`TRANSACTION_TYPE_VOUCHER`); the same rider again gets the same redemption. 404 not a valid code (or not on sale yet), 400 `FAILED_PRECONDITION` already used, cancelled or expired, 400 `INVALID_ARGUMENT` not in the form of a code (not counted). Every other failed code counts: after 5 in an hour (deployment settings) 429 `RESOURCE_EXHAUSTED`, the message says until when |
| GET | `/v1/drivers/{driverId}/standing` | the driver | can they take trips, and the amount due if suspended |
| POST | `/v1/drivers/{driverId}/payouts` | the driver | `amount` (at least the minimum payout), `destination` (where to send it, at most 120), `idempotencyKey` (the same key again returns the same request; for another amount 409). The amount is held at once: `payout` (`status` `pending`), the `wallet` and the hold's `transaction`. 400 `FAILED_PRECONDITION` below the minimum, more than the balance, a suspended driver, or a request still open |
| GET | `/v1/drivers/{driverId}/payouts?page_size=&page_token=` | the driver | their requests, newest first: `status` `pending`, `approved`, `paid` (with `paidReference`) or `rejected` (with `rejectReason`; the amount came back) |
| POST | `/v1/wallets/{ownerId}/topups` | the owner (rider or driver) | `ownerType`, `amount` (within the deployment's top-up limits; ZainCash takes whole dinars), `provider` (empty: `zaincash`). Returns `topUpId`, `status` `pending` and the provider's `redirectUrl`: the app opens it and the customer pays on the provider's page (the app never takes a card number or a ZainCash PIN). 400 `FAILED_PRECONDITION` below or above the limits, or an amount the provider does not take; 400 an unknown provider |
| POST | `/v1/wallet/topups/zaincash` | the driver | the older form of the same: `driverId` and `amount` |
| GET | `/v1/wallets/{ownerId}/topups/{topUpId}?owner_type=` | the owner | how it stands after the provider's page sent the customer back: `status` `pending`, `succeeded` (the wallet is credited) or `failed`, `provider`, `amount`. 404 for anyone else's |
| POST | `/v1/wallets/{riderId}/trips/{tripId}/tip` | the trip's rider | `amount` (within the tip limits), `idempotencyKey`: from the rider's wallet to the driver's, no commission, once per trip, within 72 hours of a completed trip. 404 not the rider's completed trip; 400 `FAILED_PRECONDITION` tipped already, a cancelled trip's fee, too late, not enough money, below or above the limits. Returns `tip` and the rider's `wallet`; rows of type `TRANSACTION_TYPE_TIP` on both wallets, and `tipAmount` on the trip's settlement |
| POST | `/v1/wallet/zaincash/webhook` | ZainCash | not a user: wallet-service exempts it from authentication and verifies the JWT in the body |

`owner_type` is a query parameter because an enum cannot be bound in a URL path.
Top-ups of a rider's wallet and trip settlement are internal: no route.
A transfer is a `TRANSACTION_TYPE_TRANSFER_OUT` row in the sender's ledger and a
`TRANSACTION_TYPE_TRANSFER_IN` row in the recipient's, both with its `transferId`.
The limits (least and most per transfer, how much and how many in any 24 hours)
are on the wallet config. A paid money request is such a transfer.
A cancelled trip's fee the wallet could not cover is paid by the next money that
reaches it: a `TRANSACTION_TYPE_DUE_PAYMENT` row with the fee's `tripId`, right
after the credit that paid it. Until then `POST /v1/trips` answers 400
`FAILED_PRECONDITION` ("the rider owes fees…", with the amount) when the
deployment blocks trips with unpaid fees (`wallet_configs.block_trips_with_dues`,
on by default).

### Files (media)

Files never go through the gateway. The app asks for an upload URL, sends the
bytes straight to the object store, then asks the service to check them. See
`services/media-service/README.md` for what the check does.

1. `POST /v1/media:upload` with `purpose`, `contentType` and `sizeBytes`
   (the exact size). The answer has `media.id`, `uploadUrl`, `uploadMethod`
   (`PUT`), `uploadHeaders` and `expiresAt`.
2. `PUT` the file to `uploadUrl` with exactly `uploadHeaders`. Another type
   or size is refused by the store (403).
3. `POST /v1/media/{mediaId}:complete`. `media.status` is then
   `MEDIA_STATUS_READY`, or `MEDIA_STATUS_REJECTED` with `rejectionReason`
   (the file was not what it said, is damaged, or is a PDF with scripts).
   Before the bytes arrive it answers 400 (precondition failed).
4. `GET /v1/media/{mediaId}:download` for a short-lived `url`.

| Method | Path | Who | Notes |
|---|---|---|---|
| POST | `/v1/media:upload` | any signed-in user | `purpose`: `MEDIA_PURPOSE_DRIVER_DOCUMENT` (JPEG/PNG/WebP/PDF, 10 MB), `MEDIA_PURPOSE_PROFILE_PHOTO` (JPEG/PNG/WebP, 5 MB), `MEDIA_PURPOSE_ADDRESS_PHOTO` (8 MB), `MEDIA_PURPOSE_SUPPORT_ATTACHMENT` (JPEG/PNG/WebP/PDF, 10 MB); 429 with too many uploads not completed |
| POST | `/v1/media/{mediaId}:complete` | the owner | calling it again on a READY file returns it unchanged |
| GET | `/v1/media/{mediaId}` | the owner, or `media.read` | |
| GET | `/v1/media/{mediaId}:download` | the owner, or `media.read` | READY files only |
| DELETE | `/v1/media/{mediaId}` | the owner | 400 while a service holds the file (a driver's submitted document) |

A file that does not exist answers 403, like someone else's file.

### Staff and admin

Staff sign in with an email OTP like anyone else. What they may do comes from
their roles in staff-service; every admin call is recorded in the audit log
before it runs. A caller who is not staff, or lacks the permission, gets 403.

| Method | Path | Who | Notes |
|---|---|---|---|
| GET | `/v1/staff/me` | any signed-in user | 404 unless staff; the staff record, roles and effective `permissions` |
| POST | `/v1/staff/me:accept` | the invited person | binds the invitation sent to one of the caller's verified emails |
| GET | `/v1/admin/staff?status=&page_size=&page_token=` | `staff.read` | newest first |
| GET | `/v1/admin/staff/{staffId}` | `staff.read` | |
| POST | `/v1/admin/staff` | `staff.manage` | `email`, `displayName`, `roleIds`; only permissions the inviter holds |
| PUT | `/v1/admin/staff/{staffId}/roles` | `staff.manage` | `roleIds`; never your own; only owners touch owners |
| POST | `/v1/admin/staff/{staffId}:suspend` / `:reactivate` / `:revoke` | `staff.manage` | revoke cancels an invitation not accepted yet |
| GET | `/v1/admin/roles` · `/v1/admin/permissions` | `staff.read` | |
| POST · PATCH · DELETE | `/v1/admin/roles`, `/v1/admin/roles/{roleId}` | `roles.manage` | custom roles only |
| GET | `/v1/admin/audit?actor_staff_id=&permission=&target_id=&occurred_after=&occurred_before=&page_size=&page_token=` | `audit.read` | newest first |
| GET | `/v1/admin/drivers?status=&page_size=&page_token=` | `drivers.read` | the review queue is `status=DRIVER_STATUS_PENDING` |
| GET | `/v1/drivers/{driverId}` | the driver, or `drivers.read` | |
| POST | `/v1/admin/drivers/{driverId}:approve` | `drivers.approve` | clears any earlier rejection reason |
| POST | `/v1/admin/drivers/{driverId}:reject` | `drivers.approve` | `reason` is required and shown to the driver (`rejectionReason`) |
| GET | `/v1/admin/cities` | `zones.manage` | every city, inactive ones too |
| POST | `/v1/admin/cities` | `zones.manage` | `name`, `names` (`{"ar":…, "ku":…, "en":…}`), `timeZone` (IANA, for example `Asia/Baghdad`), `center`; 409 for a name already used |
| PATCH | `/v1/admin/cities/{cityId}` | `zones.manage` | replaces `name`, `names`, `timeZone`, `center` |
| POST | `/v1/admin/cities/{cityId}:setActive` | `zones.manage` | `active`; an inactive city serves none of its zones |
| POST | `/v1/admin/zones` | `zones.manage` | `cityId`, `name`, `boundary` |
| PATCH | `/v1/admin/zones/{zoneId}` | `zones.manage` | `name`, `boundary` |
| POST | `/v1/admin/zones/{zoneId}:setActive` | `zones.manage` | `active` |
| GET | `/v1/admin/places?city_id=&category=&page_size=&page_token=` | `places.manage` | every curated place, inactive ones too |
| POST | `/v1/admin/places` | `places.manage` | `cityId`, `category`, `name`, `names`, `address` (at most 300 characters), `coordinates`, `priority` (-1000 to 1000) |
| PATCH | `/v1/admin/places/{placeId}` | `places.manage` | replaces everything but the city |
| POST | `/v1/admin/places/{placeId}:setActive` | `places.manage` | `active` |
| GET | `/v1/media/{mediaId}` · `/v1/media/{mediaId}:download` | `media.read` | any user's file (see Files) |
| GET | `/v1/admin/rate-cards?city_id=&zone_id=` | `pricing.manage` | the card in force for each place and class |
| POST | `/v1/admin/rate-cards` | `pricing.manage` | a whole card for a place (`zoneId`, `cityId` or neither for everywhere; never both) and class (`vehicleClass`, empty for every class): `baseFare`, `perKmRate`, `perMinuteRate` (required), `minimumFare`, `freeWaitingMinutes`, `waitingPerMinute`, `cancellationFee`, `cancellationGraceMinutes`, `noShowFee`, `maxSurgePercent` (0-300, empty = 150, 0 turns surge off), `demandSurge`, `weatherSurge`. Adds a version; the most specific card prices a trip: zone, city, everywhere |
| POST | `/v1/admin/rate-cards:retire` | `pricing.manage` | `zoneId` / `cityId` / `vehicleClass`: trips there use the next card; the card for every class everywhere cannot be retired |
| GET · POST | `/v1/admin/surge-rules?city_id=&zone_id=` | `pricing.manage` | `label`, `zoneId` or `cityId` (or neither), `dayOfWeek` (0 Sunday-6, unset = every day), `startTime`, `endTime` ("HH:MM", the city's local time; past midnight when the end is earlier), `surgePercent` (up to 300) |
| PATCH | `/v1/admin/surge-rules/{ruleId}` | `pricing.manage` | replaces label, day, hours and percent |
| POST | `/v1/admin/surge-rules/{ruleId}:setActive` | `pricing.manage` | `active` |
| GET | `/v1/admin/zone-surges?zone_id=&include_past=` | `pricing.manage` | running and coming ones (with `include_past`, the latest 100) |
| POST | `/v1/admin/zone-surges` | `pricing.manage` | `zoneId`, `surgePercent` (up to 300), `reason` (shown to riders), `durationMinutes` (1-1440), optional `startsAt` (up to 7 days ahead). The larger of it and the hour's rule applies, they never add up |
| POST | `/v1/admin/zone-surges/{zoneSurgeId}:end` | `pricing.manage` | ends it now, or calls off one not started yet |
| GET | `/v1/admin/coupons?state=&query=&page_size=&page_token=` | `promotions.manage` | newest first; `state` `running`, `scheduled` or `finished` (expired, used up or turned off), `query` the start of the code; `nextPageToken` |
| POST | `/v1/admin/coupons` | `promotions.manage` | `code` (3-40 letters, digits, `-`, `_`; stored upper-case; 409 when taken), `description`, `discountType` (`DISCOUNT_TYPE_PERCENTAGE` 0-100 with an optional `maxDiscountAmount`, or `DISCOUNT_TYPE_FIXED_AMOUNT`), `discountValue`, `validFrom` (default now), `validUntil`, `maxRedemptions` (0 unlimited), `perRiderLimit` (default 1), `minimumFareAmount`, `cityId` or `zoneId` (not both), `vehicleClasses` (empty: all), `newRidersOnly`. A coupon has `state` (`running`, `scheduled`, `expired`, `used_up`, `ended`) and `redemptionCount` (uses held: completed trips and trips under way) |
| GET | `/v1/admin/coupons/{code}` | `promotions.manage` | also `redeemedCount` and `discountGiven` (completed trips) |
| PATCH | `/v1/admin/coupons/{code}` | `promotions.manage` | only the fields sent change: `description`, `validUntil`, `maxRedemptions` (0 unlimited), `perRiderLimit`, `minimumFareAmount`, `active`. The code, the discount and where it applies never change |
| GET | `/v1/admin/coupons/{code}/redemptions?page_size=&page_token=` | `promotions.manage` | newest first: `riderId`, `tripId`, `quoteId`, `status` (`reserved` while the trip runs, `redeemed`, `released` when it was cancelled), `discountAmount`, `createdAt`, `releasedAt` |
| GET · PUT | `/v1/admin/promotion-settings` | `promotions.manage` | `firstRidePercent`, `firstRideMaxAmount`, `loyaltyEvery` (every Nth completed trip, 2-100; 0 off), `loyaltyPercent`, `loyaltyMaxAmount` (empty: no cap); a percent of 0 turns that discount off. PUT replaces them all |
| GET | `/v1/admin/voucher-batches?status=&page_size=&page_token=` | `vouchers.manage` | newest first; `status` `created`, `exported` or `cancelled`. A batch has `number`, `label`, `seller`, `amount`, `currencyCode`, `quantity`, `status`, `expiresAt`, `redeemedCount`, `voidCount`, `redeemedAmount` |
| POST | `/v1/admin/voucher-batches` | `vouchers.manage` | `label` (1-120), `seller` (at most 60), `amount`, `quantity` (1-10000), `expiresAt` (1 hour to 3 years ahead; default a year), `idempotencyKey` (required; the same key again returns the same batch, for another batch 409). The codes are generated and kept sealed; nothing is redeemable until the export |
| GET | `/v1/admin/voucher-batches/{batchId}` | `vouchers.manage` | the batch with its counts |
| POST | `/v1/admin/voucher-batches/{batchId}:export` | `vouchers.manage` | ONCE: `vouchers` (`serial`, `code` as `XXXX-XXXX-XXXX-XXXX`) and `csv` (`serial,code,amount,currency,expires_at`) for the seller; the batch becomes `exported` (redeemable) and the codes are wiped. 400 `FAILED_PRECONDITION` for a batch exported or cancelled already. A lost export is a batch to cancel and issue again |
| POST | `/v1/admin/voucher-batches/{batchId}:cancel` | `vouchers.manage` | `reason` (3-300): no voucher of it is redeemable any more; redeemed ones stay |
| GET | `/v1/admin/vouchers/{serial}` | `vouchers.manage` | by the serial printed on the card (`V12-00042`): `status` (`available`, `redeemed`, `void`), `redeemable` now, `redeemedByRiderId`, `redeemedAt`, `transactionId`, `voidedAt`, `voidReason` |
| POST | `/v1/admin/vouchers/{serial}:void` | `vouchers.manage` | `reason` (3-300): one voucher not redeemed yet (a card reported lost) |
| GET | `/v1/admin/wallets/{ownerId}?owner_type=` | `wallets.read` | any wallet: `wallet`, `recentTransactions` (20), `outstandingDues` (a rider's unpaid fees), `openPayouts` (a driver's) |
| GET | `/v1/admin/wallets/{ownerId}/statement?owner_type=&from=&to=&direction=&types=&page_size=&page_token=` | `wallets.read` | the statement of any wallet, as above |
| POST | `/v1/admin/wallets/{ownerId}/adjustments` | `wallets.adjust` | `ownerType`, `amount` (signed: positive credits, negative debits), `reason` (3-300, written on the ledger row), `idempotencyKey`. A rider's balance never goes below zero (400); a driver's may, and the suspension follows. Returns `adjustment` and the `wallet` |
| POST | `/v1/admin/trips/{tripId}/refunds` | `wallets.adjust` | `amount` (to the rider), `driverAmount` (0 to `amount`: taken back from the trip's driver; the platform pays the rest), `reason`, `idempotencyKey`. 404 a trip not settled; 400 `FAILED_PRECONDITION` when the trip's refunds would pass its fare (or fee). Rows of type `TRANSACTION_TYPE_REFUND` |
| GET | `/v1/admin/trips/{tripId}/refunds` | `wallets.read` | `refunds`, `paidAmount` (the fare or fee), `refundedAmount` |
| GET | `/v1/admin/payouts?status=&page_size=&page_token=` | `payouts.manage` | the queue, oldest first |
| POST | `/v1/admin/payouts/{payoutId}:approve` | `payouts.manage` | a pending request |
| POST | `/v1/admin/payouts/{payoutId}:markPaid` | `payouts.manage` | `reference` (1-120): a pending or approved request, once the money reached the driver |
| POST | `/v1/admin/payouts/{payoutId}:reject` | `payouts.manage` | `reason` (3-300, the driver sees it): the held amount comes back (`TRANSACTION_TYPE_PAYOUT_RETURN`); not once paid |

## Not exposed yet

Ratings and analytics.
