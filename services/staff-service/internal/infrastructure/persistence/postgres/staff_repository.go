package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/staff-service/internal/application/staff"
)

const (
	uniqueViolationCode     = "23505"
	foreignKeyViolationCode = "23503"
)

// querier is what both the pool and a transaction offer.
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type StaffRepository struct {
	pool *pgxpool.Pool
}

var _ staff.Repository = (*StaffRepository)(nil)

func NewStaffRepository(pool *pgxpool.Pool) *StaffRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &StaffRepository{pool: pool}
}

const memberColumns = `id, COALESCE(identity_id::text, ''), email, display_name, status,
	COALESCE(invited_by_staff_id::text, ''), invited_at, activated_at, created_at, updated_at`

func scanMember(row pgx.Row, member *staff.Member) error {
	var status string

	if err := row.Scan(
		&member.ID,
		&member.IdentityID,
		&member.Email,
		&member.DisplayName,
		&status,
		&member.InvitedByStaffID,
		&member.InvitedAt,
		&member.ActivatedAt,
		&member.CreatedAt,
		&member.UpdatedAt,
	); err != nil {
		return err
	}

	member.Status = staff.Status(status)

	return nil
}

func (r *StaffRepository) CountMembers(ctx context.Context) (int, error) {
	var count int

	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM staff_members`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count staff members: %w", err)
	}

	return count, nil
}

func (r *StaffRepository) CreateMember(ctx context.Context, input staff.NewMember) (staff.Member, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return staff.Member{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var invitedBy any
	if input.InvitedByStaffID != "" {
		invitedBy = input.InvitedByStaffID
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO staff_members (id, email, display_name, status, invited_by_staff_id, invited_at)
		 VALUES ($1, $2, $3, 'invited', $4, $5)`,
		input.ID,
		input.Email,
		input.DisplayName,
		invitedBy,
		input.InvitedAt,
	); err != nil {
		if isPgError(err, uniqueViolationCode) {
			return staff.Member{}, staff.ErrEmailAlreadyInvited
		}

		return staff.Member{}, fmt.Errorf("insert staff member: %w", err)
	}

	if err := insertMemberRoles(ctx, tx, input.ID, input.RoleIDs); err != nil {
		return staff.Member{}, err
	}

	member, err := findMember(ctx, tx, `id = $1`, input.ID)
	if err != nil {
		return staff.Member{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return staff.Member{}, fmt.Errorf("commit transaction: %w", err)
	}

	return member, nil
}

func (r *StaffRepository) FindMemberByID(ctx context.Context, id string) (staff.Member, error) {
	return findMember(ctx, r.pool, `id = $1`, id)
}

func (r *StaffRepository) FindMemberByIdentityID(ctx context.Context, identityID string) (staff.Member, error) {
	return findMember(ctx, r.pool, `identity_id = $1`, identityID)
}

func (r *StaffRepository) FindInvitedMemberByEmails(ctx context.Context, emails []string) (staff.Member, error) {
	return findMember(ctx, r.pool, `status = 'invited' AND email = ANY($1::text[])`, emails)
}

func findMember(ctx context.Context, q querier, where string, arg any) (staff.Member, error) {
	var member staff.Member

	row := q.QueryRow(
		ctx,
		`SELECT `+memberColumns+` FROM staff_members WHERE `+where+` ORDER BY created_at, id LIMIT 1`,
		arg,
	)

	if err := scanMember(row, &member); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return staff.Member{}, staff.ErrMemberNotFound
		}

		return staff.Member{}, fmt.Errorf("select staff member: %w", err)
	}

	roles, err := rolesOfMembers(ctx, q, []string{member.ID})
	if err != nil {
		return staff.Member{}, err
	}

	member.Roles = roles[member.ID]

	return member, nil
}

func (r *StaffRepository) ListMembers(ctx context.Context, query staff.MemberQuery) ([]staff.Member, error) {
	rows, err := r.pool.Query(ctx, listMembersQuery(query.Status != "", query.AfterID != ""), listMembersArgs(query)...)
	if err != nil {
		return nil, fmt.Errorf("list staff members: %w", err)
	}
	defer rows.Close()

	var members []staff.Member

	for rows.Next() {
		var member staff.Member
		if err := scanMember(rows, &member); err != nil {
			return nil, fmt.Errorf("scan staff member: %w", err)
		}

		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list staff members: %w", err)
	}

	ids := make([]string, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.ID)
	}

	roles, err := rolesOfMembers(ctx, r.pool, ids)
	if err != nil {
		return nil, err
	}

	for i := range members {
		members[i].Roles = roles[members[i].ID]
	}

	return members, nil
}

// listMembersQuery pages newest first; the cursor is the last member of the
// previous page, looked up by id.
func listMembersQuery(byStatus bool, afterCursor bool) string {
	query := `SELECT ` + memberColumns + ` FROM staff_members WHERE TRUE`
	arg := 1

	if byStatus {
		query += fmt.Sprintf(` AND status = $%d`, arg)
		arg++
	}

	if afterCursor {
		query += fmt.Sprintf(
			` AND (created_at, id) < (SELECT created_at, id FROM staff_members WHERE id = $%d)`,
			arg,
		)
		arg++
	}

	return query + fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, arg)
}

func listMembersArgs(query staff.MemberQuery) []any {
	var args []any

	if query.Status != "" {
		args = append(args, string(query.Status))
	}

	if query.AfterID != "" {
		args = append(args, query.AfterID)
	}

	return append(args, query.Limit)
}

func (r *StaffRepository) ActivateMember(
	ctx context.Context,
	id string,
	identityID string,
	at time.Time,
) (staff.Member, error) {
	return r.changeMember(ctx, id, func(tx pgx.Tx) error {
		tag, err := tx.Exec(
			ctx,
			`UPDATE staff_members
			 SET status = 'active', identity_id = $2, activated_at = $3, updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1 AND status = 'invited'`,
			id,
			identityID,
			at,
		)
		if err != nil {
			if isPgError(err, uniqueViolationCode) {
				return staff.ErrIdentityAlreadyStaff
			}

			return fmt.Errorf("activate staff member: %w", err)
		}

		if tag.RowsAffected() == 0 {
			return staff.ErrInvalidTransition
		}

		return nil
	})
}

func (r *StaffRepository) SetMemberStatus(
	ctx context.Context,
	id string,
	from staff.Status,
	to staff.Status,
) (staff.Member, error) {
	return r.changeMember(ctx, id, func(tx pgx.Tx) error {
		tag, err := tx.Exec(
			ctx,
			`UPDATE staff_members SET status = $3, updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1 AND status = $2`,
			id,
			string(from),
			string(to),
		)
		if err != nil {
			return fmt.Errorf("change staff member status: %w", err)
		}

		if tag.RowsAffected() == 0 {
			return staff.ErrInvalidTransition
		}

		return nil
	})
}

func (r *StaffRepository) SetMemberRoles(ctx context.Context, id string, roleIDs []string) (staff.Member, error) {
	return r.changeMember(ctx, id, func(tx pgx.Tx) error {
		tag, err := tx.Exec(
			ctx,
			`UPDATE staff_members SET updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND status <> 'revoked'`,
			id,
		)
		if err != nil {
			return fmt.Errorf("touch staff member: %w", err)
		}

		if tag.RowsAffected() == 0 {
			return staff.ErrInvalidTransition
		}

		if _, err := tx.Exec(ctx, `DELETE FROM staff_member_roles WHERE staff_id = $1`, id); err != nil {
			return fmt.Errorf("clear staff member roles: %w", err)
		}

		return insertMemberRoles(ctx, tx, id, roleIDs)
	})
}

// changeMember runs change in a transaction that also guards the owners: it
// serializes every owner-affecting change on the owner role's row, and refuses
// a change that takes the last active owner away.
func (r *StaffRepository) changeMember(
	ctx context.Context,
	id string,
	change func(tx pgx.Tx) error,
) (staff.Member, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return staff.Member{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT id FROM roles WHERE key = 'owner' FOR UPDATE`); err != nil {
		return staff.Member{}, fmt.Errorf("lock the owner role: %w", err)
	}

	before, err := countActiveOwners(ctx, tx)
	if err != nil {
		return staff.Member{}, err
	}

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM staff_members WHERE id = $1)`, id).Scan(&exists); err != nil {
		return staff.Member{}, fmt.Errorf("find staff member: %w", err)
	}

	if !exists {
		return staff.Member{}, staff.ErrMemberNotFound
	}

	if err := change(tx); err != nil {
		return staff.Member{}, err
	}

	after, err := countActiveOwners(ctx, tx)
	if err != nil {
		return staff.Member{}, err
	}

	if before > 0 && after == 0 {
		return staff.Member{}, staff.ErrLastOwner
	}

	member, err := findMember(ctx, tx, `id = $1`, id)
	if err != nil {
		return staff.Member{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return staff.Member{}, fmt.Errorf("commit transaction: %w", err)
	}

	return member, nil
}

func countActiveOwners(ctx context.Context, q querier) (int, error) {
	var count int

	if err := q.QueryRow(
		ctx,
		`SELECT count(*)
		 FROM staff_members m
		 JOIN staff_member_roles mr ON mr.staff_id = m.id
		 JOIN roles r ON r.id = mr.role_id
		 WHERE r.key = 'owner' AND m.status = 'active'`,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active owners: %w", err)
	}

	return count, nil
}

func insertMemberRoles(ctx context.Context, q querier, staffID string, roleIDs []string) error {
	if len(roleIDs) == 0 {
		return nil
	}

	if _, err := q.Exec(
		ctx,
		`INSERT INTO staff_member_roles (staff_id, role_id)
		 SELECT $1, role_id FROM unnest($2::uuid[]) AS role_id`,
		staffID,
		roleIDs,
	); err != nil {
		if isPgError(err, foreignKeyViolationCode) {
			return staff.ErrRoleNotFound
		}

		return fmt.Errorf("insert staff member roles: %w", err)
	}

	return nil
}

func isPgError(err error, code string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == code
}
