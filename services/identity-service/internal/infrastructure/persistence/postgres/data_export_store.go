package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/dataexport"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DataExportReadyEvent tells notification-service to say the file is ready.
const DataExportReadyEvent auth.IdentityDomainEventType = "identity.data_export_ready"

// DataExportStore keeps data exports (dataexport.Store), and reads this
// service's own part of them (Sections).
type DataExportStore struct {
	pool *pgxpool.Pool
}

func NewDataExportStore(pool *pgxpool.Pool) *DataExportStore {
	if pool == nil {
		panic("database pool is required")
	}

	return &DataExportStore{pool: pool}
}

const dataExportColumns = `id::text, identity_id::text, status, requested_at, ready_at, expires_at,
	COALESCE(media_id::text, ''), size_bytes, attempts`

func scanDataExport(row pgx.Row) (dataexport.Export, error) {
	var (
		e      dataexport.Export
		status string
	)

	if err := row.Scan(&e.ID, &e.IdentityID, &status, &e.RequestedAt, &e.ReadyAt, &e.ExpiresAt,
		&e.MediaID, &e.SizeBytes, &e.Attempts); err != nil {
		return dataexport.Export{}, err
	}

	e.Status = dataexport.Status(status)

	return e, nil
}

func (s *DataExportStore) list(ctx context.Context, query string, args ...any) ([]dataexport.Export, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list data exports: %w", err)
	}
	defer rows.Close()

	var out []dataexport.Export

	for rows.Next() {
		e, err := scanDataExport(rows)
		if err != nil {
			return nil, fmt.Errorf("scan data export: %w", err)
		}

		out = append(out, e)
	}

	return out, rows.Err()
}

func (s *DataExportStore) Create(ctx context.Context, e dataexport.Export) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO data_exports (id, identity_id, status, requested_at, next_attempt_at)
		 VALUES ($1::uuid, $2::uuid, 'pending', $3, $3)`, e.ID, e.IdentityID, e.RequestedAt); err != nil {
		return fmt.Errorf("record data export: %w", err)
	}

	return nil
}

func (s *DataExportStore) List(ctx context.Context, identityID string, limit int) ([]dataexport.Export, error) {
	if !isUUIDText(identityID) {
		return nil, nil
	}

	return s.list(ctx,
		`SELECT `+dataExportColumns+` FROM data_exports WHERE identity_id = $1::uuid
		 ORDER BY requested_at DESC, id DESC LIMIT $2`, identityID, limit)
}

func (s *DataExportStore) Find(ctx context.Context, identityID, exportID string) (dataexport.Export, error) {
	if !isUUIDText(identityID) || !isUUIDText(exportID) {
		return dataexport.Export{}, dataexport.ErrNotFound
	}

	e, err := scanDataExport(s.pool.QueryRow(ctx,
		`SELECT `+dataExportColumns+` FROM data_exports WHERE id = $1::uuid AND identity_id = $2::uuid`,
		exportID, identityID))
	if errors.Is(err, pgx.ErrNoRows) {
		return dataexport.Export{}, dataexport.ErrNotFound
	}

	if err != nil {
		return dataexport.Export{}, fmt.Errorf("read data export: %w", err)
	}

	return e, nil
}

func (s *DataExportStore) ClaimPending(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]dataexport.Export, error) {
	return s.list(ctx,
		`UPDATE data_exports SET next_attempt_at = $2, updated_at = $1
		 WHERE id IN (
		     SELECT id FROM data_exports
		     WHERE status = 'pending' AND next_attempt_at <= $1
		     ORDER BY next_attempt_at
		     LIMIT $3
		     FOR UPDATE SKIP LOCKED)
		 RETURNING `+dataExportColumns, now, now.Add(lease), limit)
}

type dataExportReadyPayload struct {
	IdentityID string    `json:"identity_id"`
	RiderID    string    `json:"rider_id"`
	DriverID   string    `json:"driver_id"`
	ExportID   string    `json:"export_id"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func (s *DataExportStore) MarkReady(ctx context.Context, in dataexport.ReadyInput) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin data export ready: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var identityID string

	err = tx.QueryRow(ctx,
		`UPDATE data_exports
		 SET status = 'ready', media_id = $2::uuid, size_bytes = $3, ready_at = $4, expires_at = $5,
		     last_error = '', updated_at = $4
		 WHERE id = $1::uuid AND status = 'pending'
		 RETURNING identity_id::text`,
		in.ID, in.MediaID, in.SizeBytes, in.ReadyAt, in.ExpiresAt).Scan(&identityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("the data export is no longer pending")
	}

	if err != nil {
		return fmt.Errorf("mark data export ready: %w", err)
	}

	if err := insertIdentityOutboxEventInTransaction(ctx, tx, identityID, DataExportReadyEvent,
		auth.IdentityDomainEventSchemaVersion, dataExportReadyPayload{
			IdentityID: identityID, RiderID: in.Profiles.RiderID, DriverID: in.Profiles.DriverID,
			ExportID: in.ID, ExpiresAt: in.ExpiresAt.UTC(),
		}, in.ReadyAt); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit data export ready: %w", err)
	}

	return nil
}

func (s *DataExportStore) MarkAttemptFailed(ctx context.Context, id string, retryAt *time.Time, reason string) error {
	if len(reason) > 300 {
		reason = reason[:300]
	}

	reason = strings.ToValidUTF8(reason, "")

	var err error

	if retryAt == nil {
		_, err = s.pool.Exec(ctx,
			`UPDATE data_exports SET status = 'failed', attempts = attempts + 1, last_error = $2, updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1::uuid AND status = 'pending'`, id, reason)
	} else {
		_, err = s.pool.Exec(ctx,
			`UPDATE data_exports SET attempts = attempts + 1, next_attempt_at = $3, last_error = $2, updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1::uuid AND status = 'pending'`, id, reason, *retryAt)
	}

	if err != nil {
		return fmt.Errorf("record data export failure: %w", err)
	}

	return nil
}

func (s *DataExportStore) ListExpired(ctx context.Context, now time.Time, limit int) ([]dataexport.Export, error) {
	return s.list(ctx,
		`SELECT `+dataExportColumns+` FROM data_exports
		 WHERE status = 'ready' AND expires_at <= $1 ORDER BY expires_at LIMIT $2`, now, limit)
}

func (s *DataExportStore) MarkExpired(ctx context.Context, id string) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE data_exports SET status = 'expired', updated_at = CURRENT_TIMESTAMP WHERE id = $1::uuid AND status = 'ready'`,
		id); err != nil {
		return fmt.Errorf("mark data export expired: %w", err)
	}

	return nil
}

// identitySections is this service's own part of a person's export.
var identitySections = []struct{ name, sql string }{
	{"identity/account.json", `SELECT jsonb_agg(to_jsonb(x)) FROM (
		SELECT id, status, created_at FROM identities WHERE id = $1::uuid) x`},
	{"identity/sign_in_methods.json", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT identifier_type AS type, normalized_value AS value, verified_at, created_at
		FROM identity_identifiers WHERE identity_id = $1::uuid) x`},
	{"identity/sessions.json", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT device_name, platform, app_version, host(ip_address) AS ip_address, user_agent,
		       created_at, last_seen_at, expires_at, revoked_at
		FROM auth_sessions WHERE identity_id = $1::uuid) x`},
	{"identity/wallet_pin.json", `SELECT jsonb_agg(to_jsonb(x)) FROM (
		SELECT true AS is_set, updated_at FROM wallet_pins WHERE identity_id = $1::uuid) x`},
}

// Sections reads the person's account, sign-in methods, sessions and whether
// they have a wallet PIN (never the PIN).
func (s *DataExportStore) Sections(ctx context.Context, identityID string, _ dataexport.Profiles) ([]dataexport.Section, error) {
	var out []dataexport.Section

	for _, q := range identitySections {
		var content string

		if err := s.pool.QueryRow(ctx,
			`SELECT jsonb_pretty(COALESCE((`+q.sql+`), '[]'::jsonb))`, identityID).Scan(&content); err != nil {
			return nil, fmt.Errorf("export %s: %w", q.name, err)
		}

		out = append(out, dataexport.Section{Name: q.name, Content: []byte(content)})
	}

	return out, nil
}
