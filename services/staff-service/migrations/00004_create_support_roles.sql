-- +goose Up

-- support-service starts enforcing the support.* permissions. Two system
-- roles: agents answer tickets and refund up to the ticket limit; leads also
-- approve money over it, assign tickets, suspend accounts and work safety
-- tickets. Categories (support.configure) stay with the owner, who holds every
-- permission without rows. A deployment that already has a custom role with
-- one of these names keeps it, and the system role is skipped.
INSERT INTO roles (id, key, name, description, is_system) VALUES
    ('5e7a0000-0000-4000-8000-000000000003', 'support_agent', 'Support agent',
     'Answers support tickets; refunds and fee waivers up to the ticket limit', TRUE),
    ('5e7a0000-0000-4000-8000-000000000004', 'support_lead', 'Support lead',
     'Support agent, plus approvals over the limit, assignment, suspensions and safety tickets', TRUE)
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
FROM roles r
JOIN (VALUES
    ('support_agent', 'support.read'),
    ('support_agent', 'support.reply'),
    ('support_agent', 'support.refund'),
    ('support_lead', 'support.read'),
    ('support_lead', 'support.reply'),
    ('support_lead', 'support.refund'),
    ('support_lead', 'support.manage'),
    ('support_lead', 'support.approve'),
    ('support_lead', 'support.suspend'),
    ('support_lead', 'support.safety')
) AS p (role_key, permission) ON p.role_key = r.key
ON CONFLICT DO NOTHING;

-- +goose Down

-- Custom roles may have been given support permissions too; the permissions
-- leave the catalog with this rollback, so they go from every role. The two
-- system roles go only when nobody holds them (otherwise they stay, empty).
DELETE FROM role_permissions WHERE permission LIKE 'support.%';

DELETE FROM roles r
WHERE r.key IN ('support_agent', 'support_lead')
  AND NOT EXISTS (SELECT 1 FROM staff_member_roles m WHERE m.role_id = r.id);
