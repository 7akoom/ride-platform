//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestRefreshTokenRotationStoreRotatesAndDetectsReuse(
	t *testing.T,
) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

	pool, err := database.NewPostgresPool(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	t.Cleanup(pool.Close)

	const phoneNumber = "+9647500000050"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM identities
			WHERE phone_number = $1
		`,
		phoneNumber,
	)
	if err != nil {
		t.Fatalf(
			"clean existing test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM identities
				WHERE phone_number = $1
			`,
			phoneNumber,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean test identity: %v",
				cleanupErr,
			)
		}
	})

	now := time.Now().
		UTC().
		Truncate(time.Microsecond)

	var identityID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO identities (
				phone_number,
				created_at,
				updated_at
			)
			VALUES ($1, $2, $2)
			RETURNING id::text
		`,
		phoneNumber,
		now,
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create test identity: %v",
			err,
		)
	}

	sessionExpiresAt := now.Add(
		30 * 24 * time.Hour,
	)

	var sessionID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO auth_sessions (
				identity_id,
				expires_at,
				created_at,
				updated_at
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$3
			)
			RETURNING id::text
		`,
		identityID,
		sessionExpiresAt,
		now,
	).Scan(
		&sessionID,
	)
	if err != nil {
		t.Fatalf(
			"create authentication session: %v",
			err,
		)
	}

	currentTokenHash := strings.Repeat(
		"a",
		64,
	)

	replacementTokenHash := strings.Repeat(
		"b",
		64,
	)

	currentTokenExpiresAt := now.Add(
		29 * 24 * time.Hour,
	)

	var currentTokenID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO refresh_tokens (
				session_id,
				token_hash,
				expires_at,
				created_at
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$4
			)
			RETURNING id::text
		`,
		sessionID,
		currentTokenHash,
		currentTokenExpiresAt,
		now,
	).Scan(
		&currentTokenID,
	)
	if err != nil {
		t.Fatalf(
			"create current refresh token: %v",
			err,
		)
	}

	store := NewRefreshTokenRotationStore(
		pool,
	)

	refreshContext, err := store.Inspect(
		ctx,
		currentTokenHash,
		now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"Inspect() returned an error: %v",
			err,
		)
	}

	if refreshContext.IdentityID != identityID {
		t.Fatalf(
			"IdentityID is %q, expected %q",
			refreshContext.IdentityID,
			identityID,
		)
	}

	if refreshContext.SessionID != sessionID {
		t.Fatalf(
			"SessionID is %q, expected %q",
			refreshContext.SessionID,
			sessionID,
		)
	}

	rotatedAt := now.Add(
		2 * time.Minute,
	)

	replacementExpiresAt := rotatedAt.Add(
		29 * 24 * time.Hour,
	)

	if replacementExpiresAt.After(
		sessionExpiresAt,
	) {
		replacementExpiresAt =
			sessionExpiresAt
	}

	err = store.Rotate(
		ctx,
		auth.RefreshTokenRotationInput{
			CurrentTokenHash:      currentTokenHash,
			ReplacementTokenHash:  replacementTokenHash,
			RotatedAt:             rotatedAt,
			ReplacementExpiresAt:  replacementExpiresAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"Rotate() returned an error: %v",
			err,
		)
	}

	var (
		currentUsedAt          *time.Time
		replacedByTokenID      *string
		currentRevokedAt       *time.Time
		replacementTokenID     string
		replacementUsedAt      *time.Time
		replacementRevokedAt   *time.Time
	)

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				current_rt.used_at,
				current_rt.replaced_by_token_id::text,
				current_rt.revoked_at,
				replacement_rt.id::text,
				replacement_rt.used_at,
				replacement_rt.revoked_at
			FROM refresh_tokens AS current_rt
			INNER JOIN refresh_tokens AS replacement_rt
				ON replacement_rt.id =
					current_rt.replaced_by_token_id
			WHERE current_rt.id = $1::uuid
		`,
		currentTokenID,
	).Scan(
		&currentUsedAt,
		&replacedByTokenID,
		&currentRevokedAt,
		&replacementTokenID,
		&replacementUsedAt,
		&replacementRevokedAt,
	)
	if err != nil {
		t.Fatalf(
			"query rotated refresh tokens: %v",
			err,
		)
	}

	if currentUsedAt == nil {
		t.Fatal(
			"current refresh token was not marked used",
		)
	}

	if replacedByTokenID == nil {
		t.Fatal(
			"current refresh token has no replacement token ID",
		)
	}

	if *replacedByTokenID != replacementTokenID {
		t.Fatalf(
			"replacement token link is %q, expected %q",
			*replacedByTokenID,
			replacementTokenID,
		)
	}

	if currentRevokedAt != nil {
		t.Fatal(
			"current refresh token was unexpectedly revoked during normal rotation",
		)
	}

	if replacementUsedAt != nil {
		t.Fatal(
			"replacement refresh token is unexpectedly already used",
		)
	}

	if replacementRevokedAt != nil {
		t.Fatal(
			"replacement refresh token is unexpectedly revoked",
		)
	}

	replacementContext, err := store.Inspect(
		ctx,
		replacementTokenHash,
		rotatedAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf(
			"Inspect() replacement token returned an error: %v",
			err,
		)
	}

	if replacementContext.IdentityID != identityID {
		t.Fatalf(
			"replacement token IdentityID is %q, expected %q",
			replacementContext.IdentityID,
			identityID,
		)
	}

	err = func() error {
		_, inspectErr := store.Inspect(
			ctx,
			currentTokenHash,
			rotatedAt.Add(2*time.Second),
		)

		return inspectErr
	}()

	if !errors.Is(
		err,
		auth.ErrRefreshTokenReused,
	) {
		t.Fatalf(
			"reused current token returned %v, expected %v",
			err,
			auth.ErrRefreshTokenReused,
		)
	}

	var sessionRevokedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT revoked_at
			FROM auth_sessions
			WHERE id = $1::uuid
		`,
		sessionID,
	).Scan(
		&sessionRevokedAt,
	)
	if err != nil {
		t.Fatalf(
			"query revoked session: %v",
			err,
		)
	}

	if sessionRevokedAt == nil {
		t.Fatal(
			"session was not revoked after refresh token reuse",
		)
	}

	var activeRefreshTokenCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM refresh_tokens
			WHERE session_id = $1::uuid
			  AND revoked_at IS NULL
		`,
		sessionID,
	).Scan(
		&activeRefreshTokenCount,
	)
	if err != nil {
		t.Fatalf(
			"count active refresh tokens: %v",
			err,
		)
	}

	if activeRefreshTokenCount != 0 {
		t.Fatalf(
			"active refresh tokens after reuse = %d, expected 0",
			activeRefreshTokenCount,
		)
	}
}