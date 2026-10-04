-- +goose Up

-- A driver's optional personal details, apart from the profile row every
-- service reads.
CREATE TABLE driver_details (
    driver_id UUID PRIMARY KEY REFERENCES drivers (id) ON DELETE CASCADE,
    gender VARCHAR(10) NOT NULL DEFAULT '',
    date_of_birth DATE NULL,
    nationality VARCHAR(2) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT driver_details_gender_check CHECK (gender IN ('', 'male', 'female')),
    CONSTRAINT driver_details_nationality_check CHECK (nationality = '' OR nationality ~ '^[A-Z]{2}$')
);

-- An approved driver's name changes only after staff check it.
CREATE TABLE driver_name_changes (
    id UUID PRIMARY KEY,
    driver_id UUID NOT NULL REFERENCES drivers (id) ON DELETE CASCADE,
    current_name VARCHAR(120) NOT NULL,
    requested_name VARCHAR(120) NOT NULL,
    reason VARCHAR(300) NOT NULL DEFAULT '',
    status VARCHAR(10) NOT NULL DEFAULT 'pending',
    rejection_reason VARCHAR(500) NOT NULL DEFAULT '',
    decided_by UUID NULL,
    created_at TIMESTAMPTZ NOT NULL,
    decided_at TIMESTAMPTZ NULL,

    CONSTRAINT driver_name_changes_status_check CHECK (status IN ('pending', 'approved', 'rejected')),
    CONSTRAINT driver_name_changes_decided_check CHECK ((status = 'pending') = (decided_at IS NULL)),
    CONSTRAINT driver_name_changes_name_check CHECK (length(btrim(requested_name)) > 0)
);

CREATE UNIQUE INDEX driver_name_changes_one_pending_idx
    ON driver_name_changes (driver_id) WHERE status = 'pending';

CREATE INDEX driver_name_changes_driver_idx ON driver_name_changes (driver_id, created_at DESC);

CREATE INDEX driver_name_changes_pending_idx
    ON driver_name_changes (created_at, id) WHERE status = 'pending';

-- +goose Down

DROP TABLE driver_name_changes;
DROP TABLE driver_details;
