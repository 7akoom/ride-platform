module github.com/7akoom/ride-platform/services/location-service

go 1.26.0

toolchain go1.26.8

require github.com/7akoom/ride-platform/gen/go v0.0.0

require (
	github.com/valkey-io/valkey-go v1.0.76
	google.golang.org/grpc v1.83.0
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace github.com/7akoom/ride-platform/gen/go => ../../gen/go
