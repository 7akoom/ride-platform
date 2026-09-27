-- +goose Up

-- What a driver is asked for. Each deployment can change the list (staff with
-- drivers.configure): make a type optional, stop asking for it, add another.
CREATE TABLE driver_document_types (
    code VARCHAR(40) PRIMARY KEY,

    -- The media-service purpose the file is uploaded with.
    media_purpose VARCHAR(20) NOT NULL,

    name_en VARCHAR(80) NOT NULL,
    name_ar VARCHAR(80) NOT NULL,
    name_ku VARCHAR(80) NOT NULL,

    required BOOLEAN NOT NULL DEFAULT TRUE,
    requires_number BOOLEAN NOT NULL DEFAULT FALSE,
    requires_expiry BOOLEAN NOT NULL DEFAULT FALSE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT driver_document_types_code_check
        CHECK (code ~ '^[a-z][a-z0-9_]{1,39}$'),

    CONSTRAINT driver_document_types_media_purpose_check
        CHECK (media_purpose IN ('driver_document', 'profile_photo')),

    CONSTRAINT driver_document_types_names_check
        CHECK (
            length(btrim(name_en)) > 0
            AND length(btrim(name_ar)) > 0
            AND length(btrim(name_ku)) > 0
        ),

    CONSTRAINT driver_document_types_sort_order_check
        CHECK (sort_order BETWEEN 0 AND 1000)
);

INSERT INTO driver_document_types
    (code, media_purpose, name_en, name_ar, name_ku, required, requires_number, requires_expiry, sort_order)
VALUES
    ('profile_photo', 'profile_photo',
     'Profile photo', 'الصورة الشخصية', 'وێنەی کەسی', TRUE, FALSE, FALSE, 10),
    ('national_id_front', 'driver_document',
     'National ID (front)', 'البطاقة الوطنية (الوجه)', 'ناسنامەی نیشتمانی (پێشەوە)', TRUE, TRUE, TRUE, 20),
    ('national_id_back', 'driver_document',
     'National ID (back)', 'البطاقة الوطنية (الظهر)', 'ناسنامەی نیشتمانی (پشتەوە)', TRUE, FALSE, FALSE, 30),
    ('driving_licence_front', 'driver_document',
     'Driving licence (front)', 'إجازة السوق (الوجه)', 'مۆڵەتی شۆفێری (پێشەوە)', TRUE, TRUE, TRUE, 40),
    ('driving_licence_back', 'driver_document',
     'Driving licence (back)', 'إجازة السوق (الظهر)', 'مۆڵەتی شۆفێری (پشتەوە)', TRUE, FALSE, FALSE, 50),
    ('vehicle_registration_front', 'driver_document',
     'Vehicle registration (front)', 'سنوية السيارة (الوجه)', 'ساڵانەی ئۆتۆمبێل (پێشەوە)', TRUE, TRUE, TRUE, 60),
    ('vehicle_registration_back', 'driver_document',
     'Vehicle registration (back)', 'سنوية السيارة (الظهر)', 'ساڵانەی ئۆتۆمبێل (پشتەوە)', TRUE, FALSE, FALSE, 70),
    ('vehicle_photo_front', 'driver_document',
     'Car photo (front)', 'صورة السيارة (من الأمام)', 'وێنەی ئۆتۆمبێل (پێشەوە)', TRUE, FALSE, FALSE, 80),
    ('vehicle_photo_back', 'driver_document',
     'Car photo (back)', 'صورة السيارة (من الخلف)', 'وێنەی ئۆتۆمبێل (دواوە)', TRUE, FALSE, FALSE, 90),
    ('vehicle_photo_side', 'driver_document',
     'Car photo (side)', 'صورة السيارة (من الجانب)', 'وێنەی ئۆتۆمبێل (لاتەنیشت)', TRUE, FALSE, FALSE, 100),
    ('vehicle_photo_interior', 'driver_document',
     'Car photo (inside)', 'صورة السيارة (من الداخل)', 'وێنەی ئۆتۆمبێل (ناوەوە)', TRUE, FALSE, FALSE, 110);

-- Every document a driver handed in. Per driver and type there is at most one
-- pending and one approved at a time; a newer submission supersedes the older
-- pending or rejected one, and an approval supersedes the older approved one.
CREATE TABLE driver_documents (
    id UUID PRIMARY KEY,

    driver_id UUID NOT NULL REFERENCES drivers (id) ON DELETE CASCADE,
    type_code VARCHAR(40) NOT NULL REFERENCES driver_document_types (code),

    -- The file in media-service, held there while the document is current.
    media_id UUID NOT NULL,

    document_number VARCHAR(50) NOT NULL DEFAULT '',
    -- The last day the document is valid; NULL when the type has no expiry.
    expires_on DATE,

    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    rejection_reason VARCHAR(500) NOT NULL DEFAULT '',

    -- The staff identity that reviewed it; NULL for the internal token.
    reviewed_by UUID,
    reviewed_at TIMESTAMPTZ,
    superseded_at TIMESTAMPTZ,

    -- Expiry reminders: the smallest reminder threshold (days) already sent,
    -- and when the "expired" notice went out.
    reminded_days INTEGER,
    expired_notified_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT driver_documents_status_check
        CHECK (status IN ('pending', 'approved', 'rejected', 'superseded')),

    CONSTRAINT driver_documents_media_unique
        UNIQUE (media_id),

    CONSTRAINT driver_documents_rejection_reason_check
        CHECK (status <> 'rejected' OR length(btrim(rejection_reason)) > 0)
);

CREATE UNIQUE INDEX driver_documents_one_pending_idx
    ON driver_documents (driver_id, type_code)
    WHERE status = 'pending';

CREATE UNIQUE INDEX driver_documents_one_approved_idx
    ON driver_documents (driver_id, type_code)
    WHERE status = 'approved';

-- One person, one account: an approved ID or licence number belongs to one driver.
CREATE UNIQUE INDEX driver_documents_approved_number_idx
    ON driver_documents (type_code, document_number)
    WHERE status = 'approved' AND document_number <> '';

CREATE INDEX driver_documents_driver_idx
    ON driver_documents (driver_id, created_at DESC);

CREATE INDEX driver_documents_review_queue_idx
    ON driver_documents (created_at, id)
    WHERE status = 'pending';

CREATE INDEX driver_documents_expiry_idx
    ON driver_documents (expires_on)
    WHERE status = 'approved' AND expires_on IS NOT NULL;

-- +goose Down

DROP TABLE IF EXISTS driver_documents;
DROP TABLE IF EXISTS driver_document_types;
