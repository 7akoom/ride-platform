Identity Service Completion Checklist

This original planning list is retained for reference. For current completion status, use the [Identity completion checklist](../services/identity-service/docs/completion-checklist.md) and [production validation report](../services/identity-service/docs/production-readiness-2026-09-05.md). Entries below are not a current list of unfinished implementation work.

This file tracks the remaining work for the Identity Service before it can be considered fully production-ready.

Work We Can Complete Before Tenant Core

Identity lifecycle: Suspend

Identity lifecycle: Disable

Identity lifecycle: Reactivate

Force revoke all sessions

Identity domain events

Outbox Pattern

Refresh Token replay/reuse protection review

Refresh Token concurrency hardening

Session revocation security review

Signing Key Rotation

Multiple verification public keys

Active signing kid

Grace period for old signing keys

Signing-key rotation tests

OTP brute-force security review

Identifier enumeration protection review

Rate-limit bypass review

OTP challenge replay review

Identifier Link race-condition review

Identifier Unlink race-condition review

Login/Refresh concurrency tests

Session revoke/refresh race tests

Log redaction review

Token/OTP/provider-secret leakage review

Authentication observability

OTP/provider metrics

Session/auth metrics

Provider message ID tracking

Provider delivery status tracking

Meta WhatsApp delivery webhooks

Telnyx SMS delivery webhooks

Provider delivery receipts

Provider health tracking

Advanced provider routing based on health/cost

Meta production account/configuration

Meta Authentication Templates

Telnyx production account/configuration

Resend production domain/configuration

BulkSMSIraq production credentials/configuration

Live SMS smoke tests

Live WhatsApp smoke tests

Live Email smoke tests

Migration fresh-database validation

Migration upgrade validation

Migration rollback validation

go test -race / concurrency testing

Load/stress testing for Identity Service

Final security audit

Final observability audit

Final production validation

Work Blocked Until Tenant Core Service Exists

tenant_hint → trusted tenant_id

Tenant Resolver

Trusted Tenant Context

Tenant status validation

Identity ↔ Tenant Service internal contract/gRPC integration

Trusted tenant_id inside Access Token

Trusted tenant_id inside session context

Tenant-aware authentication authorization boundaries

Tenant-specific OTP branding

Tenant-specific SMS branding

Tenant-specific WhatsApp branding/templates policy

Tenant-specific Email branding

Tenant-specific enabled auth channels

Tenant-specific authentication policies

Tenant-specific OTP policies

Tenant-aware rate limits based on trusted tenant context

Tenant-aware OTP usage accounting

Tenant-aware provider usage accounting

Tenant-aware billing attribution

Tenant-aware audit/event context

Final shared auth context containing trusted tenant_id

Final tenant-aware last_seen / session activity integration across services

Final tenant-boundary security tests

Completion Rule

Before moving permanently away from the Identity Service:

Complete all items in Work We Can Complete Before Tenant Core.

Build the Tenant Core Service.

Return to Identity Service and complete all items in Work Blocked Until Tenant Core Service Exists.

Run the final production validation s
