package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

type SupportRepository struct {
	pool *pgxpool.Pool
}

var _ support.Repository = (*SupportRepository)(nil)

func NewSupportRepository(pool *pgxpool.Pool) *SupportRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &SupportRepository{pool: pool}
}

func pageSize(limit int) int {
	if limit <= 0 {
		return defaultPageSize
	}

	if limit > maxPageSize {
		return maxPageSize
	}

	return limit
}

func nullable(value string) any {
	if value == "" {
		return nil
	}

	return value
}

func ratingArg(rating int) any {
	if rating == 0 {
		return nil
	}

	return rating
}

func textOf(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

// --- categories -----------------------------------------------------------

const categoryColumns = `key, audience, name_en, name_ar, name_ku, default_priority,
	requires_trip, safety, lost_item, active, sort_order`

func scanCategory(row pgx.Row) (support.Category, error) {
	var c support.Category
	var audience, priority string

	err := row.Scan(&c.Key, &audience, &c.NameEn, &c.NameAr, &c.NameKu, &priority,
		&c.RequiresTrip, &c.Safety, &c.LostItem, &c.Active, &c.SortOrder)
	c.Audience = support.CategoryAudience(audience)
	c.DefaultPriority = support.Priority(priority)

	return c, err
}

func (r *SupportRepository) ListCategories(ctx context.Context, includeInactive bool) ([]support.Category, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+categoryColumns+` FROM support_categories
		 WHERE $1 OR active
		 ORDER BY sort_order, key`, includeInactive)
	if err != nil {
		return nil, fmt.Errorf("list support categories: %w", err)
	}
	defer rows.Close()

	var out []support.Category

	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, fmt.Errorf("scan support category: %w", err)
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

func (r *SupportRepository) GetCategory(ctx context.Context, key string) (support.Category, error) {
	c, err := scanCategory(r.pool.QueryRow(ctx, `SELECT `+categoryColumns+` FROM support_categories WHERE key = $1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return support.Category{}, support.ErrCategoryNotFound
	}

	if err != nil {
		return support.Category{}, fmt.Errorf("get support category: %w", err)
	}

	return c, nil
}

func (r *SupportRepository) UpsertCategory(ctx context.Context, c support.Category) (support.Category, error) {
	saved, err := scanCategory(r.pool.QueryRow(ctx,
		`INSERT INTO support_categories
		    (key, audience, name_en, name_ar, name_ku, default_priority,
		     requires_trip, safety, lost_item, active, sort_order)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (key) DO UPDATE SET
		    audience = EXCLUDED.audience, name_en = EXCLUDED.name_en,
		    name_ar = EXCLUDED.name_ar, name_ku = EXCLUDED.name_ku,
		    default_priority = EXCLUDED.default_priority,
		    requires_trip = EXCLUDED.requires_trip, safety = EXCLUDED.safety,
		    lost_item = EXCLUDED.lost_item, active = EXCLUDED.active,
		    sort_order = EXCLUDED.sort_order, updated_at = CURRENT_TIMESTAMP
		 RETURNING `+categoryColumns,
		c.Key, string(c.Audience), c.NameEn, c.NameAr, c.NameKu, string(c.DefaultPriority),
		c.RequiresTrip, c.Safety, c.LostItem, c.Active, c.SortOrder))
	if err != nil {
		return support.Category{}, fmt.Errorf("upsert support category: %w", err)
	}

	return saved, nil
}

// --- tickets --------------------------------------------------------------

const ticketColumns = `id, number, requester_identity_id, audience, requester_profile_id,
	category_key, subject, status, priority, safety, source,
	trip_id::text, transaction_id::text, counterpart_profile_id::text,
	participant_driver_id::text, sos_alert_id::text, assigned_staff_id::text,
	created_at, updated_at, last_message_at, first_response_at, resolved_at, closed_at,
	first_response_due_at, status_changed_at, COALESCE(rating, 0), rating_comment, rated_at`

func scanTicket(row pgx.Row) (support.Ticket, error) {
	var t support.Ticket
	var audience, status, priority string
	var tripID, transactionID, counterpartID, participantID, sosID, assignedID *string

	err := row.Scan(&t.ID, &t.Number, &t.RequesterIdentityID, &audience, &t.RequesterProfileID,
		&t.CategoryKey, &t.Subject, &status, &priority, &t.Safety, &t.Source,
		&tripID, &transactionID, &counterpartID, &participantID, &sosID, &assignedID,
		&t.CreatedAt, &t.UpdatedAt, &t.LastMessageAt, &t.FirstResponseAt, &t.ResolvedAt, &t.ClosedAt,
		&t.FirstResponseDueAt, &t.StatusChangedAt, &t.Rating, &t.RatingComment, &t.RatedAt)

	t.Audience = support.Audience(audience)
	t.Status = support.Status(status)
	t.Priority = support.Priority(priority)
	t.TripID = textOf(tripID)
	t.TransactionID = textOf(transactionID)
	t.CounterpartProfileID = textOf(counterpartID)
	t.ParticipantDriverID = textOf(participantID)
	t.SOSAlertID = textOf(sosID)
	t.AssignedStaffID = textOf(assignedID)

	return t, err
}

func (r *SupportRepository) CreateTicket(ctx context.Context, input support.NewTicket) (support.Ticket, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return support.Ticket{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t := input.Ticket

	if input.MaxOpen > 0 {
		// One person's tickets are counted under a lock on their identity,
		// so two at once cannot both pass the limit.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('support-open:' || $1))`, t.RequesterIdentityID); err != nil {
			return support.Ticket{}, fmt.Errorf("lock requester: %w", err)
		}

		var open int

		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM support_tickets
			 WHERE requester_identity_id = $1 AND audience = $2
			   AND status NOT IN ('resolved', 'closed')`,
			t.RequesterIdentityID, string(t.Audience)).Scan(&open); err != nil {
			return support.Ticket{}, fmt.Errorf("count open tickets: %w", err)
		}

		if open >= input.MaxOpen {
			return support.Ticket{}, support.ErrTooManyOpen
		}
	}

	if err := tx.QueryRow(ctx, `SELECT nextval('support_ticket_number_seq')`).Scan(&t.Number); err != nil {
		return support.Ticket{}, fmt.Errorf("next ticket number: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO support_tickets
		    (id, number, requester_identity_id, audience, requester_profile_id,
		     category_key, subject, status, priority, safety, source,
		     trip_id, transaction_id, counterpart_profile_id, participant_driver_id,
		     sos_alert_id, assigned_staff_id, created_at, updated_at, last_message_at,
		     first_response_due_at, status_changed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)`,
		t.ID, t.Number, t.RequesterIdentityID, string(t.Audience), t.RequesterProfileID,
		t.CategoryKey, t.Subject, string(t.Status), string(t.Priority), t.Safety, t.Source,
		nullable(t.TripID), nullable(t.TransactionID), nullable(t.CounterpartProfileID),
		nullable(t.ParticipantDriverID), nullable(t.SOSAlertID), nullable(t.AssignedStaffID),
		t.CreatedAt, t.UpdatedAt, t.LastMessageAt, t.FirstResponseDueAt, t.StatusChangedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "support_tickets_sos_alert_id_key" {
			return support.Ticket{}, support.ErrDuplicateSOS
		}

		return support.Ticket{}, fmt.Errorf("insert ticket: %w", err)
	}

	if err := insertMessage(ctx, tx, input.Message); err != nil {
		return support.Ticket{}, err
	}

	if input.BuildEvents != nil {
		for _, event := range input.BuildEvents(t) {
			if err := writeOutboxEvent(ctx, tx, t.ID, event.Type, event.Payload); err != nil {
				return support.Ticket{}, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return support.Ticket{}, fmt.Errorf("commit ticket: %w", err)
	}

	return t, nil
}

func insertMessage(ctx context.Context, tx pgx.Tx, m support.Message) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO support_messages
		    (id, ticket_id, author, author_identity_id, author_staff_id, body, internal, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		m.ID, m.TicketID, string(m.Author), nullable(m.AuthorIdentityID), nullable(m.AuthorStaffID),
		m.Body, m.Internal, m.CreatedAt); err != nil {
		return fmt.Errorf("insert support message: %w", err)
	}

	for _, mediaID := range m.AttachmentMediaIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO support_attachments (media_id, ticket_id, message_id, created_at)
			 VALUES ($1, $2, $3, $4)`,
			mediaID, m.TicketID, m.ID, m.CreatedAt); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return support.ErrAttachmentInUse
			}

			return fmt.Errorf("insert support attachment: %w", err)
		}
	}

	return nil
}

func (r *SupportRepository) FindOpenDuplicate(
	ctx context.Context,
	identityID string,
	audience support.Audience,
	categoryKey, tripID string,
) (support.Ticket, bool, error) {
	t, err := scanTicket(r.pool.QueryRow(ctx,
		`SELECT `+ticketColumns+` FROM support_tickets
		 WHERE requester_identity_id = $1 AND audience = $2 AND category_key = $3
		   AND trip_id = $4 AND status NOT IN ('resolved', 'closed')
		 ORDER BY created_at DESC LIMIT 1`,
		identityID, string(audience), categoryKey, tripID))
	if errors.Is(err, pgx.ErrNoRows) {
		return support.Ticket{}, false, nil
	}

	if err != nil {
		return support.Ticket{}, false, fmt.Errorf("find open duplicate ticket: %w", err)
	}

	return t, true, nil
}

func (r *SupportRepository) FindBySOSAlert(ctx context.Context, alertID string) (support.Ticket, bool, error) {
	t, err := scanTicket(r.pool.QueryRow(ctx, `SELECT `+ticketColumns+` FROM support_tickets WHERE sos_alert_id = $1`, alertID))
	if errors.Is(err, pgx.ErrNoRows) {
		return support.Ticket{}, false, nil
	}

	if err != nil {
		return support.Ticket{}, false, fmt.Errorf("find sos ticket: %w", err)
	}

	return t, true, nil
}

func (r *SupportRepository) GetTicket(ctx context.Context, id string) (support.Ticket, error) {
	t, err := scanTicket(r.pool.QueryRow(ctx, `SELECT `+ticketColumns+` FROM support_tickets WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return support.Ticket{}, support.ErrNotFound
	}

	if err != nil {
		return support.Ticket{}, fmt.Errorf("get ticket: %w", err)
	}

	return t, nil
}

func (r *SupportRepository) ListMessages(ctx context.Context, ticketID string, includeInternal bool) ([]support.Message, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT m.id, m.ticket_id, m.author, m.author_identity_id::text, m.author_staff_id::text,
		        m.body, m.internal, m.created_at,
		        COALESCE(array_agg(a.media_id::text ORDER BY a.created_at, a.media_id)
		                 FILTER (WHERE a.media_id IS NOT NULL), '{}')
		 FROM support_messages m
		 LEFT JOIN support_attachments a ON a.message_id = m.id
		 WHERE m.ticket_id = $1 AND ($2 OR NOT m.internal)
		 GROUP BY m.id
		 ORDER BY m.created_at, m.id`, ticketID, includeInternal)
	if err != nil {
		return nil, fmt.Errorf("list support messages: %w", err)
	}
	defer rows.Close()

	var out []support.Message

	for rows.Next() {
		var m support.Message
		var author string
		var identityID, staffID *string

		if err := rows.Scan(&m.ID, &m.TicketID, &author, &identityID, &staffID,
			&m.Body, &m.Internal, &m.CreatedAt, &m.AttachmentMediaIDs); err != nil {
			return nil, fmt.Errorf("scan support message: %w", err)
		}

		m.Author = support.Author(author)
		m.AuthorIdentityID = textOf(identityID)
		m.AuthorStaffID = textOf(staffID)
		out = append(out, m)
	}

	return out, rows.Err()
}

func (r *SupportRepository) TicketHasAttachment(ctx context.Context, ticketID, mediaID string) (bool, bool, error) {
	var internal bool

	err := r.pool.QueryRow(ctx,
		`SELECT m.internal FROM support_attachments a
		 JOIN support_messages m ON m.id = a.message_id
		 WHERE a.media_id = $1 AND a.ticket_id = $2`, mediaID, ticketID).Scan(&internal)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}

	if err != nil {
		return false, false, fmt.Errorf("find support attachment: %w", err)
	}

	return internal, true, nil
}

func encodeCursor(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "|")))
}

func decodeCursor(cursor string, n int) ([]string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("%w: bad page_token", support.ErrInvalidInput)
	}

	parts := strings.Split(string(raw), "|")
	if len(parts) != n {
		return nil, fmt.Errorf("%w: bad page_token", support.ErrInvalidInput)
	}

	return parts, nil
}

func (r *SupportRepository) ListMyTickets(ctx context.Context, q support.MyTicketsQuery) ([]support.Ticket, string, error) {
	limit := pageSize(q.Limit)
	args := []any{q.IdentityID, string(q.Audience), nullable(q.ParticipantDriverID), q.OpenOnly, limit + 1}
	where := ""

	if q.Cursor != "" {
		parts, err := decodeCursor(q.Cursor, 2)
		if err != nil {
			return nil, "", err
		}

		at, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return nil, "", fmt.Errorf("%w: bad page_token", support.ErrInvalidInput)
		}

		args = append(args, at, parts[1])
		where = ` AND (last_message_at, id) < ($6, $7::uuid)`
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+ticketColumns+` FROM support_tickets
		 WHERE ((requester_identity_id = $1 AND audience = $2)
		        OR ($3::uuid IS NOT NULL AND participant_driver_id = $3::uuid))
		   AND (NOT $4 OR status NOT IN ('resolved', 'closed'))`+where+`
		 ORDER BY last_message_at DESC, id DESC
		 LIMIT $5`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list my tickets: %w", err)
	}
	defer rows.Close()

	tickets, err := collectTickets(rows)
	if err != nil {
		return nil, "", err
	}

	next := ""
	if len(tickets) > limit {
		tickets = tickets[:limit]
		last := tickets[limit-1]
		next = encodeCursor(last.LastMessageAt.UTC().Format(time.RFC3339Nano), last.ID)
	}

	return tickets, next, nil
}

func collectTickets(rows pgx.Rows) ([]support.Ticket, error) {
	var out []support.Ticket

	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ticket: %w", err)
		}

		out = append(out, t)
	}

	return out, rows.Err()
}

func priorityRank(p support.Priority) int {
	switch p {
	case support.PriorityUrgent:
		return 4
	case support.PriorityHigh:
		return 3
	case support.PriorityNormal:
		return 2
	default:
		return 1
	}
}

func (r *SupportRepository) ListQueue(ctx context.Context, q support.QueueQuery) ([]support.Ticket, string, error) {
	limit := pageSize(q.Limit)
	conditions := []string{"safety = $1"}
	args := []any{q.Safety}

	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, strings.ReplaceAll(condition, "?", "$"+strconv.Itoa(len(args))))
	}

	if q.Status != "" {
		add("status = ?", string(q.Status))
	} else {
		conditions = append(conditions, "status NOT IN ('resolved', 'closed')")
	}

	if q.CategoryKey != "" {
		add("category_key = ?", q.CategoryKey)
	}

	if q.Priority != "" {
		add("priority = ?", string(q.Priority))
	}

	if q.Audience != "" {
		add("audience = ?", string(q.Audience))
	}

	if q.UnassignedOnly {
		conditions = append(conditions, "assigned_staff_id IS NULL")
	} else if q.AssignedStaffID != "" {
		add("assigned_staff_id = ?", q.AssignedStaffID)
	}

	if q.Cursor != "" {
		parts, err := decodeCursor(q.Cursor, 3)
		if err != nil {
			return nil, "", err
		}

		rank, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, "", fmt.Errorf("%w: bad page_token", support.ErrInvalidInput)
		}

		at, err := time.Parse(time.RFC3339Nano, parts[1])
		if err != nil {
			return nil, "", fmt.Errorf("%w: bad page_token", support.ErrInvalidInput)
		}

		args = append(args, rank, at, parts[2])
		n := len(args)
		conditions = append(conditions, fmt.Sprintf(
			"(priority_rank < $%d OR (priority_rank = $%d AND (created_at, id) > ($%d, $%d::uuid)))",
			n-2, n-2, n-1, n))
	}

	args = append(args, limit+1)

	rows, err := r.pool.Query(ctx,
		`SELECT `+ticketColumns+` FROM support_tickets
		 WHERE `+strings.Join(conditions, " AND ")+`
		 ORDER BY priority_rank DESC, created_at, id
		 LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, "", fmt.Errorf("list support queue: %w", err)
	}
	defer rows.Close()

	tickets, err := collectTickets(rows)
	if err != nil {
		return nil, "", err
	}

	next := ""
	if len(tickets) > limit {
		tickets = tickets[:limit]
		last := tickets[limit-1]
		next = encodeCursor(strconv.Itoa(priorityRank(last.Priority)), last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
	}

	return tickets, next, nil
}

func (r *SupportRepository) UpdateTicket(
	ctx context.Context,
	ticketID string,
	apply func(ticket *support.Ticket) (support.Change, error),
) (support.Ticket, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return support.Ticket{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t, err := scanTicket(tx.QueryRow(ctx, `SELECT `+ticketColumns+` FROM support_tickets WHERE id = $1 FOR UPDATE`, ticketID))
	if errors.Is(err, pgx.ErrNoRows) {
		return support.Ticket{}, support.ErrNotFound
	}

	if err != nil {
		return support.Ticket{}, fmt.Errorf("lock ticket: %w", err)
	}

	before := t

	change, err := apply(&t)
	if err != nil {
		return support.Ticket{}, err
	}

	if t == before && len(change.Messages) == 0 && len(change.Events) == 0 {
		return t, nil
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_tickets SET
		    status = $2, priority = $3, assigned_staff_id = $4, updated_at = $5,
		    last_message_at = $6, first_response_at = $7, resolved_at = $8, closed_at = $9,
		    first_response_due_at = $10, status_changed_at = $11, rating = $12, rating_comment = $13, rated_at = $14
		 WHERE id = $1`,
		t.ID, string(t.Status), string(t.Priority), nullable(t.AssignedStaffID), t.UpdatedAt,
		t.LastMessageAt, t.FirstResponseAt, t.ResolvedAt, t.ClosedAt,
		t.FirstResponseDueAt, t.StatusChangedAt, ratingArg(t.Rating), t.RatingComment, t.RatedAt); err != nil {
		return support.Ticket{}, fmt.Errorf("update ticket: %w", err)
	}

	for _, m := range change.Messages {
		if err := insertMessage(ctx, tx, m); err != nil {
			return support.Ticket{}, err
		}
	}

	for _, event := range change.Events {
		if err := writeOutboxEvent(ctx, tx, t.ID, event.Type, event.Payload); err != nil {
			return support.Ticket{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return support.Ticket{}, fmt.Errorf("commit ticket change: %w", err)
	}

	return t, nil
}

// --- actions --------------------------------------------------------------

const actionColumns = `id, ticket_id, kind, status, amount::text, driver_amount::text,
	target, target_type, target_profile_id::text, target_identity_id::text,
	suspend_until, reactivated_at, reason, requested_by_staff_id, requested_by_identity_id,
	decided_by_staff_id::text, decided_by_identity_id::text, decision_reason, failure_reason,
	acting_identity_id::text, lease_until, created_at, decided_at, completed_at`

func scanAction(row pgx.Row) (support.Action, error) {
	var a support.Action
	var kind, status string
	var amount, driverAmount, target, targetType, targetProfile, targetIdentity *string
	var decidedStaff, decidedIdentity, acting *string

	err := row.Scan(&a.ID, &a.TicketID, &kind, &status, &amount, &driverAmount,
		&target, &targetType, &targetProfile, &targetIdentity,
		&a.SuspendUntil, &a.ReactivatedAt, &a.Reason, &a.RequestedByStaffID, &a.RequestedByIdentityID,
		&decidedStaff, &decidedIdentity, &a.DecisionReason, &a.FailureReason,
		&acting, &a.LeaseUntil, &a.CreatedAt, &a.DecidedAt, &a.CompletedAt)
	if err != nil {
		return support.Action{}, err
	}

	a.Kind = support.ActionKind(kind)
	a.Status = support.ActionStatus(status)
	a.Target = support.ActionTarget(textOf(target))
	a.TargetType = support.Audience(textOf(targetType))
	a.TargetProfileID = textOf(targetProfile)
	a.TargetIdentityID = textOf(targetIdentity)
	a.DecidedByStaffID = textOf(decidedStaff)
	a.DecidedByIdentityID = textOf(decidedIdentity)
	a.ActingIdentityID = textOf(acting)

	if amount != nil {
		d, err := decimal.NewFromString(*amount)
		if err != nil {
			return support.Action{}, fmt.Errorf("parse action amount: %w", err)
		}

		a.Amount = &d
	}

	if driverAmount != nil {
		d, err := decimal.NewFromString(*driverAmount)
		if err != nil {
			return support.Action{}, fmt.Errorf("parse action driver amount: %w", err)
		}

		a.DriverAmount = &d
	}

	return a, nil
}

func decimalArg(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}

	return d.StringFixed(2)
}

func (r *SupportRepository) CreateAction(
	ctx context.Context,
	ticketID string,
	decide func(ticket support.Ticket, moneySoFar decimal.Decimal) (support.Action, support.Message, error),
) (support.Action, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return support.Action{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t, err := scanTicket(tx.QueryRow(ctx, `SELECT `+ticketColumns+` FROM support_tickets WHERE id = $1 FOR UPDATE`, ticketID))
	if errors.Is(err, pgx.ErrNoRows) {
		return support.Action{}, support.ErrNotFound
	}

	if err != nil {
		return support.Action{}, fmt.Errorf("lock ticket: %w", err)
	}

	var soFarText string

	// What the ticket already moved, or is moving, without a second person
	// counts against its limit; approved actions count too.
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(sum(amount), 0)::text FROM support_actions
		 WHERE ticket_id = $1 AND kind IN ('refund', 'waive_fee', 'compensation')
		   AND status IN ('processing', 'completed')`, ticketID).Scan(&soFarText); err != nil {
		return support.Action{}, fmt.Errorf("sum ticket money actions: %w", err)
	}

	soFar, err := decimal.NewFromString(soFarText)
	if err != nil {
		return support.Action{}, fmt.Errorf("parse ticket money actions: %w", err)
	}

	a, message, err := decide(t, soFar)
	if err != nil {
		return support.Action{}, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO support_actions
		    (id, ticket_id, kind, status, amount, driver_amount, target, target_type,
		     target_profile_id, target_identity_id, suspend_until, reason,
		     requested_by_staff_id, requested_by_identity_id, acting_identity_id, lease_until, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		a.ID, a.TicketID, string(a.Kind), string(a.Status), decimalArg(a.Amount), decimalArg(a.DriverAmount),
		nullable(string(a.Target)), nullable(string(a.TargetType)), nullable(a.TargetProfileID),
		nullable(a.TargetIdentityID), a.SuspendUntil, a.Reason, a.RequestedByStaffID,
		a.RequestedByIdentityID, nullable(a.ActingIdentityID), a.LeaseUntil, a.CreatedAt); err != nil {
		return support.Action{}, fmt.Errorf("insert support action: %w", err)
	}

	if err := insertMessage(ctx, tx, message); err != nil {
		return support.Action{}, err
	}

	if _, err := tx.Exec(ctx, `UPDATE support_tickets SET updated_at = $2 WHERE id = $1`, ticketID, a.CreatedAt); err != nil {
		return support.Action{}, fmt.Errorf("touch ticket: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return support.Action{}, fmt.Errorf("commit support action: %w", err)
	}

	return a, nil
}

func (r *SupportRepository) GetAction(ctx context.Context, id string) (support.Action, error) {
	a, err := scanAction(r.pool.QueryRow(ctx, `SELECT `+actionColumns+` FROM support_actions WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return support.Action{}, support.ErrActionNotFound
	}

	if err != nil {
		return support.Action{}, fmt.Errorf("get support action: %w", err)
	}

	return a, nil
}

func (r *SupportRepository) queryActions(ctx context.Context, query string, args ...any) ([]support.Action, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list support actions: %w", err)
	}
	defer rows.Close()

	var out []support.Action

	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, fmt.Errorf("scan support action: %w", err)
		}

		out = append(out, a)
	}

	return out, rows.Err()
}

func (r *SupportRepository) ListActions(ctx context.Context, ticketID string) ([]support.Action, error) {
	return r.queryActions(ctx,
		`SELECT `+actionColumns+` FROM support_actions WHERE ticket_id = $1 ORDER BY created_at, id`, ticketID)
}

func (r *SupportRepository) ListPendingActions(ctx context.Context, limit int, cursor string) ([]support.Action, string, error) {
	size := pageSize(limit)
	args := []any{size + 1}
	where := ""

	if cursor != "" {
		parts, err := decodeCursor(cursor, 2)
		if err != nil {
			return nil, "", err
		}

		at, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return nil, "", fmt.Errorf("%w: bad page_token", support.ErrInvalidInput)
		}

		args = append(args, at, parts[1])
		where = ` AND (created_at, id) > ($2, $3::uuid)`
	}

	actions, err := r.queryActions(ctx,
		`SELECT `+actionColumns+` FROM support_actions
		 WHERE status = 'pending_approval'`+where+`
		 ORDER BY created_at, id LIMIT $1`, args...)
	if err != nil {
		return nil, "", err
	}

	next := ""
	if len(actions) > size {
		actions = actions[:size]
		last := actions[size-1]
		next = encodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
	}

	return actions, next, nil
}

func (r *SupportRepository) DecideAction(
	ctx context.Context,
	actionID string,
	apply func(action *support.Action) (*support.Message, error),
) (support.Action, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return support.Action{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	a, err := scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+` FROM support_actions WHERE id = $1 FOR UPDATE`, actionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return support.Action{}, support.ErrActionNotFound
	}

	if err != nil {
		return support.Action{}, fmt.Errorf("lock support action: %w", err)
	}

	message, err := apply(&a)
	if err != nil {
		return support.Action{}, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_actions SET
		    status = $2, decided_by_staff_id = $3, decided_by_identity_id = $4,
		    decided_at = $5, decision_reason = $6, acting_identity_id = $7, lease_until = $8
		 WHERE id = $1`,
		a.ID, string(a.Status), nullable(a.DecidedByStaffID), nullable(a.DecidedByIdentityID),
		a.DecidedAt, a.DecisionReason, nullable(a.ActingIdentityID), a.LeaseUntil); err != nil {
		return support.Action{}, fmt.Errorf("update support action: %w", err)
	}

	if message != nil {
		if err := insertMessage(ctx, tx, *message); err != nil {
			return support.Action{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return support.Action{}, fmt.Errorf("commit support action decision: %w", err)
	}

	return a, nil
}

func (r *SupportRepository) FinishAction(
	ctx context.Context,
	actionID string,
	status support.ActionStatus,
	failureReason string,
	at time.Time,
	message support.Message,
) (support.Action, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return support.Action{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	a, err := scanAction(tx.QueryRow(ctx,
		`UPDATE support_actions SET status = $2, failure_reason = $3, completed_at = $4, lease_until = NULL,
		    attempts = attempts + 1
		 WHERE id = $1 AND status = 'processing'
		 RETURNING `+actionColumns,
		actionID, string(status), failureReason, at))
	if errors.Is(err, pgx.ErrNoRows) {
		// Finished already (a retry raced the first run): nothing to add.
		return r.GetAction(ctx, actionID)
	}

	if err != nil {
		return support.Action{}, fmt.Errorf("finish support action: %w", err)
	}

	if err := insertMessage(ctx, tx, message); err != nil {
		return support.Action{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return support.Action{}, fmt.Errorf("commit support action outcome: %w", err)
	}

	return a, nil
}

func (r *SupportRepository) ClaimProcessingActions(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]support.Action, error) {
	return r.queryActions(ctx,
		`UPDATE support_actions SET lease_until = $2, attempts = attempts + 1
		 WHERE id IN (
		     SELECT id FROM support_actions
		     WHERE status = 'processing' AND (lease_until IS NULL OR lease_until < $1)
		     ORDER BY created_at
		     LIMIT $3
		     FOR UPDATE SKIP LOCKED)
		 RETURNING `+actionColumns,
		now, now.Add(lease), limit)
}

func (r *SupportRepository) ClaimDueSuspensions(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]support.Action, error) {
	return r.queryActions(ctx,
		`UPDATE support_actions SET lease_until = $2
		 WHERE id IN (
		     SELECT id FROM support_actions
		     WHERE kind = 'suspend' AND status = 'completed' AND reactivated_at IS NULL
		       AND suspend_until IS NOT NULL AND suspend_until <= $1
		       AND (lease_until IS NULL OR lease_until < $1)
		     ORDER BY suspend_until
		     LIMIT $3
		     FOR UPDATE SKIP LOCKED)
		 RETURNING `+actionColumns,
		now, now.Add(lease), limit)
}

func (r *SupportRepository) MarkReactivated(ctx context.Context, identityID string, at time.Time, message *support.Message) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE support_actions SET reactivated_at = $2, lease_until = NULL
		 WHERE kind = 'suspend' AND status = 'completed' AND reactivated_at IS NULL
		   AND target_identity_id = $1`, identityID, at); err != nil {
		return fmt.Errorf("mark suspensions lifted: %w", err)
	}

	if message != nil {
		if err := insertMessage(ctx, tx, *message); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit lifted suspensions: %w", err)
	}

	return nil
}
