# syntax=docker/dockerfile:1
#
# One Dockerfile builds every backend service — pass the service
# directory name as a build arg:
#
#   docker build \
#     --build-arg SERVICE_NAME=rider-service \
#     -f infrastructure/docker/Dockerfile \
#     -t ride-platform/rider-service:dev \
#     .
#
# Build context must be the repo root (not this directory), since the
# build needs both gen/go (the shared generated-proto module) and the
# target service's own source. See scripts/docker-build-all.sh to
# build every service in one shot.

ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-bookworm AS build
ARG SERVICE_NAME
WORKDIR /src

# gen/go is a separate local Go module (protobuf-generated code).
# Outside Docker it's resolved via the repo's go.work; inside the
# build, only this service + gen/go are present, so replace it with a
# local path instead of relying on the (absent) workspace file.
COPY gen/go ./gen/go
COPY services/${SERVICE_NAME} ./services/${SERVICE_NAME}

WORKDIR /src/services/${SERVICE_NAME}
ENV GOWORK=off
ENV CGO_ENABLED=0

# Two BuildKit cache mounts keep the downloaded modules (/go/pkg/mod) and the
# compiled packages (the Go build cache) between builds. Without them every
# build downloads every module and recompiles grpc, protobuf, pgx, nats...
# from scratch, even for a one-line change. Both caches are shared by every
# service built on this machine, so a dependency compiled for one service is
# reused by the others. They live outside the image: nothing from them ends up
# in the final layers.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod edit -replace github.com/7akoom/ride-platform/gen/go=../../gen/go \
        && go mod download \
        && go build -trimpath -ldflags="-s -w" -o /out/service "./cmd/${SERVICE_NAME}"

# Minimal, non-root runtime image. No shell, no package manager, no
# migrations — migrations are applied separately with goose against
# the target database, not by the running binary (see each service's
# README). Secrets (DB URL, signing keys, tokens) come from the
# environment / mounted files at `docker run`, never baked in here.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=build /out/service ./service
USER nonroot:nonroot

# The image defaults to production. identity-service reads APP_ENV; every other
# service reads ENVIRONMENT, and refuses to start outside ENVIRONMENT=development
# while its internal service token is still the published placeholder (or too
# short). Local development is unaffected: each service's .env sets
# ENVIRONMENT=development, and an env_file overrides an image default.
ENV APP_ENV=production
ENV ENVIRONMENT=production

ENTRYPOINT ["/app/service"]
