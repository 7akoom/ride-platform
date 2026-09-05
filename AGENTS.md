# Ride Platform Engineering Instructions

## Scope

This repository contains a production-grade white-label mobility platform built as Go microservices.

Preserve the existing architecture, contracts, boundaries, and engineering decisions unless the task explicitly requires a change.

Before modifying code, inspect the relevant implementation, tests, git diff, and surrounding architecture.

## Git Safety

- Never commit unless explicitly instructed.
- Never push unless explicitly instructed.
- Never merge branches.
- Never rebase or rewrite git history.
- Never reset or discard existing user changes.
- Never delete untracked files merely because they are unrelated to the current task.
- The working tree may contain substantial pre-existing work. Preserve it.
- Review the diff before and after each task.

## Change Discipline

- Make only changes required by the current task.
- Do not perform unrelated refactors.
- Do not rename packages, public APIs, RPCs, database fields, events, or configuration keys without explicit instruction.
- Preserve existing API contracts unless explicitly told otherwise.
- Prefer additive changes over breaking changes.
- Follow the patterns already used in the relevant service.
- Production code should remain concise and should not contain long explanatory comments.
- Tests should verify behavior rather than implementation details where practical.

## Go

After modifying Go code:

- Run gofmt on modified Go files.
- Run focused tests for affected packages first.
- Before declaring a substantial task complete, run:

  go test -count=1 ./...

- Run race tests when concurrency, authentication, sessions, refresh tokens, persistence, background workers, shared state, or provider routing are involved:

  go test -race -count=1 ./...

Do not hide failing tests. Investigate and report failures.

## Security

Security-sensitive code must fail closed.

Never weaken:

- authentication
- authorization
- session ownership checks
- refresh-token rotation
- refresh-token reuse detection
- identity lifecycle revocation
- OTP brute-force protection
- OTP replay protection
- rate limiting
- webhook signature verification
- secret redaction
- access-token verification
- transactional consistency

Do not trust client-supplied forwarded IP headers or metadata as authoritative network identity.

Never expose secrets, API keys, access tokens, refresh tokens, OTP codes, private keys, passwords, cookies, or raw .env contents in logs, tests, output, or reports.

Do not modify real secret values in local .env files unless explicitly instructed.

## Metrics and Observability

Prometheus/OpenTelemetry metric labels must remain low-cardinality.

Never use values such as:

- identity IDs
- session IDs
- challenge IDs
- provider message IDs
- phone numbers
- email addresses
- IP addresses
- user agents
- tokens

as metric labels.

Use bounded enums for labels whenever possible.

Add observability only for material production blind spots. Do not add metrics merely for completeness.

## Identity Service

The Identity Service authenticates all human identities, including riders, drivers, tenant users, staff, support, and platform users.

Business roles, permissions, onboarding state, rider profiles, driver profiles, and tenant authorization are not authoritative responsibilities of Identity.

Authentication success does not imply business readiness.

Sessions and refresh tokens are security state and must remain authoritative in persistence.

Current identity events include:

- identity.created
- identity.identifier_linked
- identity.identifier_unlinked
- identity.suspended
- identity.disabled
- identity.reactivated
- identity.session_revoked
- identity.sessions_revoked
- identity.refresh_token_reuse_detected

Do not invent new lifecycle or token events without a concrete requirement.

## Tenant Boundary

Tenant Core has not been implemented yet.

Do not invent trusted tenant behavior inside Identity before Tenant Service exists.

The following remain deferred until Tenant Core:

- trusted tenant_id
- tenant resolver/context
- tenant status validation
- Identity-to-Tenant internal contract
- tenant ID in access tokens or sessions
- tenant-aware authorization boundaries
- tenant branding
- tenant templates
- tenant communication channels
- tenant-specific OTP policies
- tenant rate limits
- tenant usage
- tenant billing
- tenant audit context

Do not workaround this boundary with temporary authoritative tenant logic.

## Gateway Boundary

The future API Gateway will be responsible for trusted external request boundaries.

Current Identity source IP protection uses the actual gRPC peer address.

When a Gateway is introduced, do not trust raw X-Forwarded-For or arbitrary client metadata.

Trusted client IP propagation must only be designed together with a trusted Gateway/service identity boundary.

## OTP Providers

Provider routing and failover are owned by this platform.

Current intended production routing:

SMS:
- primary: BulkSMSIraq
- fallback: Telnyx

WhatsApp:
- primary: BulkSMSIraq
- fallback: Meta

Email:
- Resend

Do not enable provider-native fallback that conflicts with platform-owned routing.

BulkSMSIraq delivery webhook integration is intentionally blocked until exact official webhook schema, authentication, and status semantics are available.

Do not guess undocumented provider payloads or authentication behavior.

## Infrastructure

Development infrastructure currently includes:

- PostgreSQL
- Valkey
- NATS JetStream
- Docker Desktop on Windows with WSL integration

The application source and Go tooling run inside WSL.

Do not move the project to /mnt/c or rewrite paths to Windows paths.

Production service ports must not be assumed publicly safe merely because they are reachable in development.

## Task Completion

At the end of every substantial task, report:

- what was inspected
- findings discovered
- changes made
- files changed
- tests added or changed
- exact test commands and results
- remaining issues
- intentionally deferred items and why

Do not claim success if tests are failing or required validation was not performed.