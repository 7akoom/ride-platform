# Final Identity Observability Audit — 2026-09-05

Status: implementation complete; focused tests, required full suite, required race suite, and the new focused PostgreSQL integration test pass. Initial environmental failures and their resolution are recorded below.

## Inspection and findings

Read root `AGENTS.md`, repository status and diff, then the current Identity implementation and surrounding tests. Reviewed SMS/WhatsApp routers, optional recorder runtime wiring, provider health tracker and failover policy, OpenTelemetry instruments/recorders/exporter, Telnyx and Meta receipt decoders and verification, generic webhook routing/handlers, PostgreSQL `ApplyReceipt`, outbox processor/worker/stores/NATS publisher, cleanup runner, runtime startup/shutdown, and structured logging/redaction.

| Area | Finding and disposition |
| --- | --- |
| OBS-10 circuit skips | Both routers and both runtime wiring paths were already implemented, including the safe WhatsApp type assertion. Preserved them and WhatsApp's nil delivery-recorder constructor semantics. Added coverage for exactly one primary skip, fallback skips, both circuits open, and an open fallback that is never reached. Skipped providers produce no delivery-attempt metric. |
| OBS-10 initialization | Creating the provider-health counter overwrote the delivery-duration histogram initialization error. Fixed error-check ordering and added a regression test. |
| Webhook processing | HTTP responses alone were the only signals for accepted, ignored, unauthorized, malformed, or persistence-failed processing. Added one bounded request-outcome counter at the generic handler, wired to both production adapters. Existing HTTP status codes, signature checks, batch processing, and receipt persistence semantics remain unchanged. |
| Transactional outbox | Worker error logs already include claimed/published/retry-scheduled/lost-claim counts. NATS logs connection, reconnect, asynchronous, and closure failures; publishing has a timeout. Sustained backlog or stuck processing lacked a live backlog signal. Added two database-backed gauges for pending count and oldest pending age, independent of worker progress. |
| Cleanup/background lifecycle | Cleanup logs successful completion with deletion counts and non-cancellation failures. Outbox cycle/runtime failures are logged. Existing completion/error logs and outbox backlog signals cover the material operational needs; no extra cleanup or worker metrics added. |
| Metrics HTTP endpoint | Configured bind address is preserved (default `:9090`, all interfaces). Read-header/read/write/idle timeouts are 5s/10s/10s/60s. Zero `MaxHeaderBytes` uses Go's bounded 1 MiB default, confirmed in the installed standard library. Runtime serve failures are logged and trigger shutdown; metrics shutdown participates in the 10s graceful shutdown context. No endpoint or TLS change needed. |
| Structured logging | Preserved the intentional development/test adapter's OTP code and recipient logging after reviewing its environment restrictions. The constructor rejects every other environment; startup selects production providers for `APP_ENV=production` and rejects missing/unknown environments. Production provider implementations contain no logging calls. Existing production secret-redaction rules remain unchanged; `otp_code` and `otp_identifier` retain their intentional development logging exception. New webhook metrics contain no request data; outbox collection failures log a fixed message without raw database errors. |

## Metric semantics and operations

- `identity.otp.provider_health.events`: existing OBS-10 counter; `channel`, `provider`, `event`. `circuit_open` counts actual routing skips, not circuit transitions or provider calls.
- `identity.otp.delivery.webhooks`: new counter; `provider` is `telnyx` or `meta`; `outcome` is `accepted`, `ignored`, `unauthorized`, `invalid`, or `persistence_failed`. Unsupported label values are rejected by the recorder.
- Webhook counts are per POST request, including Meta batches. `accepted` means every decoded receipt returned successfully from `ApplyReceipt`; it includes duplicate, terminal-state, or stale receipts that persistence intentionally ignores. `ignored` means the decoder ignored the request or returned no receipts. A partially applied batch with a later failure counts once as `persistence_failed`; previously successful receipt updates retain existing behavior. Missing receipt correlation also surfaces as `persistence_failed` through the existing store error. These are processing outcomes, not delivery-success counts.
- `identity.outbox.pending`: new gauge without labels; includes every unpublished event, including active leases and delayed retries.
- `identity.outbox.oldest_pending.age`: new gauge in seconds without labels; zero for an empty backlog. Pending count and oldest age come from a single read-only aggregate query, using the existing unpublished-event index where PostgreSQL's planner chooses it. No schema migration.
- Outbox collection runs on scrape with a maximum 3s query context. Failed collection logs `outbox metrics collection failed` and omits both samples, rather than emitting false zeros or retaining stale values. Other metrics remain available.
- Operators should monitor sustained oldest age/pending count, missing outbox samples, scrape health, webhook `persistence_failed`/`unauthorized` rates, circuit skips, and the existing outbox/cleanup error logs. Thresholds, dashboards, scrape configuration, and log-alert rules belong to deployment configuration and were not invented here.
- Every instance observes the shared database backlog. Aggregate replica gauges using `max`, not `sum`, to avoid counting the same backlog repeatedly.
- Metrics access must be restricted by deployment networking. Public reachability is not a safe production default; no in-service TLS or new architectural boundary was introduced.

## Tests and validation

All commands below ran from `services/identity-service`. Every changed Go file was formatted with `gofmt`; final `gofmt -l` returned no files. `git diff --check` passed.

| Exact test command | Result |
| --- | --- |
| `go test -count=1 ./internal/infrastructure/otp ./internal/observability ./cmd/identity-service` | PASS: 3 packages. |
| `go test -count=1 ./internal/transport/http ./internal/observability ./cmd/identity-service` | PASS: 3 packages. |
| `go test -count=1 ./internal/observability ./internal/transport/http ./cmd/identity-service ./internal/application/outbox ./internal/infrastructure/cleanup` | PASS: 5 packages. |
| `go test -count=1 ./internal/infrastructure/otp ./internal/observability` | PASS: 2 packages. |
| `go test -count=1 ./internal/infrastructure/otp ./internal/observability ./internal/transport/http ./cmd/identity-service` | PASS: 4 packages. |
| `go test -count=1 ./...` | Initial FAIL: sandbox denied the gRPC lifecycle test's loopback listener (`operation not permitted`). Approved rerun PASS: 14 tested packages, 2 packages without tests. |
| `go test -race -count=1 ./...` | Initial FAIL: same sandbox listener restriction. Approved rerun PASS: 14 tested packages, 2 packages without tests; no race reports. |
| `go test -tags=integration -count=1 ./internal/infrastructure/persistence/postgres -run '^TestOutboxPendingStats$'` | Initial setup FAIL: sandbox read-only Go cache. Two approved attempts then FAIL because configured PostgreSQL refused connections (first generic error, then confirmed connection refusal). Final approved run PASS after starting the existing stopped PostgreSQL container. |

The integration command was launched through Python with only `DATABASE_URL` parsed from local `.env` into the child environment; no credentials were printed or changed. The test uses a connection-local temporary table with a single-connection pool, leaving persistent application tables untouched. The existing `ride-identity-postgres` container was started with approval and stopped afterward, restoring its original state. The broad pre-existing integration-tag suite was not run; the required full commands do not select that build tag.

Added/updated behavior coverage: router primary/fallback/both/unused skips and attempted-delivery counts; WhatsApp runtime compatibility with both recorder interface variants; exported provider-health labels; histogram initialization failure propagation; Telnyx/Meta generic handler outcomes; empty, accepted, and partially failed Meta batches; webhook label bounds; outbox gauges across backlog/drain/failure/recovery and query deadline; actual PostgreSQL backlog filtering/age/cancellation; development/test log availability, rejection outside those environments, and preservation of existing secret redaction.

## Files changed by this task

This inventory is relative to the service directory and compares against the working files at task start, including previously untracked work. It is not the repository's larger pre-existing Git diff.

- `cmd/identity-service/main.go` — modified.
- `cmd/identity-service/otp_delivery_metrics_test.go` — added.
- `cmd/identity-service/otp_webhook.go` — modified.
- `internal/infrastructure/otp/development_delivery_logging_test.go` — added.
- `internal/infrastructure/otp/provider_health_metrics_test.go` — added.
- `internal/infrastructure/otp/sms_router_test.go` — modified.
- `internal/infrastructure/otp/whatsapp_router_test.go` — modified.
- `internal/infrastructure/persistence/postgres/outbox_store_metrics.go` — added.
- `internal/infrastructure/persistence/postgres/outbox_store_metrics_integration_test.go` — added.
- `internal/observability/auth_metrics.go` — modified.
- `internal/observability/outbox_metrics.go` — added.
- `internal/observability/outbox_metrics_test.go` — added.
- `internal/observability/provider_health_metrics_test.go` — added.
- `internal/observability/webhook_metrics.go` — added.
- `internal/observability/webhook_metrics_test.go` — added.
- `internal/transport/http/webhook.go` — modified.
- `internal/transport/http/webhook_metrics.go` — added.
- `internal/transport/http/webhook_metrics_test.go` — added.
- `docs/final-observability-audit-2026-09-05.md` — added audit report.

## Development logging correction

The initial audit incorrectly removed an intentional development/testing feature. On review, restored the original `otp_code` and `otp_identifier` log attributes and removed only the two global redaction entries introduced by that audit. The adapter and logger implementation/redaction tests now match their pre-audit working versions; all unrelated observability changes remain intact.

Verified `NewDevelopmentDelivery` accepts exactly `development` and `test`; runtime normalizes `APP_ENV`, chooses this adapter only for those values, uses production provider construction for `production`, and rejects every other value. Configuration does not default a missing `APP_ENV` to development. SMS/WhatsApp production validation and provider factories do not accept the development adapter; the email factory accepts Resend only. These guarantees are based on the configured environment; no deployment can infer its physical environment independently of `APP_ENV`.

Updated logging coverage verifies both allowed environments expose the two fields for manual OTP testing and production/staging/empty/unknown environments reject construction without logging. Restored the original logger tests preserving the intentional OTP fields; token, API-key, authorization, password, cookie, and private-key redaction tests remain intact. Production providers were inspected and left unchanged.

Correction files:

- `internal/infrastructure/otp/development_delivery.go`
- `internal/infrastructure/otp/development_delivery_logging_test.go`
- `internal/observability/logger.go`
- `internal/observability/logger_test.go`
- `docs/final-observability-audit-2026-09-05.md`

Correction validation (run from `services/identity-service`):

- `gofmt -w internal/infrastructure/otp/development_delivery.go internal/infrastructure/otp/development_delivery_logging_test.go internal/observability/logger.go internal/observability/logger_test.go` — completed; subsequent `gofmt -l` returned no files.
- `go test -count=1 ./internal/infrastructure/otp ./internal/observability ./cmd/identity-service` — PASS, all 3 packages.
- `go test -count=1 ./...` — PASS, all 14 tested packages; 2 packages without tests. Ran with approved local socket access for the gRPC lifecycle test.
- `git diff --check` — PASS.

Only the five correction files listed above changed; no files were deleted, no unrelated observability changes were modified, and no Git history or stash operation occurred. Prior audit results above describe the original audit runs; the race and integration suites were not rerun for this logging-only correction.

## Remaining scope and deferrals

No unresolved material code observability gap was identified in the audited paths after these changes. Production alert rules, dashboards, private metrics access, and scrape intervals still require deployment configuration. The backlog aggregate has bounded execution time but was not load-tested against a production-sized backlog; monitor collection failures and tune operational scrape frequency to deployment scale.

Do not interpret accepted webhook requests as changed database rows or successful end-user delivery. Separate duplicate/stale receipt counters and Meta's one-time GET verification counters were not added because they do not justify extra instrumentation or changes to `ApplyReceipt`'s contract. Individual delivery state remains in persistence.

BulkSMSIraq webhooks remain blocked on exact official schema, authentication, and status documentation. Tenant observability remains deferred until Tenant Core exists. Cost-aware routing, new lifecycle events, API changes, schema changes, and authentication redesign remain outside this task.

No unrelated service was changed and no pre-existing file was deleted by this task. Source hashes and a task-only diff were checked against the starting state. No real secret values were changed or printed. No commit, push, merge, rebase, reset, restore, checkout, or history-rewriting operation was performed. The baseline stash was not accessed or modified.
