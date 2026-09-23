package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/staff-service/internal/application/staff"
)

func (r *StaffRepository) RecordAudit(ctx context.Context, entry staff.AuditEntry) error {
	var actorStaffID any
	if entry.ActorStaffID != "" {
		actorStaffID = entry.ActorStaffID
	}

	var outcome any
	if entry.Outcome != "" {
		outcome = string(entry.Outcome)
	}

	if _, err := r.pool.Exec(
		ctx,
		`INSERT INTO audit_entries
		    (id, occurred_at, actor_staff_id, actor_identity_id, permission, method, target_id, decision, outcome)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		entry.ID,
		entry.OccurredAt,
		actorStaffID,
		entry.ActorIdentityID,
		entry.Permission,
		entry.Method,
		entry.TargetID,
		string(entry.Decision),
		outcome,
	); err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}

	return nil
}

func (r *StaffRepository) CompleteAudit(
	ctx context.Context,
	id string,
	outcome staff.Outcome,
	code string,
	at time.Time,
) error {
	tag, err := r.pool.Exec(
		ctx,
		`UPDATE audit_entries SET outcome = $2, outcome_code = $3, completed_at = $4
		 WHERE id = $1 AND outcome = 'pending'`,
		id,
		string(outcome),
		code,
		at,
	)
	if err != nil {
		return fmt.Errorf("complete audit entry: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return staff.ErrAuditEntryNotFound
	}

	return nil
}

const auditColumns = `id, occurred_at, COALESCE(actor_staff_id::text, ''), actor_identity_id, permission,
	method, target_id, decision, COALESCE(outcome, ''), outcome_code, completed_at`

func (r *StaffRepository) ListAudit(ctx context.Context, query staff.AuditQuery) ([]staff.AuditEntry, error) {
	sql, args := listAuditQuery(query)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()

	var entries []staff.AuditEntry

	for rows.Next() {
		var entry staff.AuditEntry
		var decision, outcome string

		if err := rows.Scan(
			&entry.ID,
			&entry.OccurredAt,
			&entry.ActorStaffID,
			&entry.ActorIdentityID,
			&entry.Permission,
			&entry.Method,
			&entry.TargetID,
			&decision,
			&outcome,
			&entry.OutcomeCode,
			&entry.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}

		entry.Decision = staff.Decision(decision)
		entry.Outcome = staff.Outcome(outcome)
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}

	return entries, nil
}

// listAuditQuery builds the filtered, newest-first page query. Every value is
// a bind parameter; only the fixed condition texts are concatenated.
func listAuditQuery(query staff.AuditQuery) (string, []any) {
	sql := `SELECT ` + auditColumns + ` FROM audit_entries WHERE TRUE`

	var args []any

	add := func(condition string, value any) {
		args = append(args, value)
		sql += fmt.Sprintf(condition, len(args))
	}

	if query.ActorStaffID != "" {
		add(` AND actor_staff_id = $%d`, query.ActorStaffID)
	}

	if query.Permission != "" {
		add(` AND permission = $%d`, query.Permission)
	}

	if query.TargetID != "" {
		add(` AND target_id = $%d`, query.TargetID)
	}

	if query.OccurredAfter != nil {
		add(` AND occurred_at >= $%d`, *query.OccurredAfter)
	}

	if query.OccurredBefore != nil {
		add(` AND occurred_at < $%d`, *query.OccurredBefore)
	}

	if query.AfterID != "" {
		add(` AND (occurred_at, id) < (SELECT occurred_at, id FROM audit_entries WHERE id = $%d)`, query.AfterID)
	}

	add(` ORDER BY occurred_at DESC, id DESC LIMIT $%d`, query.Limit)

	return sql, args
}
