package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/staff-service/internal/application/staff"
)

const roleSelect = `SELECT r.id, COALESCE(r.key, ''), r.name, r.description, r.is_system,
	COALESCE(array_agg(p.permission ORDER BY p.permission) FILTER (WHERE p.permission IS NOT NULL), '{}'),
	r.created_at, r.updated_at
	FROM roles r
	LEFT JOIN role_permissions p ON p.role_id = r.id`

const roleGroupOrder = ` GROUP BY r.id ORDER BY r.is_system DESC, lower(r.name), r.id`

func scanRole(row pgx.Row, role *staff.Role) error {
	return row.Scan(
		&role.ID,
		&role.Key,
		&role.Name,
		&role.Description,
		&role.System,
		&role.Permissions,
		&role.CreatedAt,
		&role.UpdatedAt,
	)
}

func queryRoles(ctx context.Context, q querier, where string, args ...any) ([]staff.Role, error) {
	query := roleSelect
	if where != "" {
		query += ` WHERE ` + where
	}

	rows, err := q.Query(ctx, query+roleGroupOrder, args...)
	if err != nil {
		return nil, fmt.Errorf("select roles: %w", err)
	}
	defer rows.Close()

	var roles []staff.Role

	for rows.Next() {
		var role staff.Role
		if err := scanRole(rows, &role); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}

		roles = append(roles, role)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select roles: %w", err)
	}

	return roles, nil
}

func (r *StaffRepository) ListRoles(ctx context.Context) ([]staff.Role, error) {
	return queryRoles(ctx, r.pool, "")
}

func (r *StaffRepository) FindRolesByIDs(ctx context.Context, ids []string) ([]staff.Role, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	return queryRoles(ctx, r.pool, `r.id = ANY($1::uuid[])`, ids)
}

func (r *StaffRepository) FindRoleByID(ctx context.Context, id string) (staff.Role, error) {
	return findOneRole(ctx, r.pool, `r.id = $1`, id)
}

func (r *StaffRepository) FindRoleByKey(ctx context.Context, key string) (staff.Role, error) {
	return findOneRole(ctx, r.pool, `r.key = $1`, key)
}

func findOneRole(ctx context.Context, q querier, where string, arg any) (staff.Role, error) {
	roles, err := queryRoles(ctx, q, where, arg)
	if err != nil {
		return staff.Role{}, err
	}

	if len(roles) == 0 {
		return staff.Role{}, staff.ErrRoleNotFound
	}

	return roles[0], nil
}

// rolesOfMembers returns the roles of each member, keyed by staff id.
func rolesOfMembers(ctx context.Context, q querier, staffIDs []string) (map[string][]staff.Role, error) {
	out := map[string][]staff.Role{}
	if len(staffIDs) == 0 {
		return out, nil
	}

	rows, err := q.Query(
		ctx,
		`SELECT mr.staff_id, r.id, COALESCE(r.key, ''), r.name, r.description, r.is_system,
		        COALESCE(array_agg(p.permission ORDER BY p.permission) FILTER (WHERE p.permission IS NOT NULL), '{}'),
		        r.created_at, r.updated_at
		 FROM staff_member_roles mr
		 JOIN roles r ON r.id = mr.role_id
		 LEFT JOIN role_permissions p ON p.role_id = r.id
		 WHERE mr.staff_id = ANY($1::uuid[])
		 GROUP BY mr.staff_id, r.id
		 ORDER BY mr.staff_id, r.is_system DESC, lower(r.name), r.id`,
		staffIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("select member roles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var staffID string
		var role staff.Role

		if err := rows.Scan(
			&staffID,
			&role.ID,
			&role.Key,
			&role.Name,
			&role.Description,
			&role.System,
			&role.Permissions,
			&role.CreatedAt,
			&role.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan member role: %w", err)
		}

		out[staffID] = append(out[staffID], role)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select member roles: %w", err)
	}

	return out, nil
}

func (r *StaffRepository) CreateRole(ctx context.Context, role staff.Role) (staff.Role, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return staff.Role{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO roles (id, name, description, is_system) VALUES ($1, $2, $3, FALSE)`,
		role.ID,
		role.Name,
		role.Description,
	); err != nil {
		if isPgError(err, uniqueViolationCode) {
			return staff.Role{}, staff.ErrRoleNameTaken
		}

		return staff.Role{}, fmt.Errorf("insert role: %w", err)
	}

	if err := insertRolePermissions(ctx, tx, role.ID, role.Permissions); err != nil {
		return staff.Role{}, err
	}

	created, err := findOneRole(ctx, tx, `r.id = $1`, role.ID)
	if err != nil {
		return staff.Role{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return staff.Role{}, fmt.Errorf("commit transaction: %w", err)
	}

	return created, nil
}

func (r *StaffRepository) UpdateRole(ctx context.Context, role staff.Role) (staff.Role, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return staff.Role{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(
		ctx,
		`UPDATE roles SET name = $2, description = $3, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND NOT is_system`,
		role.ID,
		role.Name,
		role.Description,
	)
	if err != nil {
		if isPgError(err, uniqueViolationCode) {
			return staff.Role{}, staff.ErrRoleNameTaken
		}

		return staff.Role{}, fmt.Errorf("update role: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return staff.Role{}, staff.ErrRoleNotFound
	}

	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, role.ID); err != nil {
		return staff.Role{}, fmt.Errorf("clear role permissions: %w", err)
	}

	if err := insertRolePermissions(ctx, tx, role.ID, role.Permissions); err != nil {
		return staff.Role{}, err
	}

	updated, err := findOneRole(ctx, tx, `r.id = $1`, role.ID)
	if err != nil {
		return staff.Role{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return staff.Role{}, fmt.Errorf("commit transaction: %w", err)
	}

	return updated, nil
}

func (r *StaffRepository) DeleteRole(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM roles WHERE id = $1 AND NOT is_system`, id)
	if err != nil {
		if isPgError(err, foreignKeyViolationCode) {
			return staff.ErrRoleInUse
		}

		return fmt.Errorf("delete role: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return staff.ErrRoleNotFound
	}

	return nil
}

func insertRolePermissions(ctx context.Context, q querier, roleID string, permissions []string) error {
	if len(permissions) == 0 {
		return nil
	}

	if _, err := q.Exec(
		ctx,
		`INSERT INTO role_permissions (role_id, permission)
		 SELECT $1, permission FROM unnest($2::text[]) AS permission`,
		roleID,
		permissions,
	); err != nil {
		return fmt.Errorf("insert role permissions: %w", err)
	}

	return nil
}
