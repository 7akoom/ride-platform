-- +goose Up

CREATE TABLE drivers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    identity_id UUID NOT NULL,

    display_name VARCHAR(120) NOT NULL,

    status VARCHAR(20) NOT NULL DEFAULT 'active',
    availability_status VARCHAR(20) NOT NULL DEFAULT 'offline',

    vehicle_make VARCHAR(60) NOT NULL,
    vehicle_model VARCHAR(60) NOT NULL,
    vehicle_color VARCHAR(40) NOT NULL,
    vehicle_plate_number VARCHAR(20) NOT NULL,

    rating_average NUMERIC(3, 2) NOT NULL DEFAULT 5.00,
    rating_count INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT drivers_identity_id_unique
        UNIQUE (identity_id),

    CONSTRAINT drivers_plate_number_unique
        UNIQUE (vehicle_plate_number),

    CONSTRAINT drivers_status_check
        CHECK (
            status IN (
                'active',
                'suspended'
            )
        ),

    CONSTRAINT drivers_availability_status_check
        CHECK (
            availability_status IN (
                'offline',
                'available',
                'busy'
            )
        ),

    CONSTRAINT drivers_display_name_not_blank_check
        CHECK (
            length(btrim(display_name)) > 0
        ),

    CONSTRAINT drivers_display_name_trimmed_check
        CHECK (
            display_name = btrim(display_name)
        ),

    CONSTRAINT drivers_vehicle_plate_not_blank_check
        CHECK (
            length(btrim(vehicle_plate_number)) > 0
        ),

    CONSTRAINT drivers_rating_average_range_check
        CHECK (
            rating_average >= 0
            AND rating_average <= 5
        ),

    CONSTRAINT drivers_rating_count_non_negative_check
        CHECK (
            rating_count >= 0
        )
);

CREATE INDEX drivers_identity_id_idx
    ON drivers (
        identity_id
    );

CREATE INDEX drivers_availability_status_idx
    ON drivers (
        availability_status
    )
    WHERE availability_status = 'available';

-- +goose Down

DROP TABLE IF EXISTS drivers;
