# Identity Service Provider Routing

This document defines OTP provider routing, failover, delivery tracking, and provider health behavior for the Identity Service.

## Goals

Provider routing must:

- Avoid duplicate OTP delivery.
- Preserve provider delivery traceability.
- Support provider fallback when it is safe.
- Avoid unhealthy providers before sending new requests.
- Keep SMS and WhatsApp health state isolated.
- Remain configurable and provider-neutral.
- Avoid depending on undocumented provider webhook contracts.

---

## Current Providers

### SMS

Primary provider:

- BulkSMSIraq

Fallback provider:

- Telnyx

### WhatsApp

Primary provider:

- BulkSMSIraq

Fallback provider (implemented and locally tested; live validation pending):

- Meta WhatsApp

### Email

Current provider:

- Resend

Email routing and health failover are not currently implemented.

---

## Delivery Attempts

Every tracked provider send creates an independent delivery attempt.

A delivery attempt records:

- Challenge ID
- Channel
- Provider
- Provider message ID when available
- Current delivery status
- Provider-native status
- Failure code
- Attempt timestamp
- Accepted timestamp
- Sent timestamp
- Delivered timestamp
- Failed timestamp

Multiple attempts may belong to the same OTP challenge.

Example:

```text
OTP Challenge
    |
    +-- Attempt 1
    |      Provider: BulkSMSIraq
    |      Status: failed
    |      Failure: rate_limited
    |
    +-- Attempt 2
           Provider: Telnyx
           Status: accepted
```

See [the completion checklist](completion-checklist.md#provider-routing-safety-rules) for current failover rules and [production readiness](production-readiness-2026-09-05.md) for pending live validation and deployment requirements. BulkSMSIraq delivery webhooks remain blocked on official contract documentation.
