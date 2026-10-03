-- +goose Up

CREATE TABLE support_categories (
    key VARCHAR(40) PRIMARY KEY,
    audience VARCHAR(10) NOT NULL,
    name_en VARCHAR(80) NOT NULL,
    name_ar VARCHAR(80) NOT NULL,
    name_ku VARCHAR(80) NOT NULL,
    default_priority VARCHAR(10) NOT NULL DEFAULT 'normal',
    requires_trip BOOLEAN NOT NULL DEFAULT FALSE,
    safety BOOLEAN NOT NULL DEFAULT FALSE,
    lost_item BOOLEAN NOT NULL DEFAULT FALSE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT support_categories_key_check CHECK (key ~ '^[a-z][a-z0-9_]{1,39}$'),
    CONSTRAINT support_categories_audience_check CHECK (audience IN ('rider', 'driver', 'both')),
    CONSTRAINT support_categories_priority_check
        CHECK (default_priority IN ('low', 'normal', 'high', 'urgent')),
    CONSTRAINT support_categories_lost_item_trip_check CHECK (NOT lost_item OR requires_trip),
    CONSTRAINT support_categories_safety_urgent_check CHECK (NOT safety OR default_priority = 'urgent')
);

CREATE SEQUENCE support_ticket_number_seq;

CREATE TABLE support_tickets (
    id UUID PRIMARY KEY,
    number BIGINT NOT NULL UNIQUE DEFAULT nextval('support_ticket_number_seq'),

    requester_identity_id UUID NOT NULL,
    audience VARCHAR(10) NOT NULL,
    requester_profile_id UUID NOT NULL,

    category_key VARCHAR(40) NOT NULL REFERENCES support_categories (key),
    subject VARCHAR(120) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'open',
    priority VARCHAR(10) NOT NULL,
    priority_rank SMALLINT GENERATED ALWAYS AS (
        CASE priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'normal' THEN 2 ELSE 1 END
    ) STORED,
    safety BOOLEAN NOT NULL DEFAULT FALSE,
    source VARCHAR(10) NOT NULL DEFAULT 'app',

    trip_id UUID NULL,
    transaction_id UUID NULL,
    counterpart_profile_id UUID NULL,
    participant_driver_id UUID NULL,
    sos_alert_id UUID NULL UNIQUE,

    assigned_staff_id UUID NULL,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    last_message_at TIMESTAMPTZ NOT NULL,
    first_response_at TIMESTAMPTZ NULL,
    resolved_at TIMESTAMPTZ NULL,
    closed_at TIMESTAMPTZ NULL,

    CONSTRAINT support_tickets_audience_check CHECK (audience IN ('rider', 'driver')),
    CONSTRAINT support_tickets_status_check
        CHECK (status IN ('open', 'in_progress', 'waiting_user', 'resolved', 'closed')),
    CONSTRAINT support_tickets_priority_check CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    CONSTRAINT support_tickets_source_check CHECK (source IN ('app', 'sos')),
    CONSTRAINT support_tickets_safety_urgent_check CHECK (NOT safety OR priority = 'urgent'),
    CONSTRAINT support_tickets_sos_check CHECK ((source = 'sos') = (sos_alert_id IS NOT NULL)),
    CONSTRAINT support_tickets_closed_check CHECK ((status = 'closed') = (closed_at IS NOT NULL))
);

CREATE INDEX support_tickets_requester_idx
    ON support_tickets (requester_identity_id, audience, last_message_at DESC, id DESC);

CREATE INDEX support_tickets_participant_idx
    ON support_tickets (participant_driver_id, last_message_at DESC, id DESC)
    WHERE participant_driver_id IS NOT NULL;

CREATE INDEX support_tickets_queue_idx
    ON support_tickets (safety, priority_rank DESC, created_at, id)
    WHERE status NOT IN ('resolved', 'closed');

CREATE INDEX support_tickets_trip_idx
    ON support_tickets (trip_id)
    WHERE trip_id IS NOT NULL;

CREATE TABLE support_messages (
    id UUID PRIMARY KEY,
    ticket_id UUID NOT NULL REFERENCES support_tickets (id),
    author VARCHAR(12) NOT NULL,
    author_identity_id UUID NULL,
    author_staff_id UUID NULL,
    body TEXT NOT NULL,
    internal BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT support_messages_author_check
        CHECK (author IN ('requester', 'participant', 'staff', 'system')),
    CONSTRAINT support_messages_body_check CHECK (length(body) BETWEEN 1 AND 4000),
    CONSTRAINT support_messages_system_internal_check CHECK (author <> 'system' OR internal),
    CONSTRAINT support_messages_internal_staff_check CHECK (NOT internal OR author IN ('staff', 'system')),
    CONSTRAINT support_messages_staff_check CHECK ((author = 'staff') = (author_staff_id IS NOT NULL))
);

CREATE INDEX support_messages_ticket_idx ON support_messages (ticket_id, created_at, id);

CREATE TABLE support_attachments (
    media_id UUID PRIMARY KEY,
    ticket_id UUID NOT NULL REFERENCES support_tickets (id),
    message_id UUID NOT NULL REFERENCES support_messages (id),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX support_attachments_message_idx ON support_attachments (message_id);

CREATE TABLE support_actions (
    id UUID PRIMARY KEY,
    ticket_id UUID NOT NULL REFERENCES support_tickets (id),
    kind VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL,

    amount NUMERIC(18, 2) NULL,
    driver_amount NUMERIC(18, 2) NULL,

    target VARCHAR(12) NULL,
    target_type VARCHAR(10) NULL,
    target_profile_id UUID NULL,
    target_identity_id UUID NULL,
    suspend_until TIMESTAMPTZ NULL,
    reactivated_at TIMESTAMPTZ NULL,

    reason VARCHAR(300) NOT NULL,
    requested_by_staff_id UUID NOT NULL,
    requested_by_identity_id UUID NOT NULL,
    decided_by_staff_id UUID NULL,
    decided_by_identity_id UUID NULL,
    decision_reason VARCHAR(300) NOT NULL DEFAULT '',
    failure_reason VARCHAR(300) NOT NULL DEFAULT '',

    -- Who the action is carried out as (the requester, or the approver), and
    -- the lease of the worker retrying it while other services do not answer.
    acting_identity_id UUID NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    lease_until TIMESTAMPTZ NULL,

    created_at TIMESTAMPTZ NOT NULL,
    decided_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,

    CONSTRAINT support_actions_kind_check
        CHECK (kind IN ('refund', 'waive_fee', 'compensation', 'suspend', 'reactivate')),
    CONSTRAINT support_actions_status_check
        CHECK (status IN ('pending_approval', 'processing', 'completed', 'rejected', 'failed')),
    CONSTRAINT support_actions_amount_check
        CHECK (
            (kind IN ('refund', 'waive_fee', 'compensation') AND amount > 0)
            OR (kind IN ('suspend', 'reactivate') AND amount IS NULL)
        ),
    CONSTRAINT support_actions_driver_amount_check
        CHECK (driver_amount IS NULL OR (kind IN ('refund', 'waive_fee') AND driver_amount >= 0 AND driver_amount <= amount)),
    CONSTRAINT support_actions_target_check
        CHECK (
            (kind IN ('suspend', 'reactivate')
                AND target IN ('requester', 'counterpart')
                AND target_type IN ('rider', 'driver')
                AND target_profile_id IS NOT NULL
                AND target_identity_id IS NOT NULL)
            OR (kind NOT IN ('suspend', 'reactivate') AND target IS NULL)
        ),
    CONSTRAINT support_actions_suspend_until_check
        CHECK (suspend_until IS NULL OR kind = 'suspend'),
    CONSTRAINT support_actions_acting_check
        CHECK (status IN ('pending_approval', 'rejected') OR acting_identity_id IS NOT NULL)
);

CREATE INDEX support_actions_ticket_idx ON support_actions (ticket_id, created_at);

CREATE INDEX support_actions_pending_idx
    ON support_actions (created_at, id)
    WHERE status = 'pending_approval';

CREATE INDEX support_actions_processing_idx
    ON support_actions (lease_until)
    WHERE status = 'processing';

CREATE INDEX support_actions_suspensions_due_idx
    ON support_actions (suspend_until)
    WHERE kind = 'suspend' AND status = 'completed'
        AND suspend_until IS NOT NULL AND reactivated_at IS NULL;

INSERT INTO support_categories
    (key, audience, name_en, name_ar, name_ku, default_priority, requires_trip, safety, lost_item, sort_order)
VALUES
    ('trip_fare', 'both', 'Fare or charges', 'الأجرة أو الرسوم', 'کرێ یان تێچوون', 'normal', TRUE, FALSE, FALSE, 10),
    ('trip_route', 'rider', 'Route or trip problem', 'مشكلة في المسار أو الرحلة', 'کێشەی ڕێگا یان گەشت', 'normal', TRUE, FALSE, FALSE, 20),
    ('driver_behavior', 'rider', 'Captain''s behaviour', 'سلوك الكابتن', 'ڕەفتاری کاپتن', 'high', TRUE, FALSE, FALSE, 30),
    ('rider_behavior', 'driver', 'Rider''s behaviour', 'سلوك الراكب', 'ڕەفتاری سەرنشین', 'high', TRUE, FALSE, FALSE, 30),
    ('lost_item', 'rider', 'I lost an item', 'نسيت شيئاً في السيارة', 'شتێکم لە ئۆتۆمبێلەکە بەجێهێشت', 'high', TRUE, FALSE, TRUE, 40),
    ('safety', 'both', 'Safety incident', 'حادثة تتعلق بالسلامة', 'ڕووداوی سەلامەتی', 'urgent', FALSE, TRUE, FALSE, 50),
    ('accident', 'both', 'Accident', 'حادث سير', 'ڕووداوی هاتوچۆ', 'urgent', FALSE, TRUE, FALSE, 60),
    ('sos', 'both', 'SOS alert', 'نداء استغاثة', 'داوای فریاکەوتن', 'urgent', FALSE, TRUE, FALSE, 999),
    ('payment_wallet', 'both', 'Payment or wallet', 'الدفع أو المحفظة', 'پارەدان یان جزدان', 'normal', FALSE, FALSE, FALSE, 70),
    ('earnings_payout', 'driver', 'Earnings or payout', 'الأرباح أو السحب', 'داهات یان وەرگرتنی پارە', 'normal', FALSE, FALSE, FALSE, 80),
    ('documents_vehicle', 'driver', 'Documents or vehicle', 'الوثائق أو السيارة', 'بەڵگەنامە یان ئۆتۆمبێل', 'normal', FALSE, FALSE, FALSE, 90),
    ('profile_change', 'driver', 'Change my name or details', 'تعديل الاسم أو البيانات الشخصية', 'گۆڕینی ناو یان زانیاری', 'normal', FALSE, FALSE, FALSE, 100),
    ('account', 'both', 'My account', 'حسابي', 'هەژمارەکەم', 'normal', FALSE, FALSE, FALSE, 110),
    ('app_issue', 'both', 'App problem', 'مشكلة في التطبيق', 'کێشەی ئەپ', 'low', FALSE, FALSE, FALSE, 120),
    ('other', 'both', 'Something else', 'أمر آخر', 'شتێکی تر', 'normal', FALSE, FALSE, FALSE, 130);

-- +goose Down

DROP TABLE support_actions;
DROP TABLE support_attachments;
DROP TABLE support_messages;
DROP TABLE support_tickets;
DROP SEQUENCE support_ticket_number_seq;
DROP TABLE support_categories;
