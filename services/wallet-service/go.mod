module github.com/7akoom/ride-platform/services/wallet-service

go 1.26.0

toolchain go1.26.8

require github.com/7akoom/ride-platform/gen/go v0.0.0

require (
	github.com/jackc/pgx-shopspring-decimal v0.0.0-20220624020537-1d36b5a1853e
	github.com/jackc/pgx/v5 v5.10.0
	github.com/nats-io/nats.go v1.53.1
	github.com/shopspring/decimal v1.4.0
	google.golang.org/grpc v1.83.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace github.com/7akoom/ride-platform/gen/go => ../../gen/go
