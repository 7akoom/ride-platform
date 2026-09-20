# Client HTTP API

What the Rider app, the Driver app and the Admin web can call, through the API
gateway (`infrastructure/gateway`, port 8080; TLS terminates in front of it).
Every route below is a `google.api.http` annotation in `proto/`; an RPC without an
annotation is **not reachable** from outside (Send, FindNearby, CalculateFare,
coupons, zone changes, dispatch, analytics...).

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

### Trips (rider and driver)

| Method | Path | Who | Notes |
|---|---|---|---|
| POST | `/v1/trips` | rider | `riderId` must be the caller's rider profile |
| GET | `/v1/trips/{tripId}` | rider or driver of the trip | poll for status |
| POST | `/v1/trips/{tripId}:start` | driver of the trip | |
| POST | `/v1/trips/{tripId}:complete` | driver of the trip | |
| POST | `/v1/trips/{tripId}:cancel` | rider or driver of the trip | |
| POST | `/v1/trips/{tripId}:sos` | rider or driver, as themselves | `triggeredBy` must match the caller's role |
| POST | `/v1/trips/{tripId}:waypoint` | driver of the trip | every 15-30 s; throttled server-side |
| GET | `/v1/trips/{tripId}/path` | rider or driver of the trip | |
| GET | `/v1/trips/{tripId}/driver-location` | **rider** of the trip | only while accepted or in progress; 404 means the driver has not reported for 30 s, keep polling |

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

### Positions and zones

| Method | Path | Who | Notes |
|---|---|---|---|
| PUT | `/v1/locations/{entityId}` | the entity itself | body has `entityType` and `coordinates`; driver app every few seconds |
| GET | `/v1/locations/{entityId}?entity_type=...` | the entity itself | riders follow their driver through `driver-location`, not this |
| GET | `/v1/zones` | any user | `?city=` optional |
| GET | `/v1/zones/{zoneId}` | any user | |
| GET | `/v1/zones:check` | any user | `?coordinates.latitude=&coordinates.longitude=` |

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

## Not exposed yet

Login and sessions (identity), the
trip list and "my active trip", driver offers (accept or reject with a timeout),
ratings, admin and analytics (needs staff roles).
