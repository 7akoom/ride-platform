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