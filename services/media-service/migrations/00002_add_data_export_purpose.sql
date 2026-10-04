-- +goose Up

-- A person's "Download your data" ZIP, stored by identity-service (never
-- uploaded).
ALTER TABLE media_objects DROP CONSTRAINT media_objects_purpose_check;

ALTER TABLE media_objects
    ADD CONSTRAINT media_objects_purpose_check
        CHECK (purpose IN ('driver_document', 'profile_photo', 'address_photo', 'support_attachment', 'data_export'));

-- +goose Down

DELETE FROM media_objects WHERE purpose = 'data_export';

ALTER TABLE media_objects DROP CONSTRAINT media_objects_purpose_check;

ALTER TABLE media_objects
    ADD CONSTRAINT media_objects_purpose_check
        CHECK (purpose IN ('driver_document', 'profile_photo', 'address_photo', 'support_attachment'));
