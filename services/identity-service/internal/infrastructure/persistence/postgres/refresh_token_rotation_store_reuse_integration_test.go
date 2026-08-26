//go:build integration

package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestRefreshTokenRotationStoreRotatesAndDetectsReuse(
	t *testing.T,
) {
	fixture := newRefreshTokenRotationIntegrationFixture(
		t,
		"+9647500000050",
	)

	currentTokenHash := strings.Repeat(
		"a",
		64,
	)

	replacementTokenHash := strings.Repeat(
		"b",
		64,
	)

	currentTokenExpiresAt := fixture.now.Add(
		29 * 24 * time.Hour,
	)

	currentTokenID := fixture.createRefreshToken(
		currentTokenHash,
		currentTokenExpiresAt,
	)

	store := NewRefreshTokenRotationStore(
		fixture.pool,
	)

	refreshContext, err := store.Inspect(
		fixture.ctx,
		currentTokenHash,
		fixture.now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"Inspect() returned an error: %v",
			err,
		)
	}

	if refreshContext.IdentityID != fixture.identityID {
		t.Fatalf(
			"IdentityID is %q, expected %q",
			refreshContext.IdentityID,
			fixture.identityID,
		)
	}

	if refreshContext.SessionID != fixture.sessionID {
		t.Fatalf(
			"SessionID is %q, expected %q",
			refreshContext.SessionID,
			fixture.sessionID,
		)
	}

	rotatedAt := fixture.now.Add(
		2 * time.Minute,
	)

	replacementExpiresAt := rotatedAt.Add(
		29 * 24 * time.Hour,
	)

	if replacementExpiresAt.After(
		fixture.sessionExpiresAt,
	) {
		replacementExpiresAt =
			fixture.sessionExpiresAt
	}

	err = store.Rotate(
		fixture.ctx,
		auth.RefreshTokenRotationInput{
			CurrentTokenHash:     currentTokenHash,
			ReplacementTokenHash: replacementTokenHash,
			RotatedAt:            rotatedAt,
			ReplacementExpiresAt: replacementExpiresAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"Rotate() returned an error: %v",
			err,
		)
	}

	var (
		currentUsedAt        *time.Time
		replacedByTokenID    *string
		currentRevokedAt     *time.Time
		replacementTokenID   string
		replacementUsedAt    *time.Time
		replacementRevokedAt *time.Time
	)

	err = fixture.pool.QueryRow(
		fixture.ctx,
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
		fixture.ctx,
		replacementTokenHash,
		rotatedAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf(
			"Inspect() replacement token returned an error: %v",
			err,
		)
	}

	if replacementContext.IdentityID != fixture.identityID {
		t.Fatalf(
			"replacement token IdentityID is %q, expected %q",
			replacementContext.IdentityID,
			fixture.identityID,
		)
	}

	_, err = store.Inspect(
		fixture.ctx,
		currentTokenHash,
		rotatedAt.Add(2*time.Second),
	)

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

	if fixture.sessionRevokedAt() == nil {
		t.Fatal(
			"session was not revoked after refresh token reuse",
		)
	}

	activeRefreshTokenCount :=
		fixture.activeRefreshTokenCount()

	if activeRefreshTokenCount != 0 {
		t.Fatalf(
			"active refresh tokens after reuse = %d, expected 0",
			activeRefreshTokenCount,
		)
	}
}
