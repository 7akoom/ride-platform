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

## Deleting an account

As the big ride apps do (`AccountDeletionService`, `/v1/me/deletion`):

- `GET /v1/me/deletion` says whether a deletion is pending and what deleting
  would meet now. identity-service asks rider- and driver-service for the
  person's profiles, trip-service for an active trip or an upcoming booking,
  and wallet-service for unpaid fees, a driver's negative balance or open
  payout (each blocks), and positive balances (lost with the account, so
  confirming needs `acceptBalanceLoss`). A service that does not answer makes
  the check fail (503), never pass.
- `POST /v1/me/deletion:request-otp` sends a code (OTP purpose
  `delete_account`) to one of the account's own sign-in methods, once nothing
  stands in the way; `POST /v1/me/deletion:confirm {challengeId, code,
  acceptBalanceLoss}` checks it (wrong codes count, as for signing in) and
  starts the deletion: every session ends in the same transaction, and
  `identity.deletion_requested` tells the others (a driver goes offline, push
  devices go).
- Signing in again during the grace period (`ACCOUNT_DELETION_GRACE_PERIOD`,
  30 days, at least one) cancels it, in the same transaction as the new
  session (`identity.deletion_cancelled`). While pending, the person cannot be
  found by phone for transfers.
- The eraser (`ACCOUNT_DELETION_CHECK_INTERVAL`) takes due deletions, checks
  again that nothing stands in the way, has media-service delete every file of
  the identity, then erases sign-in methods, the codes sent to them, sessions
  and the PIN, disables the identity and writes `identity.deleted`
  `{identity_id, rider_id, driver_id}`. Anything in the way (or a service not
  answering) postpones it by `ACCOUNT_DELETION_RETRY_AFTER`. The identities
  row stays as the anonymous id other records point at.
- On `identity.deleted` each service erases its part (durable consumers
  `*-account-erasure` on `IDENTITY_EVENTS`): rider and driver profiles are
  renamed "Deleted account" and suspended, with their details, saved
  addresses, documents and name changes deleted and plates replaced;
  trip-service clears passenger and pickup details and rating comments and
  revokes share links; wallet-service records the forfeited balance on the
  ledger, blocks the wallets and clears phones and payout destinations;
  notification-service deletes the inbox. Trips, ledgers and tickets stay.
- The same phone signing in afterwards gets a new, empty account.

End to end: `bash scripts/e2e/test-account-deletion.sh`.

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

