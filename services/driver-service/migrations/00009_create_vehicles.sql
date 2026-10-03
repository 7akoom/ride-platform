-- +goose Up

-- A driver's cars. One is active: it is the one riders see and dispatch uses,
-- and its copy stays on the driver row (vehicle_* columns) so every existing
-- reader keeps working. A new car waits for staff review before it can be
-- made active; a retired car is kept for history.
CREATE TABLE vehicles (
    id UUID PRIMARY KEY,

    driver_id UUID NOT NULL REFERENCES drivers (id) ON DELETE CASCADE,

    make VARCHAR(60) NOT NULL,
    model VARCHAR(60) NOT NULL,
    color VARCHAR(40) NOT NULL,
    plate_number VARCHAR(20) NOT NULL,
    -- NULL only for cars registered before years were asked for.
    year SMALLINT,
    vehicle_class VARCHAR(20) NOT NULL DEFAULT 'economy',

    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    active BOOLEAN NOT NULL DEFAULT FALSE,
    rejection_reason VARCHAR(500) NOT NULL DEFAULT '',
    reviewed_by UUID,
    reviewed_at TIMESTAMPTZ,
    retired_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT vehicles_status_check
        CHECK (status IN ('pending', 'approved', 'rejected', 'retired')),

    CONSTRAINT vehicles_class_check
        CHECK (vehicle_class IN ('economy', 'comfort')),

    CONSTRAINT vehicles_year_check
        CHECK (year IS NULL OR year BETWEEN 1980 AND 2100),

    CONSTRAINT vehicles_plate_not_blank_check
        CHECK (length(btrim(plate_number)) > 0),

    CONSTRAINT vehicles_retired_not_active_check
        CHECK (status <> 'retired' OR NOT active),

    CONSTRAINT vehicles_rejection_reason_check
        CHECK (status <> 'rejected' OR length(btrim(rejection_reason)) > 0)
);

CREATE UNIQUE INDEX vehicles_one_active_idx
    ON vehicles (driver_id)
    WHERE active;

-- A plate is on one car in service at a time; a retired car frees it (a car
-- that is sold goes to its new owner).
CREATE UNIQUE INDEX vehicles_plate_in_service_idx
    ON vehicles (plate_number)
    WHERE status <> 'retired';

CREATE INDEX vehicles_driver_idx
    ON vehicles (driver_id, created_at);

CREATE INDEX vehicles_review_queue_idx
    ON vehicles (created_at, id)
    WHERE status = 'pending';

-- Every driver gets their current car as the active vehicle: approved for a
-- driver who already works, waiting with the driver's own review otherwise.
INSERT INTO vehicles
    (id, driver_id, make, model, color, plate_number, vehicle_class, status, active, reviewed_at)
SELECT gen_random_uuid(), d.id, d.vehicle_make, d.vehicle_model, d.vehicle_color,
       d.vehicle_plate_number, d.vehicle_class,
       CASE WHEN d.status IN ('active', 'suspended') THEN 'approved' ELSE 'pending' END,
       TRUE,
       CASE WHEN d.status IN ('active', 'suspended') THEN CURRENT_TIMESTAMP END
FROM drivers AS d;

-- The driver row keeps a copy of the active car; now with its id and year.
ALTER TABLE drivers
    ADD COLUMN vehicle_id UUID,
    ADD COLUMN vehicle_year SMALLINT;

UPDATE drivers AS d
SET vehicle_id = v.id
FROM vehicles AS v
WHERE v.driver_id = d.id AND v.active;

-- Document types belong to the driver (ID, licence, photo) or to a car
-- (registration, car photos).
ALTER TABLE driver_document_types
    ADD COLUMN scope VARCHAR(20) NOT NULL DEFAULT 'driver';

ALTER TABLE driver_document_types
    ADD CONSTRAINT driver_document_types_scope_check
        CHECK (scope IN ('driver', 'vehicle'));

UPDATE driver_document_types
SET scope = 'vehicle', updated_at = CURRENT_TIMESTAMP
WHERE code IN (
    'vehicle_registration_front',
    'vehicle_registration_back',
    'vehicle_photo_front',
    'vehicle_photo_back',
    'vehicle_photo_side',
    'vehicle_photo_interior'
);

-- A car's documents name the car.
ALTER TABLE driver_documents
    ADD COLUMN vehicle_id UUID REFERENCES vehicles (id) ON DELETE CASCADE;

UPDATE driver_documents AS doc
SET vehicle_id = v.id
FROM vehicles AS v, driver_document_types AS t
WHERE v.driver_id = doc.driver_id
  AND v.active
  AND t.code = doc.type_code
  AND t.scope = 'vehicle';

-- At most one pending and one approved per driver, type and car.
DROP INDEX driver_documents_one_pending_idx;
DROP INDEX driver_documents_one_approved_idx;

CREATE UNIQUE INDEX driver_documents_one_pending_idx
    ON driver_documents (driver_id, type_code, COALESCE(vehicle_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE status = 'pending';

CREATE UNIQUE INDEX driver_documents_one_approved_idx
    ON driver_documents (driver_id, type_code, COALESCE(vehicle_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE status = 'approved';

CREATE INDEX driver_documents_vehicle_idx
    ON driver_documents (vehicle_id)
    WHERE vehicle_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS driver_documents_vehicle_idx;
DROP INDEX IF EXISTS driver_documents_one_pending_idx;
DROP INDEX IF EXISTS driver_documents_one_approved_idx;

-- Only the active car's documents fit the old one-per-type shape; the others go.
DELETE FROM driver_documents AS doc
USING vehicles AS v
WHERE v.id = doc.vehicle_id AND NOT v.active;

ALTER TABLE driver_documents DROP COLUMN IF EXISTS vehicle_id;

CREATE UNIQUE INDEX driver_documents_one_pending_idx
    ON driver_documents (driver_id, type_code)
    WHERE status = 'pending';

CREATE UNIQUE INDEX driver_documents_one_approved_idx
    ON driver_documents (driver_id, type_code)
    WHERE status = 'approved';

ALTER TABLE driver_document_types DROP CONSTRAINT IF EXISTS driver_document_types_scope_check;
ALTER TABLE driver_document_types DROP COLUMN IF EXISTS scope;

ALTER TABLE drivers
    DROP COLUMN IF EXISTS vehicle_id,
    DROP COLUMN IF EXISTS vehicle_year;

DROP TABLE IF EXISTS vehicles;
