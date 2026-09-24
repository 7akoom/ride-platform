# Identity Service

Identity Service is responsible for authentication and platform identity.

## Responsibilities

- User authentication
- Phone number authentication
- OTP verification
- Access token issuance
- Refresh token management
- Session management
- Device sessions
- Account status
- Authentication security
- Login attempt protection
- Token revocation

## Supported Identity Types

The service must support identities used by:

- Riders
- Drivers
- Tenant administrators
- Tenant staff
- Platform administrators

## Ownership Boundaries

Identity Service owns authentication-related data.

It does not own:

- Rider profiles
- Driver profiles
- Driver vehicles
- Tenant business data
- Trips
- Pricing
- Wallet balances

Other services reference an authenticated user using an immutable identity identifier.

## Communication

Public authentication requests are received through the API Gateway.

Identity Service may expose internal gRPC APIs for trusted service-to-service identity validation when required.

Authentication-related domain events are published asynchronously when useful.

Examples:

- IdentityCreated
- IdentityActivated
- IdentitySuspended
- IdentityDeleted
- PhoneVerified
- SessionRevoked

## Security Principles

- Passwords, secrets, and OTP values must never be stored in plain text.
- Access tokens must have short lifetimes.
- Refresh tokens must be revocable.
- Authentication endpoints must be rate limited.
- OTP requests must be protected against abuse.
- Sessions must be traceable by device.
- Sensitive authentication events must be auditable.

## Wallet PIN

The PIN a person confirms money leaving their wallet with (a transfer to
another rider). It is theirs, like the phone they sign in with, so identity
keeps it; wallet-service asks.

- `GET /v1/me/wallet-pin`, `PUT /v1/me/wallet-pin` (`WalletPinService`): 4 or
  6 digits, not all the same and not a straight run. Changing it takes the
  current one. **Forgotten PIN:** sign in again with a one-time code; within
  10 minutes of that sign-in (the session's start) a new PIN can be set
  without the old one, which also lifts a lock. No new OTP purpose is
  needed: signing in already proves the phone.
- Only a bcrypt hash is kept (`wallet_pins`), over the identity id and the PIN,
  so a hash copied onto another identity never matches.
- 5 wrong PINs in a row lock it for 15 minutes, each lock after that twice as
  long, at most a day. A correct PIN clears the count. Guesses are counted one
  at a time (the row is locked while one is checked), so parallel guesses
  cannot slip past the limit. A wrong current PIN when changing it counts too.

## Internal methods

Called by other services only, with the shared `INTERNAL_SERVICE_TOKEN` (the
same value every service has; the placeholder is refused unless
`APP_ENV=development`). They have no route through the gateway, which also
refuses the internal token:

- `WalletPinService/VerifyWalletPin`: is this person's PIN right (ok, wrong
  with the attempts left, locked until, not set). Counts a wrong one.
- `IdentityDirectoryService/FindIdentityByPhone`: the active identity that
  signs in with an E.164 phone.
- `IdentityDirectoryService/GetIdentityPhone`: the phone an identity signs in
  with.

`NewInternalServiceUnaryInterceptor` lets these through only with that token,
and their handlers refuse a call that did not come through it (fail closed).
Access tokens never reach them, and the token never reaches anything else.

