-- +goose Up

-- location-service starts enforcing places.manage: operations curates the
-- places riders pick from (airports, malls, hotels). The owner role holds
-- every permission without rows.
INSERT INTO role_permissions (role_id, permission) VALUES
    ('5e7a0000-0000-4000-8000-000000000002', 'places.manage')
ON CONFLICT DO NOTHING;

-- +goose Down

-- Custom roles may have been given places.manage too; the permission leaves
-- the catalog with this rollback, so it goes from every role.
DELETE FROM role_permissions WHERE permission = 'places.manage';
