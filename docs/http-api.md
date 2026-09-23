# Client HTTP API

What the Rider app, the Driver app and the Admin web can call, through the API
gateway (`infrastructure/gateway`, port 8080; TLS terminates in front of it).
Every route below is a `google.api.http` annotation in `proto/`; an RPC without an
annotation is **not reachable** from outside (Send, FindNearby, CalculateFare,
coupons, dispatch, analytics, AuthorizeStaffAction...).

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
| POST | `/v1/trips` | rider | `riderId` must be the caller's rider profile. `pickupAddress` and `dropoffAddress` (at most 300 characters) are kept with the trip as the rider picked them. `pickupSavedAddressId` / `dropoffSavedAddressId` name one of the rider's saved addresses: its point and address replace `pickup` / `dropoff`, and for the pickup its details, note for the captain and photo are copied into the trip (404 for an address that is not the rider's) |
| GET | `/v1/trips/{tripId}` | rider or driver of the trip | poll for status; has `pickupAddress`, `dropoffAddress`, `pickupDetails`, `pickupNote` and `hasPickupPhoto` |
| GET | `/v1/trips/{tripId}/pickup-photo` | rider or driver of the trip | a short-lived `url` to the photo of the saved pickup address, while the trip is accepted or in progress (400 otherwise, 404 without a photo) |
| GET | `/v1/riders/{riderId}/recent-destinations?limit=` | the rider | where their completed trips ended, newest first, each place once (points within about 10 m are one): `coordinates`, `address`, `lastTripAt`; `limit` 5 by default, at most 10 |
| POST | `/v1/trips/{tripId}:start` | driver of the trip | |
| POST | `/v1/trips/{tripId}:complete` | driver of the trip | |
| POST | `/v1/trips/{tripId}:cancel` | rider or driver of the trip | |
| POST | `/v1/trips/{tripId}:sos` | rider or driver, as themselves | `triggeredBy` must match the caller's role |
| POST | `/v1/trips/{tripId}:waypoint` | driver of the trip | every 15-30 s; throttled server-side |
| GET | `/v1/trips/{tripId}/path` | rider or driver of the trip | |
| GET | `/v1/trips/{tripId}/driver-location` | **rider** of the trip | only while accepted or in progress; 404 means the driver has not reported for 30 s, keep polling |
| GET | `/v1/trips:active?rider_id=` or `?driver_id=` | the profile's owner | the requested, accepted or in-progress trip; 404 `no active trip` when there is none. Call it when the app opens, to resume a trip |
| GET | `/v1/trips?rider_id=` or `?driver_id=` | the profile's owner | history, newest first: `page_size` (1-50, default 20) and `page_token`; the response's `nextPageToken` is empty on the last page |
| GET | `/v1/drivers/{driverId}/offer` | the driver | the trip currently offered to them: `tripId`, `pickup`, `dropoff`, `pickupAddress`, `dropoffAddress`, `vehicleClass`, `paymentMethod`, `offeredAt`, `expiresAt` (not who the rider is, nor the pickup note and photo, which come with the trip once accepted); 404 when there is none. Poll about every 2 s while online |
| POST | `/v1/trips/{tripId}:accept-offer` | the driver | body `{"driverId": ...}`; makes them the driver of the trip. 404: no live offer; 400: the offer expired, the trip was cancelled, or they are on another trip |
| POST | `/v1/trips/{tripId}:reject-offer` | the driver | body `{"driverId": ...}`; the trip goes on to the next driver and is not offered to them again |

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
| POST | `/v1/routes:compute` | Best route by road between `origin` and `destination` (each `{latitude, longitude}`): `distanceMeters`, `durationSeconds` and `polyline`, the whole path as a Google encoded polyline with 5 digits of precision, ready to decode and draw. `404` when there is no way between the points, `400` when a point is missing, is not a position on Earth, or is more than 1 km from any road, `503` when the routing engine is down. |
| GET | `/v1/places:search` | Places by name, best match first: curated places (see above) whose name in any language contains the query or is close to it, then places from the map (a map result within 75 m of a curated one is left out). Query: `query` (2 to 200 characters), `near.latitude` and `near.longitude` (optional, ranks close places first without excluding the rest), `limit` (5 by default, at most 10), `language` (`ar`, `ku` or `en`; Arabic first when empty). Each place has `id`, `name`, `displayName`, `category`, `type`, `coordinates` and `address` (road, neighbourhood, suburb, city, state, postcode...); a curated place has `id` `curated/<placeId>`, `curatedPlaceId`, `type` `curated` and its category (`airport`, `mall`...). If one of the two sources is down the other still answers. An empty `places` list means nothing was found. |
| GET | `/v1/places:reverse` | What is at a point. Query: `coordinates.latitude`, `coordinates.longitude`, `language`. `404` when there is nothing there. |

Searching is limited to the country set by `MAPS_COUNTRY_CODES` (Iraq by default).

### Fares

| Method | Path | Who |
|---|---|---|
| POST | `/v1/fare-estimates` | rider, for their own `riderId` |

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
| GET | `/v1/drivers/{driverId}/standing` | the driver | can they take trips, and the amount due if suspended |
| POST | `/v1/drivers/{driverId}/payouts` | the driver | `amount` and `idempotencyKey`; a retry with the same key is safe |
| POST | `/v1/wallet/topups/zaincash` | the driver | returns the ZainCash `redirectUrl` for the webview |
| POST | `/v1/wallet/zaincash/webhook` | ZainCash | not a user: wallet-service exempts it from authentication and verifies the JWT in the body |

`owner_type` is a query parameter because an enum cannot be bound in a URL path.
Top-ups of a rider's wallet and trip settlement are internal: no route.

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

## Not exposed yet

Ratings and analytics.
