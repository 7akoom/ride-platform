# goose, to apply every service's migrations on the VPS:
#   docker compose -f infrastructure/deploy/compose.vps.yaml --env-file instance/instance.env run --rm migrate
FROM golang:1.26-alpine AS build
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.26.0

FROM alpine:3.22
COPY --from=build /go/bin/goose /usr/local/bin/goose
