# Identity Service Final Production Validation — 2026-09-05

## Verdict and evidence boundary

Local production validation passed. Identity is ready for final source/documentation review and an explicitly authorized commit/push before Tenant Core. No unresolved code blocker was found within this validation's scope after the fixes below. This is not approval for an unconditional live production launch.

Production accounts, real destination delivery, public webhook ingress, infrastructure resilience, and capacity acceptance still require deployment validation. No real SMS, WhatsApp, or email was sent. Synthetic provider construction does not prove credentials or provider-side configuration work.

The completed security and observability audits were preserved. This review traced production construction and existing regression coverage; it did not redesign authentication, event semantics, routing, or tenant boundaries.

## Findings fixed

| Finding | Change | Evidence |
| --- | --- | --- |
| Independently valid signing and verification keys could be different, allowing startup to issue tokens its own verifier rejects. | Startup now requires the active verification key to match the signing private key, with consistent kid, issuer, and audience. | New token tests cover matching keys with an old rotation key retained, mismatched keys, missing active kid, mismatched issuer/audience, and unconfigured components. |
| The installed Go 1.26.5 toolchain had seven called standard-library vulnerability findings. | Workspace and Identity module now prefer Go 1.26.8. No dependency-library upgrade was made. | Standalone build, full tests, race tests, and govulncheck passed with Go 1.26.8. |
| Development PostgreSQL published port 5433 on all host interfaces, unlike Valkey/NATS. | Bind the existing port to `127.0.0.1:5433`; retain the developer port and data volume. | Compose configuration validation passed. The existing container was not recreated; this mapping takes effect on its next recreation. |

Go 1.26 remains within the Go support window at this review date. Patch selection follows the [official Go release history](https://go.dev/doc/devel/release). The `toolchain` directives are preferences, not a substitute for enforcing a patched compiler in CI and production builds; do not override them with an older `GOTOOLCHAIN=local` toolchain.

## Configuration and runtime review

| Area inspected | Result and production requirement |
| --- | --- |
| Environment | Runtime trims/lowercases the environment. Production selects real providers; development/test select DevelopmentDelivery. Missing/unknown environments fail construction. DevelopmentDelivery's constructor rejects environments other than development/test. Its intentional local OTP code/recipient logging remains unchanged. |
| Channel enablement | Production construction requires SMS and Resend email. WhatsApp can intentionally be disabled with no default/routes; it is not silently replaced by a successful noop sender. Enable WhatsApp for the intended deployment below. |
| Routing | Intended SMS: `bulksmsiraq` primary, `telnyx` fallback. Intended WhatsApp: `bulksmsiraq` primary, `meta` fallback. Email: `resend`. Defaults, fallbacks, and regional routes are validated across configuration parsing and provider/router construction. Configured default and fallback must differ. Fallback is optional in the general design, but must be configured for this deployment's stated intent. |
| Provider requirements | BulkSMSIraq requires base/OTP endpoints, API key, and sender. Telnyx requires endpoint, API key, and sender or messaging profile. Meta requires its Graph HTTPS endpoint, access token, and configured authentication templates/languages; production default/fallback validation requires English, Arabic, and Kurdish template configuration. Resend requires endpoint, API key, and From address. Constructors reject incomplete configuration; account validity remains a live check. |
| Webhooks | Telnyx verification key and Meta verify-token/app-secret enable their adapters. Meta credentials must be paired. Configured webhook verification requires a nonempty address; invalid verification material fails construction. Sending does not require receipt ingestion to be enabled. Configure both supported callbacks for production delivery-status visibility. |
| Provider timeout/health | HTTP timeout, circuit failure thresholds, and cooldowns must be positive. Defaults are 10 seconds, 3 failures, and 60 seconds. SMS and WhatsApp health remain isolated; platform failover semantics are unchanged. |
| OTP and token durations | OTP, access token, session, refresh, and cleanup duration parsing rejects nonpositive values. Refresh TTL cannot exceed session TTL. Issued access-token expiry is capped by session expiry. Set explicit approved production values. |
| Signing configuration | Ed25519 private/public material must parse, issuer/audience/kid must exist, and the configured verification keyring must contain the active kid. The new consistency check runs before serving requests. Keep retiring public keys until their tokens expire. Mount private keys read-only with restricted access. |
| OTP secret | HMAC secret must contain at least 32 bytes. Provision a strong production secret through secret management; never use example/test values. |
| PostgreSQL/Valkey | Connection initialization and ping errors stop startup. PostgreSQL is authoritative for sessions. Valkey revocation/cache failures fail closed; a cache miss never overrides PostgreSQL state. Explicit production connection settings are required instead of local defaults. |
| NATS/outbox | Invalid configuration stops startup. Temporary NATS unavailability intentionally permits reconnect plus durable outbox accumulation. Healthy gRPC alone therefore does not prove event publishing is working. See operational requirements below. |
| Metrics/webhook construction | Exporter, metrics registration, webhook construction, and listener failures are handled as startup/runtime failures. Production metrics and delivery tracking are wired to real implementations. |
| Background lifecycle | Cleanup and outbox errors are logged; work uses cancellation and completion channels. Shutdown stops HTTP/gRPC listeners, joins background work, and drains NATS before closing dependencies. HTTP/gRPC graceful shutdown has a 10-second deadline; gRPC and webhook have forced-stop paths. Allow additional process termination grace for worker completion and NATS drain. |

New construction tests use synthetic configuration and a transport that rejects outbound requests. They cover the intended routing, intentional WhatsApp disablement, and 18 incomplete-configuration cases. They do not load real local provider credentials.

## Existing security and OTP behavior checked for regressions

- Ed25519 verification, active kid/keyring rotation, expiry checks, session expiry cap, authoritative PostgreSQL session state, Valkey fail-closed behavior, refresh rotation, reuse detection, and revocation remain intact.
- Cryptographic OTP generation, challenge-bound HMAC hashing, expiry, attempt limits, atomic consumption, replay resistance, recipient/request and source-IP limiting remain intact. Source IP comes from the actual gRPC peer; no forwarded-header trust was introduced.
- Platform routing and conservative failover remain authoritative. Open-circuit skips are observable without counting an uncalled provider as a delivery attempt. Unknown delivery state does not cause an unsafe immediate retry through another provider.
- SMS/WhatsApp provider message IDs, delivery attempt tracking, signed Telnyx/Meta receipt handling, receipt persistence/idempotency, and out-of-order protection remain implemented. Email currently has send-result observability; no new email receipt adapter is claimed.
- Production provider implementations do not log OTP codes/recipients or raw webhook bodies. Existing token, credential, Authorization-header, cookie, and key redaction is preserved. DevelopmentDelivery's intentionally useful local logs remain restricted to development/test.

## Transport and infrastructure deployment requirements

| Surface | Bind/transport | Required exposure |
| --- | --- | --- |
| gRPC | Default `:50051`, plaintext | Trusted private/internal callers only. Use the deployment's TLS/service identity boundary where required. It is not a publicly safe API merely because it has authentication interceptors. |
| Metrics | Default `:9090`, plaintext `/metrics` | Private scraper access only; no public ingress. |
| OTP webhook | Default `:8081`, plaintext backend, enabled when adapters are configured | Public HTTPS ingress must forward Telnyx POST `/webhooks/otp/telnyx` and Meta GET/POST `/webhooks/otp/meta`. Preserve signature-relevant request bytes and headers. Terminate TLS at the approved ingress boundary. |
| PostgreSQL | Development host `127.0.0.1:5433` after container recreation | Production database stays private with restricted credentials, encrypted connections, backups, and tested recovery. Use a suitable PostgreSQL TLS DSN. |
| Valkey | Development host `127.0.0.1:6380` | Private access with authentication and appropriate network encryption/isolation. The current client construction does not configure TLS itself; provide the approved protected transport/network boundary. |
| NATS | Development client/monitoring ports are loopback-bound | Production client, management, and monitoring access must be private and restricted. Configure authentication, protected transport, durable storage, and the deployment's availability policy. |

Metrics and webhook HTTP servers have 5-second header, 10-second read/write, and 60-second idle timeouts. Webhook header limit is explicitly 1 MiB; metrics uses Go's default 1 MiB. No service-local TLS architecture was introduced.

The development compose file is not a production deployment manifest. Its single-instance dependencies, development credentials/configuration, and JetStream replication settings are not production availability guarantees. The localhost PostgreSQL binding keeps the existing WSL developer port workflow; validate any separately required remote developer access through an explicit tunnel rather than reopening it globally.

Production packaging must include the relative generated-code module used by `replace ... => ../../gen/go`, or an equivalent established build context. Build with Go 1.26.8 or a newer approved patched toolchain; race testing requires the supported native compiler/CGO environment. Do not bake local `.env` files or private keys into the image. Use HTTPS provider endpoints and production secret mounts. Deployment health checks must account for dependency health, outbox progress, and listener availability; the gRPC health response alone is insufficient.

## Database and migration evidence

- Repository migrations remain versions 1–10. Their contents are unchanged from this production-validation task's baseline; no new migration requires testing.
- The existing local PostgreSQL container was initially stopped, temporarily started for a read-only inspection, and stopped afterward. Its Goose history had applied versions 0 through 10, and `public.otp_delivery_attempts` existed.
- The user's previously completed fresh 0→10, rollback 10→0, reapply 0→10, and realistic populated 7→10 upgrade validation remains applicable. Legacy-data preservation and intended nullable columns are historical evidence, not tests rerun here.
- No destructive migration test, application-row modification, or migration change was performed. Production must run the existing Goose migrations externally before starting the service; startup does not apply them automatically.
- The required default full/race suites exclude tests guarded by `//go:build integration`. No fresh database/Valkey/JetStream integration-suite result is claimed for this task; persistence code and migrations were not changed.

## Outbox and NATS readiness

The transactional outbox is wired to PostgreSQL and an acknowledged JetStream publisher. Claims use leases and `SKIP LOCKED`; publish failures are recorded for retry. Defaults are a batch of 10, a 2-second publish timeout, a 30-second lease, 500-millisecond polling, and retry delay from 1 second to 1 minute. Configuration enforces `batch × publish timeout < lease`.

Connection retry on initial failure and unlimited reconnect are intentional. Reconnect buffering is disabled; the durable outbox remains the retry source. Existing worker/publisher logs, including retry-scheduled counts, and the pending-count/oldest-pending-age gauges support detection of sustained failures and stuck processing. There is no separate retry-state metric. Configure alerts and verify scraping/log collection in the deployment; their existence in code does not install operational alerts.

Provision `IDENTITY_EVENTS` before expecting successful publishes, with `identity.>` subject coverage and publish/ack permissions. The development bootstrap specifies file storage, 7-day retention, 1 GiB maximum storage, 1 MiB messages, a 10-minute duplicate window, and one replica. Select production capacity/replication and recovery procedures deliberately. A missing stream or disconnected broker can leave authentication serving while the outbox grows; test recovery and event delivery in staging without inventing new event semantics.

## Pending live smoke tests — explicit authorization required

| Provider/channel | Inputs needed, without disclosing them in reports | Remaining check | Cost |
| --- | --- | --- | --- |
| BulkSMSIraq SMS and WhatsApp | Production base/OTP endpoints, API key, approved sender, enabled account/channel, funded balance, approved E.164 destination | Send one explicitly authorized OTP per channel; confirm receipt, sign-in verification, and persisted provider ID/attempt | May incur message charges/quota |
| Telnyx SMS fallback | API key, sender or messaging profile, permitted destination, account permissions/balance; public key and HTTPS callback for receipts | Authorized fallback delivery plus signed callback persistence; controlled failover exercise | May incur SMS charges/quota |
| Meta WhatsApp fallback | Access token, phone-number ID/API version in Graph endpoint, approved authentication templates/languages, eligible opted-in destination; verify token/app secret and HTTPS callback | Authorized delivery for required locales, handshake/signature verification, and delivery status persistence | May incur provider charges/quota |
| Resend email | API key, verified sending domain/From, permitted destination mailbox | Authorized delivery and complete OTP verification; check sender/domain restrictions | Consumes quota and may incur cost |

Also validate refresh/logout after an authorized live sign-in, supported duplicate/out-of-order webhook handling, metrics/log delivery, and controlled provider outage recovery. Use a provider sandbox or an agreed failure mechanism; do not manufacture ambiguous production sends to test fallback. Provider-native fallback must remain disabled where it conflicts with platform routing.

Capacity/load testing, production ingress/TLS, account permissions, real credentials, provider-side templates/domains, actual webhook reachability, database recovery, and broker resilience are pending launch gates. Traffic/SLO targets and production infrastructure were not supplied; no load/stress or live outage result is claimed.

## Exact validation results

Commands below ran from `services/identity-service` unless marked repository root. The final build, focused/full/race tests, and vulnerability scan used Go 1.26.8; module verification was completed before the toolchain change.

| Command | Result |
| --- | --- |
| `gofmt -w cmd/identity-service/main.go cmd/identity-service/production_readiness_test.go internal/infrastructure/token/signing_configuration.go internal/infrastructure/token/signing_configuration_test.go` | PASS; four Go files formatted. |
| `gofmt -l cmd/identity-service/main.go cmd/identity-service/production_readiness_test.go internal/infrastructure/token/signing_configuration.go internal/infrastructure/token/signing_configuration_test.go` | PASS, exit 0; no output. |
| `go test -count=1 ./internal/infrastructure/token ./cmd/identity-service ./internal/config` | PASS, exit 0; all three packages. |
| `go test -count=1 ./...` | PASS, exit 0; 14 packages passed, 2 had no test files. Confirmed rerun after interruption. |
| `go test -race -count=1 ./...` | PASS, exit 0; 14 packages passed, 2 had no test files; no race reports. |
| `GOWORK=off go build -o /tmp/identity-service-production ./cmd/identity-service` | PASS, exit 0; standalone module build. |
| `go version /tmp/identity-service-production` | PASS; binary reports Go 1.26.8. |
| `go mod verify` | PASS; all modules verified. |
| `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` | Initial Go 1.26.5 scan FAILED: seven called standard-library vulnerabilities; resolved tool version was v1.7.0. |
| `go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 -show verbose ./...` | PASS, exit 0 on Go 1.26.8; zero called or imported-package vulnerabilities. Four module-only advisories remain in unimported `golang.org/x/crypto` SSH/OpenPGP packages. |
| `docker compose -f infrastructure/compose/compose.yaml config --quiet` (root) | PASS, exit 0; no resolved secrets printed. |
| `git diff --check` (root) | PASS, exit 0. |

Initial sandbox attempts at scanner downloads/toolchain cache access and focused tests encountered network/cache restrictions. Approved reruns succeeded. An earlier full-test invocation was interrupted without a recoverable exit status; it is not counted as passing. The confirmed full and race results above supersede that unknown result. No protections were disabled.

The initial called advisories were GO-2026-6218, GO-2026-6091, GO-2026-6090, GO-2026-6089, GO-2026-6088, GO-2026-5972, and GO-2026-5026. The remaining module-only notices are GO-2026-6355, GO-2026-6354, GO-2026-6303, and GO-2026-5932. They were not suppressed; none of their affected packages is imported by this service according to the final scan. Track dependency maintenance and rerun [govulncheck](https://go.dev/doc/security/vuln/) when dependencies or imports change.

Read-only database verification used the existing container, without printing its credentials:

```sh
docker ps -a --format '{{.Names}} {{.Status}}' --filter name=ride-identity-postgres
docker start ride-identity-postgres
docker exec ride-identity-postgres sh -c 'exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -Atc "BEGIN READ ONLY; SELECT version_id,is_applied FROM goose_db_version ORDER BY id; SELECT to_regclass('\''public.otp_delivery_attempts'\'') IS NOT NULL; COMMIT;"'
docker stop ride-identity-postgres
```

All completed successfully; the query returned applied versions 0–10 and table existence `true`. The container's original stopped state was restored.

## Changed files in this task

Pre-existing security/observability work is excluded from this inventory.

- `go.work`: preferred patched workspace toolchain.
- `services/identity-service/go.mod`: preferred patched standalone toolchain.
- `infrastructure/compose/compose.yaml`: PostgreSQL loopback host binding.
- `services/identity-service/cmd/identity-service/main.go`: signing consistency fail-fast wiring.
- `services/identity-service/cmd/identity-service/production_readiness_test.go`: new non-sending provider construction/configuration tests.
- `services/identity-service/internal/infrastructure/token/signing_configuration.go`: signer/verifier consistency validation.
- `services/identity-service/internal/infrastructure/token/signing_configuration_test.go`: new consistency tests.
- `services/identity-service/docs/production-readiness-2026-09-05.md`: this report.
- `services/identity-service/docs/completion-checklist.md`: align completed local work with pending live/deferred work.

## Intentional deferrals and safety

- BulkSMSIraq delivery webhook adapter remains blocked on exact official payload, authentication, and status semantics. No schema or authentication was guessed. BulkSMSIraq send acceptance is not proof of final delivery status.
- Tenant Core remains a separate dependency: trusted tenant resolution/context/status, Identity↔Tenant contract, tenant IDs in tokens/sessions, tenant authorization, branding/templates/channels/policies, tenant limits/usage/billing/audit all remain deferred.
- Cost-aware routing is outside this scope. Trusted Gateway client-IP propagation awaits a Gateway/service identity boundary.
- No API contract, event semantics, migration, unrelated service, production secret, or intentional DevelopmentDelivery logging behavior was changed.
- No commit, push, merge, rebase, reset, history rewrite, or stash operation was performed. Existing changes and the baseline stash were preserved.
