# Rider Service

Owns the rider domain: rider profiles linked to an `identity_id` from
`identity-service`. Rider is deliberately its own bounded context — no
tenant, driver, or trip data lives here.

## What's implemented (v1 slice)

- `CreateRider`, `GetRider`, `GetRiderByIdentity`, `UpdateRiderProfile` over gRPC
- Postgres persistence with a transactional outbox (`rider.created` event
  written in the same transaction as the rider row, so Dispatch/Trip can
  later subscribe without ever missing an event)
- Clean layering matching `identity-service`: `application/rider` (domain +
  use cases), `infrastructure/persistence/postgres` (repository),
  `transport/grpc` (handler + server), `config`

## What's intentionally NOT done yet

Matching `identity-service`'s maturity level is a follow-up, not a v1
requirement:

- **NATS JetStream outbox publisher + worker** — the `outbox.Store` /
  `outbox.Publisher` ports match identity-service's contracts exactly, so
  `internal/application/outbox/{processor,worker}.go` and
  `internal/infrastructure/messaging/nats/jetstream_publisher.go` can be
  copied over with only import-path changes once Dispatch/Trip are ready to
  consume `rider.created`.
- **Observability** (Prometheus metrics, structured request logging) — only
  a bare JSON logger is wired in `main.go` right now.
- **Authentication interceptor** — requests aren't yet verified against
  identity-service's access tokens. Copy
  `identity-service/internal/transport/grpc/authentication_interceptor.go`
  once the token verification key material is shared across services.
- **Tests** — no unit or integration tests yet. `identity-service`'s
  `service_*_test.go` / `*_integration_test.go` pattern is the template to
  follow.

## Running locally

```bash
cd services/rider-service
cp .env.example .env   # adjust DATABASE_URL
go mod tidy
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/rider-service
```

## Regenerating proto code

The service depends on generated code from `proto/ride/rider/v1/rider.proto`.
After editing the proto, regenerate from the repo root:

```bash
buf generate
```

This requires `protoc-gen-go` and `protoc-gen-go-grpc` on your `PATH` (see
`buf.gen.yaml`). The generated package `gen/go/ride/rider/v1` is what
`rider_handler.go` imports as `riderv1` — it does not exist yet in this
delivery and must be generated before the service will compile.
