-- +goose Up

CREATE TABLE roles (
    id UUID PRIMARY KEY,

    -- Stable key of a system role (owner, operations); NULL for custom roles.
    key VARCHAR(40) NULL,

    name VARCHAR(80) NOT NULL,
    description VARCHAR(500) NOT NULL DEFAULT '',

    is_system BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT roles_key_unique
        UNIQUE (key),

    CONSTRAINT roles_system_has_key_check
        CHECK (is_system = (key IS NOT NULL)),

    CONSTRAINT roles_name_not_blank_check
        CHECK (length(btrim(name)) > 0 AND name = btrim(name))
);

CREATE UNIQUE INDEX roles_name_lower_unique
    ON roles (lower(name));

CREATE TABLE role_permissions (
    role_id UUID NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission VARCHAR(80) NOT NULL,

    PRIMARY KEY (role_id, permission)
);

CREATE TABLE staff_members (
    id UUID PRIMARY KEY,

    -- Bound when the invitation is accepted.
    identity_id UUID NULL,

    email VARCHAR(254) NOT NULL,
    display_name VARCHAR(120) NOT NULL,

    status VARCHAR(20) NOT NULL DEFAULT 'invited',

    invited_by_staff_id UUID NULL REFERENCES staff_members (id),
    invited_at TIMESTAMPTZ NOT NULL,
    activated_at TIMESTAMPTZ NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT staff_members_status_check
        CHECK (status IN ('invited', 'active', 'suspended', 'revoked')),

    CONSTRAINT staff_members_email_lower_check
        CHECK (email = lower(btrim(email)) AND length(email) > 0),

    CONSTRAINT staff_members_display_name_check
        CHECK (length(btrim(display_name)) > 0 AND display_name = btrim(display_name)),

    -- An invitation has no identity yet; an active or suspended member always has one.
    CONSTRAINT staff_members_identity_state_check
        CHECK (
            (status = 'invited' AND identity_id IS NULL AND activated_at IS NULL)
            OR (status IN ('active', 'suspended') AND identity_id IS NOT NULL AND activated_at IS NOT NULL)
            OR (status = 'revoked' AND identity_id IS NULL)
        )
);

CREATE UNIQUE INDEX staff_members_identity_unique
    ON staff_members (identity_id)
    WHERE identity_id IS NOT NULL;

-- One live record per email; a revoked invitation frees the address again.
CREATE UNIQUE INDEX staff_members_email_unique
    ON staff_members (email)
    WHERE status <> 'revoked';

CREATE INDEX staff_members_created_idx
    ON staff_members (created_at DESC, id DESC);

CREATE TABLE staff_member_roles (
    staff_id UUID NOT NULL REFERENCES staff_members (id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles (id) ON DELETE RESTRICT,

    PRIMARY KEY (staff_id, role_id)
);

CREATE INDEX staff_member_roles_role_idx
    ON staff_member_roles (role_id);

CREATE TABLE audit_entries (
    id UUID PRIMARY KEY,

    occurred_at TIMESTAMPTZ NOT NULL,

    -- NULL when the caller is not staff at all.
    actor_staff_id UUID NULL,
    actor_identity_id UUID NOT NULL,

    permission VARCHAR(200) NOT NULL,
    method VARCHAR(200) NOT NULL,
    target_id VARCHAR(200) NOT NULL DEFAULT '',

    decision VARCHAR(10) NOT NULL,
    outcome VARCHAR(10) NULL,
    outcome_code VARCHAR(40) NOT NULL DEFAULT '',
    completed_at TIMESTAMPTZ NULL,

    CONSTRAINT audit_entries_decision_check
        CHECK (decision IN ('allowed', 'denied')),

    CONSTRAINT audit_entries_outcome_check
        CHECK (outcome IS NULL OR outcome IN ('pending', 'succeeded', 'failed')),

    -- Only an allowed attempt has an outcome.
    CONSTRAINT audit_entries_decision_outcome_check
        CHECK (
            (decision = 'denied' AND outcome IS NULL)
            OR (decision = 'allowed' AND outcome IS NOT NULL)
        ),

    CONSTRAINT audit_entries_completion_check
        CHECK ((outcome IN ('succeeded', 'failed')) = (completed_at IS NOT NULL))
);

CREATE INDEX audit_entries_occurred_idx
    ON audit_entries (occurred_at DESC, id DESC);

CREATE INDEX audit_entries_actor_idx
    ON audit_entries (actor_staff_id, occurred_at DESC, id DESC);

CREATE INDEX audit_entries_target_idx
    ON audit_entries (target_id, occurred_at DESC, id DESC)
    WHERE target_id <> '';

-- The audit log is append-only: an entry is never deleted, and the only change
-- ever made to one is recording how a pending action ended.
-- +goose StatementBegin
CREATE FUNCTION audit_entries_guard() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'audit entries cannot be deleted';
    END IF;

    IF OLD.outcome IS DISTINCT FROM 'pending'
        OR NEW.outcome NOT IN ('succeeded', 'failed')
        OR NEW.id IS DISTINCT FROM OLD.id
        OR NEW.occurred_at IS DISTINCT FROM OLD.occurred_at
        OR NEW.actor_staff_id IS DISTINCT FROM OLD.actor_staff_id
        OR NEW.actor_identity_id IS DISTINCT FROM OLD.actor_identity_id
        OR NEW.permission IS DISTINCT FROM OLD.permission
        OR NEW.method IS DISTINCT FROM OLD.method
        OR NEW.target_id IS DISTINCT FROM OLD.target_id
        OR NEW.decision IS DISTINCT FROM OLD.decision THEN
        RAISE EXCEPTION 'audit entries can only be completed once';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER audit_entries_guard
    BEFORE UPDATE OR DELETE ON audit_entries
    FOR EACH ROW EXECUTE FUNCTION audit_entries_guard();

-- System roles. The owner role holds every permission in code, so it has no
-- rows here; the others get theirs explicitly, in the migration that adds a
-- permission they should have.
INSERT INTO roles (id, key, name, description, is_system) VALUES
    ('5e7a0000-0000-4000-8000-000000000001', 'owner', 'Owner',
     'Everything, including managing other owners', TRUE),
    ('5e7a0000-0000-4000-8000-000000000002', 'operations', 'Operations',
     'Driver review and service zones', TRUE);

INSERT INTO role_permissions (role_id, permission) VALUES
    ('5e7a0000-0000-4000-8000-000000000002', 'drivers.read'),
    ('5e7a0000-0000-4000-8000-000000000002', 'drivers.approve'),
    ('5e7a0000-0000-4000-8000-000000000002', 'zones.manage');

-- +goose Down

DROP TABLE IF EXISTS audit_entries;
DROP FUNCTION IF EXISTS audit_entries_guard();
DROP TABLE IF EXISTS staff_member_roles;
DROP TABLE IF EXISTS staff_members;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
