#!/bin/sh

set -eu

NATS_URL="${NATS_URL:-nats://nats:4222}"
STREAM_NAME="${STREAM_NAME:-IDENTITY_EVENTS}"
STREAM_CONFIG="${STREAM_CONFIG:-/streams/identity-events.json}"

until nats \
    --server "$NATS_URL" \
    account info >/dev/null 2>&1
do
    sleep 1
done

if nats \
    --server "$NATS_URL" \
    stream info "$STREAM_NAME" >/dev/null 2>&1
then
    nats \
        --server "$NATS_URL" \
        stream edit "$STREAM_NAME" \
        --config "$STREAM_CONFIG" \
        --force
else
    nats \
        --server "$NATS_URL" \
        stream add \
        --config "$STREAM_CONFIG"
fi

nats \
    --server "$NATS_URL" \
    stream info "$STREAM_NAME"