-- +goose Up

-- Links a rider sends to people they trust to follow a trip. Only the
-- SHA-256 of a link's token is kept: the token itself is seen once, by the
-- rider, when the link is made.
CREATE TABLE trip_shares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trip_id UUID NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,

    CONSTRAINT trip_shares_token_hash_unique UNIQUE (token_hash),
    CONSTRAINT trip_shares_token_hash_check CHECK (octet_length(token_hash) = 32),
    CONSTRAINT trip_shares_expiry_check CHECK (expires_at > created_at)
);

CREATE INDEX trip_shares_trip_idx ON trip_shares (trip_id) WHERE revoked_at IS NULL;

-- +goose Down

DROP TABLE trip_shares;
