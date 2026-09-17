# syntax=docker/dockerfile:1
#
# Builds the API gateway image. Not part of the shared
# infrastructure/docker/Dockerfile (that one assumes every service
# lives under services/<name> — the gateway lives under
# infrastructure/gateway instead). Build context must be the repo
# root (needs both gen/go and this module's own source):
#
#   docker build \
#     -f infrastructure/gateway/Dockerfile \
#     -t ride-platform/gateway:dev \
#     .

ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src

COPY gen/go ./gen/go
COPY infrastructure/gateway ./infrastructure/gateway

WORKDIR /src/infrastructure/gateway
ENV GOWORK=off
ENV CGO_ENABLED=0

RUN go mod edit -replace github.com/7akoom/ride-platform/gen/go=../../gen/go \
	&& go mod download \
	&& go build -trimpath -ldflags="-s -w" -o /out/gateway "./cmd/gateway"

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=build /out/gateway ./gateway
USER nonroot:nonroot
ENV APP_ENV=production
ENTRYPOINT ["/app/gateway"]
