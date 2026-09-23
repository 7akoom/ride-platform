package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/media-service/internal/application/media"
)

type MediaRepository struct {
	pool *pgxpool.Pool
}

var _ media.Repository = (*MediaRepository)(nil)

func NewMediaRepository(pool *pgxpool.Pool) *MediaRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &MediaRepository{pool: pool}
}

const mediaColumns = `id, owner_identity_id, purpose, status, declared_content_type, declared_size,
	content_type, size_bytes, COALESCE(sha256, ''), width, height, object_key, upload_cleared,
	rejection_reason, held, created_at, completed_at`

func scanMedia(row pgx.Row, out *media.Media) error {
	var purpose, status string

	if err := row.Scan(
		&out.ID,
		&out.OwnerIdentityID,
		&purpose,
		&status,
		&out.DeclaredContentType,
		&out.DeclaredSize,
		&out.ContentType,
		&out.SizeBytes,
		&out.SHA256,
		&out.Width,
		&out.Height,
		&out.ObjectKey,
		&out.UploadCleared,
		&out.RejectionReason,
		&out.Held,
		&out.CreatedAt,
		&out.CompletedAt,
	); err != nil {
		return err
	}

	out.Purpose = media.Purpose(purpose)
	out.Status = media.Status(status)

	return nil
}

func (r *MediaRepository) Create(ctx context.Context, m media.Media) error {
	if _, err := r.pool.Exec(
		ctx,
		`INSERT INTO media_objects
		    (id, owner_identity_id, purpose, status, declared_content_type, declared_size, object_key, created_at)
		 VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7)`,
		m.ID,
		m.OwnerIdentityID,
		string(m.Purpose),
		m.DeclaredContentType,
		m.DeclaredSize,
		m.ObjectKey,
		m.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert media: %w", err)
	}

	return nil
}

func (r *MediaRepository) FindByID(ctx context.Context, id string) (media.Media, error) {
	var found media.Media

	err := scanMedia(r.pool.QueryRow(ctx, `SELECT `+mediaColumns+` FROM media_objects WHERE id = $1`, id), &found)
	if errors.Is(err, pgx.ErrNoRows) {
		return media.Media{}, media.ErrNotFound
	}

	if err != nil {
		return media.Media{}, fmt.Errorf("select media: %w", err)
	}

	return found, nil
}

func (r *MediaRepository) CountPending(ctx context.Context, owner string) (int, error) {
	var count int

	if err := r.pool.QueryRow(
		ctx,
		`SELECT count(*) FROM media_objects WHERE owner_identity_id = $1 AND status = 'pending'`,
		owner,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending media: %w", err)
	}

	return count, nil
}

// transition runs an UPDATE that only matches a record in an allowed state,
// and tells a missing record from one in the wrong state.
func (r *MediaRepository) transition(ctx context.Context, id string, update string, args ...any) (media.Media, error) {
	var updated media.Media

	err := scanMedia(r.pool.QueryRow(ctx, update+` RETURNING `+mediaColumns, append([]any{id}, args...)...), &updated)
	if err == nil {
		return updated, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return media.Media{}, fmt.Errorf("update media: %w", err)
	}

	if _, findErr := r.FindByID(ctx, id); findErr != nil {
		return media.Media{}, findErr
	}

	return media.Media{}, media.ErrInvalidState
}

func (r *MediaRepository) MarkReady(ctx context.Context, id string, input media.ReadyInput) (media.Media, error) {
	return r.transition(
		ctx,
		id,
		`UPDATE media_objects
		 SET status = 'ready', content_type = $2, size_bytes = $3, sha256 = $4, width = $5, height = $6,
		     completed_at = $7, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND status = 'pending'`,
		input.ContentType,
		input.SizeBytes,
		input.SHA256,
		input.Width,
		input.Height,
		input.At,
	)
}

func (r *MediaRepository) MarkRejected(ctx context.Context, id string, reason string, at time.Time) (media.Media, error) {
	return r.transition(
		ctx,
		id,
		`UPDATE media_objects
		 SET status = 'rejected', rejection_reason = $2, completed_at = $3, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND status = 'pending'`,
		reason,
		at,
	)
}

func (r *MediaRepository) MarkDeleted(ctx context.Context, id string, _ time.Time) (media.Media, error) {
	return r.transition(
		ctx,
		id,
		`UPDATE media_objects SET status = 'deleted', updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND status IN ('pending', 'ready', 'rejected') AND NOT held`,
	)
}

func (r *MediaRepository) SetHeld(ctx context.Context, id string, held bool) (media.Media, error) {
	return r.transition(
		ctx,
		id,
		`UPDATE media_objects SET held = $2, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND status = 'ready'`,
		held,
	)
}

func (r *MediaRepository) ListStalePending(ctx context.Context, cutoff time.Time, limit int) ([]media.Media, error) {
	return r.list(
		ctx,
		`SELECT `+mediaColumns+` FROM media_objects
		 WHERE status = 'pending' AND created_at < $1
		 ORDER BY created_at
		 LIMIT $2`,
		cutoff,
		limit,
	)
}

func (r *MediaRepository) list(ctx context.Context, query string, args ...any) ([]media.Media, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}
	defer rows.Close()

	var out []media.Media

	for rows.Next() {
		var found media.Media
		if err := scanMedia(rows, &found); err != nil {
			return nil, fmt.Errorf("scan media: %w", err)
		}

		out = append(out, found)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}

	return out, nil
}

func (r *MediaRepository) MarkExpired(ctx context.Context, id string, _ time.Time) error {
	_, err := r.transition(
		ctx,
		id,
		`UPDATE media_objects SET status = 'expired', upload_cleared = TRUE, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND status = 'pending'`,
	)

	return err
}

func (r *MediaRepository) ListUploadsToClear(ctx context.Context, cutoff time.Time, limit int) ([]media.Media, error) {
	return r.list(
		ctx,
		`SELECT `+mediaColumns+` FROM media_objects
		 WHERE NOT upload_cleared AND status <> 'pending' AND created_at < $1
		 ORDER BY created_at
		 LIMIT $2`,
		cutoff,
		limit,
	)
}

func (r *MediaRepository) MarkUploadCleared(ctx context.Context, id string) error {
	if _, err := r.pool.Exec(
		ctx,
		`UPDATE media_objects SET upload_cleared = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
		id,
	); err != nil {
		return fmt.Errorf("mark upload cleared: %w", err)
	}

	return nil
}
