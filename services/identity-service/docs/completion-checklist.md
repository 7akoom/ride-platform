# Identity Service Completion Checklist

This document tracks the remaining work required before the Identity Service can be considered fully production-ready.

## Current Status

Identity Service core authentication, session security, lifecycle management, OTP security, observability, delivery tracking, SMS/WhatsApp provider failover, and Telnyx/Meta webhooks are implemented and locally validated.

Final security and observability audits are complete. [Final production validation](production-readiness-2026-09-05.md) passed on Go 1.26.8. Identity is ready for final source review and an authorized commit before Tenant Core; live launch still requires provider smoke tests, production infrastructure validation, and load/capacity acceptance. BulkSMSIraq webhooks remain blocked on documentation. Checked implementation items below do not imply live provider validation.

---

## Work We Can Complete Before Tenant Core

### Identity Lifecycle

- [x] Identity lifecycle: Suspend
- [x] Identity lifecycle: Disable
- [x] Identity lifecycle: Reactivate
- [x] Force revoke all sessions

### Events and Reliability

- [x] Identity domain events
- [x] Transactional Outbox Pattern

### Refresh Tokens and Sessions

- [x] Refresh Token replay/reuse protection review
- [x] Refresh Token concurrency hardening
- [x] Session revocation security review
- [x] Login/Refresh concurrency tests
- [x] Session revoke/refresh race tests

### Signing Keys

- [x] Signing Key Rotation
- [x] Multiple verification public keys
- [x] Active signing kid
- [x] Grace period for old signing keys
- [x] Signing-key rotation tests
- [x] Startup signer/active verification-key consistency validation

### OTP Security

- [x] OTP brute-force security review
- [x] Identifier enumeration protection review
- [x] Rate-limit bypass review
- [x] OTP challenge replay review

### Identifier Security

- [x] Identifier Link race-condition review
- [x] Identifier Unlink race-condition review

### Logging and Secret Safety

- [x] Log redaction review
- [x] Token/OTP/provider-secret leakage review

### Observability

- [x] Authentication observability
- [x] OTP/provider metrics
- [x] Session/auth metrics

### Provider Delivery Tracking

- [x] Provider message ID tracking
- [x] Provider delivery status tracking
- [x] Provider delivery attempts persistence
- [x] Provider delivery receipts
- [x] Receipt idempotency
- [x] Out-of-order receipt protection
- [x] Terminal delivery-state protection

### Provider Webhook Infrastructure

- [x] Provider-neutral HTTP webhook server
- [x] Provider-neutral webhook router
- [x] Provider-neutral delivery receipt handler
- [ ] BulkSMSIraq delivery webhook/status adapter
- [x] Meta WhatsApp delivery webhooks (implementation and local tests)
- [x] Telnyx SMS delivery webhooks (implementation and local tests)

BulkSMSIraq's exact official webhook payload, authentication, and status semantics remain insufficiently documented. Do not implement this adapter from assumptions. Live Telnyx/Meta callback verification remains in the live validation checklist.

### Provider Routing and Health

- [x] Conservative provider failover policy
- [x] SMS fallback provider support
- [x] SMS BulkSMSIraq primary routing
- [x] SMS Telnyx fallback routing
- [x] SMS provider health tracking
- [x] SMS circuit breaker
- [x] SMS health-aware pre-send bypass
- [x] SMS provider health runtime configuration
- [x] WhatsApp provider failover
- [x] WhatsApp provider health tracking
- [x] WhatsApp circuit breaker
- [x] WhatsApp BulkSMSIraq -> Meta health-aware routing
- [ ] Cost-aware provider routing (out of scope; not a pre-Tenant commit prerequisite)

### Provider Routing Safety Rules

Current SMS routing follows these rules:

- `rate_limited` may trigger same-request fallback.
- `permanent` does not trigger fallback.
- `temporary` does not trigger same-request fallback.
- `unknown_delivery_state` never triggers same-request fallback.
- `rate_limited`, `temporary`, and `unknown_delivery_state` may affect provider health.
- `permanent` failures do not affect provider health.
- An open circuit allows a provider to be bypassed before an outbound request is made.
- Pre-send circuit bypass may safely use the fallback provider.
- SMS and WhatsApp provider health are isolated by channel.

Detailed routing behavior should also be documented in `provider-routing.md`.

### Production Provider Configuration

- [x] Local production configuration/construction validation with synthetic credentials and no sends
- [ ] Meta production account/configuration
- [ ] Meta Authentication Templates
- [ ] Telnyx production account/configuration
- [ ] Resend production domain/configuration
- [ ] BulkSMSIraq production credentials/configuration

Configuration being present locally is not sufficient for completion. Production credentials and provider-side configuration must be validated.

### Live Provider Validation

- [ ] Live SMS smoke tests
- [ ] Live WhatsApp smoke tests
- [ ] Live Email smoke tests
- [ ] Delivery-status smoke tests where supported

### Database Validation

- [x] Migration fresh-database validation (previously completed 0→10 and reapply)
- [x] Migration upgrade validation (previously completed populated 7→10)
- [x] Migration rollback validation (previously completed 10→0)
- [x] Current migrations unchanged; local Goose version 10 confirmed read-only

Destructive migration validation was not repeated. The database container was returned to its original stopped state.

### Final Engineering Validation

- [x] `go test -count=1 ./...`
- [x] `go test -race -count=1 ./...`
- [x] Concurrency validation under race detector for the default suite
- [ ] Load/stress testing for Identity Service
- [x] Final security audit
- [x] Final observability audit
- [x] Final production validation (local pre-Tenant scope; live launch gates remain)
- [x] Standalone production binary build and called-vulnerability scan on Go 1.26.8
- [ ] Production ingress/TLS, private dependency exposure, backups/recovery, and alert delivery validation

The default full/race commands exclude tests behind the `integration` build tag; this task does not claim a new database/Valkey/JetStream integration-suite run. See the production validation report for exact commands, prior migration evidence, scan limitations, and deployment requirements.

---

## Work Blocked Until Tenant Core Service Exists

### Trusted Tenant Context

- [ ] `tenant_hint` -> trusted `tenant_id`
- [ ] Tenant Resolver
- [ ] Trusted Tenant Context
- [ ] Tenant status validation
- [ ] Identity <-> Tenant Service internal contract/gRPC integration
- [ ] Trusted `tenant_id` inside Access Token
- [ ] Trusted `tenant_id` inside session context
- [ ] Tenant-aware authentication authorization boundaries

### Tenant Authentication Configuration

- [ ] Tenant-specific OTP branding
- [ ] Tenant-specific SMS branding
- [ ] Tenant-specific WhatsApp branding/templates policy
- [ ] Tenant-specific Email branding
- [ ] Tenant-specific enabled auth channels
- [ ] Tenant-specific authentication policies
- [ ] Tenant-specific OTP policies

### Tenant-Aware Limits, Usage, and Billing

- [ ] Tenant-aware rate limits based on trusted tenant context
- [ ] Tenant-aware OTP usage accounting
- [ ] Tenant-aware provider usage accounting
- [ ] Tenant-aware billing attribution

### Tenant-Aware Audit and Session Context

- [ ] Tenant-aware audit/event context
- [ ] Final shared auth context containing trusted `tenant_id`
- [ ] Final tenant-aware `last_seen` / session activity integration across services
- [ ] Final tenant-boundary security tests

---

## Completion Rule

Before live production launch and completion of the full platform authentication boundary:

1. Complete applicable live provider, infrastructure, and capacity gates. Keep explicitly blocked/out-of-scope items separate from completed local engineering work.
2. Record real deployment validation results. A pre-Tenant source commit may proceed after review without claiming these live gates are complete.
3. Build the Tenant Core Service.
4. Return to Identity Service.
5. Complete all items in **Work Blocked Until Tenant Core Service Exists**.
6. Run final tenant-boundary security validation.
7. Run final production validation for the complete Identity + Tenant authentication flow.
