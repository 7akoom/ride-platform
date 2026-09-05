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

func TestRefreshTokenRotationStoreAllowsOnlyOneConcurrentRotation(
	t *testing.T,
) {
	fixture := newRefreshTokenRotationIntegrationFixture(
		t,
		"+9647500000054",
	)

	cleanupIdentityRefreshTokenReuseDetectedOutboxEvents(
		t,
		fixture,
	)

	currentTokenHash := strings.Repeat(
		"1",
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

	rotatedAt := fixture.now.Add(
		time.Minute,
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

	inputs := []auth.RefreshTokenRotationInput{
		{
			CurrentTokenHash: currentTokenHash,
			ReplacementTokenHash: strings.Repeat(
				"2",
				64,
			),
			RotatedAt:            rotatedAt,
			ReplacementExpiresAt: replacementExpiresAt,
		},
		{
			CurrentTokenHash: currentTokenHash,
			ReplacementTokenHash: strings.Repeat(
				"3",
				64,
			),
			RotatedAt:            rotatedAt,
			ReplacementExpiresAt: replacementExpiresAt,
		},
	}

	start := make(chan struct{})

	results := make(
		chan error,
		len(inputs),
	)

	var waitGroup sync.WaitGroup

	for _, input := range inputs {
		input := input

		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			<-start

			results <- store.Rotate(
				fixture.ctx,
				input,
			)
		}()
	}

	close(start)

	waitGroup.Wait()
	close(results)

	var (
		successCount int
		reuseCount   int
	)

	for rotateErr := range results {
		switch {
		case rotateErr == nil:
			successCount++

		case errors.Is(
			rotateErr,
			auth.ErrRefreshTokenReused,
		):
			reuseCount++

		default:
			t.Fatalf(
				"unexpected concurrent rotation error: %v",
				rotateErr,
			)
		}
	}

	if successCount != 1 {
		t.Fatalf(
			"successful concurrent rotations = %d, expected 1",
			successCount,
		)
	}

	if reuseCount != 1 {
		t.Fatalf(
			"refresh token reuse results = %d, expected 1",
			reuseCount,
		)
	}

	if fixture.sessionRevokedAt() == nil {
		t.Fatal(
			"session was not revoked after concurrent refresh token reuse",
		)
	}

	activeRefreshTokenCount :=
		fixture.activeRefreshTokenCount()

	if activeRefreshTokenCount != 0 {
		t.Fatalf(
			"active refresh tokens after concurrent reuse = %d, expected 0",
			activeRefreshTokenCount,
		)
	}

	var storedReuseDetectedAt *time.Time

	err := fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT reuse_detected_at
			FROM refresh_tokens
			WHERE id = $1::uuid
		`,
		currentTokenID,
	).Scan(
		&storedReuseDetectedAt,
	)
	if err != nil {
		t.Fatalf(
			"query concurrent refresh token reuse detection marker: %v",
			err,
		)
	}

	if storedReuseDetectedAt == nil {
		t.Fatal(
			"concurrent refresh token reuse detection marker was not stored",
		)
	}

	if !storedReuseDetectedAt.Equal(
		rotatedAt,
	) {
		t.Fatalf(
			"reuse detected at = %v, want %v",
			storedReuseDetectedAt,
			rotatedAt,
		)
	}

	assertIdentityRefreshTokenReuseDetectedOutboxEvent(
		t,
		fixture,
		rotatedAt,
	)

	eventCount :=
		countIdentityRefreshTokenReuseDetectedOutboxEvents(
			t,
			fixture,
		)

	if eventCount != 1 {
		t.Fatalf(
			"concurrent refresh token reuse detected outbox event count = %d, want 1",
			eventCount,
		)
	}
}
