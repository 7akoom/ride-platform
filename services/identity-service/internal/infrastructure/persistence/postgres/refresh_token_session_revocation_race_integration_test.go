//go:build integration

package postgres

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestRefreshTokenRotationFailsAfterSessionRevocation(
	t *testing.T,
) {
	fixture := newRefreshTokenRotationIntegrationFixture(
		t,
		"+9647500000055",
	)

	currentTokenHash := strings.Repeat(
		"4",
		64,
	)

	replacementTokenHash := strings.Repeat(
		"5",
		64,
	)

	fixture.createRefreshToken(
		currentTokenHash,
		fixture.now.Add(29*24*time.Hour),
	)

	rotationStore := NewRefreshTokenRotationStore(
		fixture.pool,
	)

	revocationStore := NewSessionRevocationStore(
		fixture.pool,
	)

	revokedAt := fixture.now.Add(
		time.Minute,
	)

	err := revocationStore.RevokeSessionByRefreshTokenHash(
		fixture.ctx,
		currentTokenHash,
		revokedAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	rotatedAt := revokedAt.Add(
		time.Minute,
	)

	err = rotationStore.Rotate(
		fixture.ctx,
		auth.RefreshTokenRotationInput{
			CurrentTokenHash:     currentTokenHash,
			ReplacementTokenHash: replacementTokenHash,
			RotatedAt:            rotatedAt,
			ReplacementExpiresAt: refreshTokenRaceReplacementExpiration(
				fixture,
				rotatedAt,
			),
		},
	)

	if !errors.Is(
		err,
		auth.ErrSessionRevoked,
	) {
		t.Fatalf(
			"Rotate() after session revocation returned %v, want %v",
			err,
			auth.ErrSessionRevoked,
		)
	}

	if fixture.sessionRevokedAt() == nil {
		t.Fatal(
			"session is not revoked",
		)
	}

	if fixture.activeRefreshTokenCount() != 0 {
		t.Fatalf(
			"active refresh token count = %d, want 0",
			fixture.activeRefreshTokenCount(),
		)
	}

	totalReplacementTokens, revokedReplacementTokens :=
		refreshTokenRaceCountsByHash(
			t,
			fixture,
			replacementTokenHash,
		)

	if totalReplacementTokens != 0 {
		t.Fatalf(
			"replacement refresh token count = %d, want 0",
			totalReplacementTokens,
		)
	}

	if revokedReplacementTokens != 0 {
		t.Fatalf(
			"revoked replacement refresh token count = %d, want 0",
			revokedReplacementTokens,
		)
	}
}

func TestSessionRevocationAfterRefreshTokenRotationRevokesReplacement(
	t *testing.T,
) {
	fixture := newRefreshTokenRotationIntegrationFixture(
		t,
		"+9647500000056",
	)

	currentTokenHash := strings.Repeat(
		"6",
		64,
	)

	replacementTokenHash := strings.Repeat(
		"7",
		64,
	)

	fixture.createRefreshToken(
		currentTokenHash,
		fixture.now.Add(29*24*time.Hour),
	)

	rotationStore := NewRefreshTokenRotationStore(
		fixture.pool,
	)

	revocationStore := NewSessionRevocationStore(
		fixture.pool,
	)

	rotatedAt := fixture.now.Add(
		time.Minute,
	)

	err := rotationStore.Rotate(
		fixture.ctx,
		auth.RefreshTokenRotationInput{
			CurrentTokenHash:     currentTokenHash,
			ReplacementTokenHash: replacementTokenHash,
			RotatedAt:            rotatedAt,
			ReplacementExpiresAt: refreshTokenRaceReplacementExpiration(
				fixture,
				rotatedAt,
			),
		},
	)
	if err != nil {
		t.Fatalf(
			"Rotate() returned an error: %v",
			err,
		)
	}

	totalReplacementTokens, revokedReplacementTokens :=
		refreshTokenRaceCountsByHash(
			t,
			fixture,
			replacementTokenHash,
		)

	if totalReplacementTokens != 1 {
		t.Fatalf(
			"replacement refresh token count before revocation = %d, want 1",
			totalReplacementTokens,
		)
	}

	if revokedReplacementTokens != 0 {
		t.Fatalf(
			"revoked replacement refresh token count before revocation = %d, want 0",
			revokedReplacementTokens,
		)
	}

	revokedAt := rotatedAt.Add(
		time.Minute,
	)

	err = revocationStore.RevokeSessionByRefreshTokenHash(
		fixture.ctx,
		currentTokenHash,
		revokedAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	if fixture.sessionRevokedAt() == nil {
		t.Fatal(
			"session was not revoked",
		)
	}

	if fixture.activeRefreshTokenCount() != 0 {
		t.Fatalf(
			"active refresh token count after revocation = %d, want 0",
			fixture.activeRefreshTokenCount(),
		)
	}

	totalReplacementTokens, revokedReplacementTokens =
		refreshTokenRaceCountsByHash(
			t,
			fixture,
			replacementTokenHash,
		)

	if totalReplacementTokens != 1 {
		t.Fatalf(
			"replacement refresh token count after revocation = %d, want 1",
			totalReplacementTokens,
		)
	}

	if revokedReplacementTokens != 1 {
		t.Fatalf(
			"revoked replacement refresh token count after revocation = %d, want 1",
			revokedReplacementTokens,
		)
	}
}

func TestRefreshTokenRotationAndSessionRevocationRaceLeavesNoActiveRefreshToken(
	t *testing.T,
) {
	fixture := newRefreshTokenRotationIntegrationFixture(
		t,
		"+9647500000057",
	)

	currentTokenHash := strings.Repeat(
		"8",
		64,
	)

	replacementTokenHash := strings.Repeat(
		"9",
		64,
	)

	fixture.createRefreshToken(
		currentTokenHash,
		fixture.now.Add(29*24*time.Hour),
	)

	rotationStore := NewRefreshTokenRotationStore(
		fixture.pool,
	)

	revocationStore := NewSessionRevocationStore(
		fixture.pool,
	)

	operationAt := fixture.now.Add(
		time.Minute,
	)

	start := make(chan struct{})

	var (
		rotateErr error
		revokeErr error
		waitGroup sync.WaitGroup
	)

	waitGroup.Add(2)

	go func() {
		defer waitGroup.Done()

		<-start

		rotateErr = rotationStore.Rotate(
			fixture.ctx,
			auth.RefreshTokenRotationInput{
				CurrentTokenHash:     currentTokenHash,
				ReplacementTokenHash: replacementTokenHash,
				RotatedAt:            operationAt,
				ReplacementExpiresAt: refreshTokenRaceReplacementExpiration(
					fixture,
					operationAt,
				),
			},
		)
	}()

	go func() {
		defer waitGroup.Done()

		<-start

		revokeErr = revocationStore.RevokeSessionByRefreshTokenHash(
			fixture.ctx,
			currentTokenHash,
			operationAt,
		)
	}()

	close(start)

	waitGroup.Wait()

	if revokeErr != nil {
		t.Fatalf(
			"concurrent RevokeByRefreshTokenHash() returned an error: %v",
			revokeErr,
		)
	}

	switch {
	case rotateErr == nil:

	case errors.Is(
		rotateErr,
		auth.ErrSessionRevoked,
	):

	default:
		t.Fatalf(
			"concurrent Rotate() returned unexpected error: %v",
			rotateErr,
		)
	}

	if fixture.sessionRevokedAt() == nil {
		t.Fatal(
			"session is not revoked after refresh/revoke race",
		)
	}

	activeRefreshTokens :=
		fixture.activeRefreshTokenCount()

	if activeRefreshTokens != 0 {
		t.Fatalf(
			"active refresh token count after refresh/revoke race = %d, want 0",
			activeRefreshTokens,
		)
	}

	totalReplacementTokens, revokedReplacementTokens :=
		refreshTokenRaceCountsByHash(
			t,
			fixture,
			replacementTokenHash,
		)

	if rotateErr == nil {
		if totalReplacementTokens != 1 {
			t.Fatalf(
				"replacement refresh token count after successful concurrent rotation = %d, want 1",
				totalReplacementTokens,
			)
		}

		if revokedReplacementTokens != 1 {
			t.Fatalf(
				"revoked replacement refresh token count after successful concurrent rotation = %d, want 1",
				revokedReplacementTokens,
			)
		}

		return
	}

	if totalReplacementTokens != 0 {
		t.Fatalf(
			"replacement refresh token count after rejected concurrent rotation = %d, want 0",
			totalReplacementTokens,
		)
	}

	if revokedReplacementTokens != 0 {
		t.Fatalf(
			"revoked replacement refresh token count after rejected concurrent rotation = %d, want 0",
			revokedReplacementTokens,
		)
	}
}

func refreshTokenRaceReplacementExpiration(
	fixture *refreshTokenRotationIntegrationFixture,
	rotatedAt time.Time,
) time.Time {
	replacementExpiresAt := rotatedAt.Add(
		29 * 24 * time.Hour,
	)

	if replacementExpiresAt.After(
		fixture.sessionExpiresAt,
	) {
		return fixture.sessionExpiresAt
	}

	return replacementExpiresAt
}

func refreshTokenRaceCountsByHash(
	t *testing.T,
	fixture *refreshTokenRotationIntegrationFixture,
	tokenHash string,
) (
	int,
	int,
) {
	t.Helper()

	var (
		total   int
		revoked int
	)

	err := fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT
				COUNT(*),
				COUNT(*) FILTER (
					WHERE revoked_at IS NOT NULL
				)
			FROM refresh_tokens
			WHERE token_hash = $1
		`,
		tokenHash,
	).Scan(
		&total,
		&revoked,
	)
	if err != nil {
		t.Fatalf(
			"query refresh token race state: %v",
			err,
		)
	}

	return total, revoked
}
