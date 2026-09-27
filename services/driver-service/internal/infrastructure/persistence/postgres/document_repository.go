package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/documents"
)

const (
	eventDocumentReviewed = "driver.document_reviewed"
	eventDocumentExpiring = "driver.document_expiring"
	eventDocumentExpired  = "driver.document_expired"
)

const documentColumns = `d.id::text, d.driver_id::text, d.type_code, d.media_id::text, d.document_number,
	d.expires_on, d.status, d.rejection_reason, COALESCE(d.reviewed_by::text, ''), d.reviewed_at,
	d.created_at, d.updated_at`

const typeColumns = `code, media_purpose, name_en, name_ar, name_ku, required, requires_number,
	requires_expiry, active, sort_order`

// DocumentRepository keeps document types and the documents drivers hand in.
type DocumentRepository struct {
	pool *pgxpool.Pool
}

var _ documents.Repository = (*DocumentRepository)(nil)

func NewDocumentRepository(pool *pgxpool.Pool) *DocumentRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &DocumentRepository{pool: pool}
}

// --- types -------------------------------------------------------------------

func (r *DocumentRepository) ListTypes(ctx context.Context, includeInactive bool) ([]documents.Type, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+typeColumns+`
		 FROM driver_document_types
		 WHERE active OR $1
		 ORDER BY sort_order, code`,
		includeInactive,
	)
	if err != nil {
		return nil, fmt.Errorf("select document types: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (documents.Type, error) {
		var t documents.Type

		return t, scanType(row, &t)
	})
}

func (r *DocumentRepository) GetType(ctx context.Context, code string) (documents.Type, error) {
	var t documents.Type

	err := scanType(r.pool.QueryRow(ctx, `SELECT `+typeColumns+` FROM driver_document_types WHERE code = $1`, code), &t)
	if errors.Is(err, pgx.ErrNoRows) {
		return documents.Type{}, documents.ErrTypeNotFound
	}

	if err != nil {
		return documents.Type{}, fmt.Errorf("select document type: %w", err)
	}

	return t, nil
}

func (r *DocumentRepository) UpsertType(ctx context.Context, t documents.Type) (documents.Type, error) {
	var saved documents.Type

	err := scanType(r.pool.QueryRow(
		ctx,
		`INSERT INTO driver_document_types
		    (code, media_purpose, name_en, name_ar, name_ku, required, requires_number,
		     requires_expiry, active, sort_order)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (code) DO UPDATE
		 SET media_purpose = EXCLUDED.media_purpose,
		     name_en = EXCLUDED.name_en,
		     name_ar = EXCLUDED.name_ar,
		     name_ku = EXCLUDED.name_ku,
		     required = EXCLUDED.required,
		     requires_number = EXCLUDED.requires_number,
		     requires_expiry = EXCLUDED.requires_expiry,
		     active = EXCLUDED.active,
		     sort_order = EXCLUDED.sort_order,
		     updated_at = CURRENT_TIMESTAMP
		 RETURNING `+typeColumns,
		t.Code, string(t.MediaPurpose), t.NameEN, t.NameAR, t.NameKU,
		t.Required, t.RequiresNumber, t.RequiresExpiry, t.Active, t.SortOrder,
	), &saved)
	if err != nil {
		return documents.Type{}, fmt.Errorf("upsert document type: %w", err)
	}

	return saved, nil
}

// --- documents ---------------------------------------------------------------

func (r *DocumentRepository) Submit(ctx context.Context, d documents.Document) (documents.Document, []documents.Document, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return documents.Document{}, nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// One submission per driver at a time: the row lock orders them.
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM drivers WHERE id = $1 FOR UPDATE`, d.DriverID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return documents.Document{}, nil, documents.ErrDriverNotFound
		}

		return documents.Document{}, nil, fmt.Errorf("lock driver: %w", err)
	}

	superseded, err := collectDocuments(tx.Query(
		ctx,
		`UPDATE driver_documents AS d
		 SET status = 'superseded', superseded_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE d.driver_id = $1 AND d.type_code = $2 AND d.status IN ('pending', 'rejected')
		 RETURNING `+documentColumns,
		d.DriverID, d.TypeCode,
	))
	if err != nil {
		return documents.Document{}, nil, fmt.Errorf("supersede earlier documents: %w", err)
	}

	var created documents.Document

	err = scanDocument(tx.QueryRow(
		ctx,
		`INSERT INTO driver_documents AS d
		    (id, driver_id, type_code, media_id, document_number, expires_on, status)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending')
		 RETURNING `+documentColumns,
		d.ID, d.DriverID, d.TypeCode, d.MediaID, d.Number, nullableDate(d.ExpiresOn),
	), &created)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == "driver_documents_media_unique" {
			return documents.Document{}, nil, documents.ErrMediaAlreadyUsed
		}

		return documents.Document{}, nil, fmt.Errorf("insert document: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return documents.Document{}, nil, fmt.Errorf("commit transaction: %w", err)
	}

	// The file of a document being replaced is the same as the new one only
	// if the driver re-sent it; that one must not be deleted.
	superseded = slices.DeleteFunc(superseded, func(old documents.Document) bool { return old.MediaID == created.MediaID })

	return created, superseded, nil
}

func (r *DocumentRepository) Get(ctx context.Context, id string) (documents.Document, error) {
	var d documents.Document

	err := scanDocument(r.pool.QueryRow(ctx, `SELECT `+documentColumns+` FROM driver_documents AS d WHERE d.id = $1`, id), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return documents.Document{}, documents.ErrDocumentNotFound
	}

	if err != nil {
		return documents.Document{}, fmt.Errorf("select document: %w", err)
	}

	return d, nil
}

func (r *DocumentRepository) ListByDriver(ctx context.Context, driverID string, includeSuperseded bool) ([]documents.Document, error) {
	docs, err := collectDocuments(r.pool.Query(
		ctx,
		`SELECT `+documentColumns+`
		 FROM driver_documents AS d
		 WHERE d.driver_id = $1 AND (d.status <> 'superseded' OR $2)
		 ORDER BY d.created_at DESC, d.id DESC`,
		driverID, includeSuperseded,
	))
	if err != nil {
		return nil, fmt.Errorf("select driver documents: %w", err)
	}

	return docs, nil
}

func (r *DocumentRepository) Approve(ctx context.Context, record documents.ApproveRecord) (documents.Document, []documents.Document, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return documents.Document{}, nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current documents.Document
	if err := scanDocument(tx.QueryRow(ctx, `SELECT `+documentColumns+` FROM driver_documents AS d WHERE d.id = $1 FOR UPDATE`, record.DocumentID), &current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return documents.Document{}, nil, documents.ErrDocumentNotFound
		}

		return documents.Document{}, nil, fmt.Errorf("lock document: %w", err)
	}

	if current.Status != documents.StatusPending {
		return documents.Document{}, nil, documents.ErrDocumentNotPending
	}

	superseded, err := collectDocuments(tx.Query(
		ctx,
		`UPDATE driver_documents AS d
		 SET status = 'superseded', superseded_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE d.driver_id = $1 AND d.type_code = $2 AND d.status = 'approved' AND d.id <> $3
		 RETURNING `+documentColumns,
		current.DriverID, current.TypeCode, current.ID,
	))
	if err != nil {
		return documents.Document{}, nil, fmt.Errorf("supersede the approved document: %w", err)
	}

	var approved documents.Document

	err = scanDocument(tx.QueryRow(
		ctx,
		`UPDATE driver_documents AS d
		 SET status = 'approved',
		     document_number = $2,
		     expires_on = $3,
		     rejection_reason = '',
		     reviewed_by = NULLIF($4, '')::uuid,
		     reviewed_at = CURRENT_TIMESTAMP,
		     reminded_days = NULL,
		     expired_notified_at = NULL,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE d.id = $1
		 RETURNING `+documentColumns,
		current.ID, record.Number, nullableDate(record.ExpiresOn), record.ReviewedBy,
	), &approved)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == "driver_documents_approved_number_idx" {
			return documents.Document{}, nil, documents.ErrNumberTaken
		}

		return documents.Document{}, nil, fmt.Errorf("approve document: %w", err)
	}

	if err := writeOutboxEvent(ctx, tx, approved.DriverID, eventDocumentReviewed, reviewedPayload(approved, record.Type, "approved", "", false)); err != nil {
		return documents.Document{}, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return documents.Document{}, nil, fmt.Errorf("commit transaction: %w", err)
	}

	return approved, superseded, nil
}

func (r *DocumentRepository) Reject(ctx context.Context, record documents.RejectRecord) (documents.Document, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return documents.Document{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rejected documents.Document

	err = scanDocument(tx.QueryRow(
		ctx,
		`UPDATE driver_documents AS d
		 SET status = 'rejected',
		     rejection_reason = $2,
		     reviewed_by = NULLIF($3, '')::uuid,
		     reviewed_at = CURRENT_TIMESTAMP,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE d.id = $1 AND d.status = $4
		 RETURNING `+documentColumns,
		record.DocumentID, record.Reason, record.ReviewedBy, string(record.ExpectedStatus),
	), &rejected)
	if errors.Is(err, pgx.ErrNoRows) {
		return documents.Document{}, documents.ErrDocumentNotReviewable
	}

	if err != nil {
		return documents.Document{}, fmt.Errorf("reject document: %w", err)
	}

	if record.TakeOffline {
		if _, err := tx.Exec(
			ctx,
			`UPDATE drivers
			 SET availability_status = 'offline', updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1 AND availability_status = 'available'`,
			rejected.DriverID,
		); err != nil {
			return documents.Document{}, fmt.Errorf("take the driver offline: %w", err)
		}
	}

	withdrawn := record.ExpectedStatus == documents.StatusApproved

	if err := writeOutboxEvent(ctx, tx, rejected.DriverID, eventDocumentReviewed, reviewedPayload(rejected, record.Type, "rejected", record.Reason, withdrawn)); err != nil {
		return documents.Document{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return documents.Document{}, fmt.Errorf("commit transaction: %w", err)
	}

	return rejected, nil
}

func (r *DocumentRepository) ListPending(ctx context.Context, query documents.PendingQuery) ([]documents.PendingItem, error) {
	after := query.AfterCreatedAt
	if after.IsZero() {
		after = time.Unix(0, 0).UTC()
	}

	afterID := query.AfterID
	if afterID == "" {
		afterID = "00000000-0000-0000-0000-000000000000"
	}

	rows, err := r.pool.Query(
		ctx,
		`SELECT `+documentColumns+`, dr.display_name, dr.status
		 FROM driver_documents AS d
		 JOIN drivers AS dr ON dr.id = d.driver_id
		 WHERE d.status = 'pending' AND (d.created_at, d.id) > ($1, $2::uuid)
		 ORDER BY d.created_at, d.id
		 LIMIT $3`,
		after, afterID, query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select pending documents: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (documents.PendingItem, error) {
		var item documents.PendingItem

		return item, scanDocument(row, &item.Document, &item.DriverDisplayName, &item.DriverStatus)
	})
}

// --- expiry ------------------------------------------------------------------

type expiryRow struct {
	id, driverID, typeCode string
	expiresOn              time.Time
	remindedDays           *int32
	nameEN, nameAR, nameKU string
	required               bool
}

func (r *DocumentRepository) RunExpiry(ctx context.Context, today documents.Date, thresholds []int, limit int) (documents.ExpiryRound, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return documents.ExpiryRound{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var round documents.ExpiryRound

	if len(thresholds) > 0 {
		if round.Reminded, err = r.remind(ctx, tx, today, thresholds, limit); err != nil {
			return documents.ExpiryRound{}, err
		}
	}

	if round.Expired, round.TookOffline, err = r.expire(ctx, tx, today, limit); err != nil {
		return documents.ExpiryRound{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return documents.ExpiryRound{}, fmt.Errorf("commit transaction: %w", err)
	}

	return round, nil
}

// remind sends each approved document the reminder it is due: the smallest
// threshold not below its days left, if a smaller or equal one was not sent yet.
func (r *DocumentRepository) remind(ctx context.Context, tx pgx.Tx, today documents.Date, thresholds []int, limit int) (int, error) {
	longest := slices.Max(thresholds)

	due, err := collectExpiryRows(tx.Query(
		ctx,
		`SELECT d.id::text, d.driver_id::text, d.type_code, d.expires_on, d.reminded_days,
		        t.name_en, t.name_ar, t.name_ku, t.required
		 FROM driver_documents AS d
		 JOIN driver_document_types AS t ON t.code = d.type_code
		 WHERE d.status = 'approved' AND t.active
		   AND d.expires_on >= $1::date AND d.expires_on <= $1::date + $2::int
		   AND (d.reminded_days IS NULL OR d.reminded_days > (
		        SELECT min(x) FROM unnest($3::int[]) AS x WHERE x >= d.expires_on - $1::date))
		 ORDER BY d.expires_on, d.id
		 LIMIT $4
		 FOR UPDATE OF d SKIP LOCKED`,
		today.Time(), longest, thresholds, limit,
	))
	if err != nil {
		return 0, fmt.Errorf("select documents to remind: %w", err)
	}

	sent := 0

	for _, row := range due {
		expiresOn := documents.DateFromTime(row.expiresOn)
		daysLeft := today.DaysUntil(expiresOn)

		threshold, ok := documents.ReminderThreshold(daysLeft, thresholds)
		if !ok || (row.remindedDays != nil && int(*row.remindedDays) <= threshold) {
			continue
		}

		if _, err := tx.Exec(
			ctx,
			`UPDATE driver_documents SET reminded_days = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
			row.id, threshold,
		); err != nil {
			return 0, fmt.Errorf("record a reminder: %w", err)
		}

		payload := expiryPayload(row, expiresOn)
		payload["days_left"] = daysLeft

		if err := writeOutboxEvent(ctx, tx, row.driverID, eventDocumentExpiring, payload); err != nil {
			return 0, err
		}

		sent++
	}

	return sent, nil
}

// expire tells drivers once that a document ran out, and puts an available
// driver offline when it was a required one.
func (r *DocumentRepository) expire(ctx context.Context, tx pgx.Tx, today documents.Date, limit int) (int, int, error) {
	due, err := collectExpiryRows(tx.Query(
		ctx,
		`SELECT d.id::text, d.driver_id::text, d.type_code, d.expires_on, d.reminded_days,
		        t.name_en, t.name_ar, t.name_ku, t.required
		 FROM driver_documents AS d
		 JOIN driver_document_types AS t ON t.code = d.type_code
		 WHERE d.status = 'approved' AND t.active
		   AND d.expires_on < $1::date AND d.expired_notified_at IS NULL
		 ORDER BY d.expires_on, d.id
		 LIMIT $2
		 FOR UPDATE OF d SKIP LOCKED`,
		today.Time(), limit,
	))
	if err != nil {
		return 0, 0, fmt.Errorf("select expired documents: %w", err)
	}

	offline := 0

	for _, row := range due {
		if _, err := tx.Exec(
			ctx,
			`UPDATE driver_documents SET expired_notified_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
			row.id,
		); err != nil {
			return 0, 0, fmt.Errorf("record an expiry: %w", err)
		}

		if row.required {
			tag, err := tx.Exec(
				ctx,
				`UPDATE drivers
				 SET availability_status = 'offline', updated_at = CURRENT_TIMESTAMP
				 WHERE id = $1 AND availability_status = 'available'`,
				row.driverID,
			)
			if err != nil {
				return 0, 0, fmt.Errorf("take the driver offline: %w", err)
			}

			offline += int(tag.RowsAffected())
		}

		if err := writeOutboxEvent(ctx, tx, row.driverID, eventDocumentExpired, expiryPayload(row, documents.DateFromTime(row.expiresOn))); err != nil {
			return 0, 0, err
		}
	}

	return len(due), offline, nil
}

// --- scanning ------------------------------------------------------------------

func scanType(row pgx.Row, t *documents.Type) error {
	var purpose string

	if err := row.Scan(
		&t.Code, &purpose, &t.NameEN, &t.NameAR, &t.NameKU,
		&t.Required, &t.RequiresNumber, &t.RequiresExpiry, &t.Active, &t.SortOrder,
	); err != nil {
		return err
	}

	t.MediaPurpose = documents.MediaPurpose(purpose)

	return nil
}

func scanDocument(row pgx.Row, d *documents.Document, extra ...any) error {
	var (
		expiresOn  *time.Time
		reviewedAt *time.Time
		status     string
	)

	dest := []any{
		&d.ID, &d.DriverID, &d.TypeCode, &d.MediaID, &d.Number,
		&expiresOn, &status, &d.RejectionReason, &d.ReviewedBy, &reviewedAt,
		&d.CreatedAt, &d.UpdatedAt,
	}

	if err := row.Scan(append(dest, extra...)...); err != nil {
		return err
	}

	d.Status = documents.Status(status)

	if expiresOn != nil {
		d.ExpiresOn = documents.DateFromTime(*expiresOn)
	}

	if reviewedAt != nil {
		d.ReviewedAt = *reviewedAt
	}

	return nil
}

func collectDocuments(rows pgx.Rows, err error) ([]documents.Document, error) {
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (documents.Document, error) {
		var d documents.Document

		return d, scanDocument(row, &d)
	})
}

func collectExpiryRows(rows pgx.Rows, err error) ([]expiryRow, error) {
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (expiryRow, error) {
		var e expiryRow

		return e, row.Scan(&e.id, &e.driverID, &e.typeCode, &e.expiresOn, &e.remindedDays,
			&e.nameEN, &e.nameAR, &e.nameKU, &e.required)
	})
}

func nullableDate(d documents.Date) any {
	if d.IsZero() {
		return nil
	}

	return d.Time()
}

func reviewedPayload(d documents.Document, t documents.Type, decision, reason string, withdrawn bool) map[string]any {
	return map[string]any{
		"driver_id":        d.DriverID,
		"document_id":      d.ID,
		"type_code":        d.TypeCode,
		"document_name_en": t.NameEN,
		"document_name_ar": t.NameAR,
		"document_name_ku": t.NameKU,
		"decision":         decision,
		"reason":           reason,
		"withdrawn":        withdrawn,
		"expires_on":       d.ExpiresOn.String(),
	}
}

func expiryPayload(row expiryRow, expiresOn documents.Date) map[string]any {
	return map[string]any{
		"driver_id":        row.driverID,
		"document_id":      row.id,
		"type_code":        row.typeCode,
		"document_name_en": row.nameEN,
		"document_name_ar": row.nameAR,
		"document_name_ku": row.nameKU,
		"required":         row.required,
		"expires_on":       expiresOn.String(),
	}
}
