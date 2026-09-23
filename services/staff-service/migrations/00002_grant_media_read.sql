-- +goose Up

-- media-service starts enforcing media.read: operations reviews driver
-- documents, so it may view uploaded files. The owner role holds every
-- permission without rows.
INSERT INTO role_permissions (role_id, permission) VALUES
    ('5e7a0000-0000-4000-8000-000000000002', 'media.read')
ON CONFLICT DO NOTHING;

-- +goose Down

-- Custom roles may have been given media.read too; the permission leaves the
-- catalog with this rollback, so it goes from every role.
DELETE FROM role_permissions WHERE permission = 'media.read';
