package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestRefreshTokenRotationStoreInspectRejectsZeroTime(
	t *testing.T,
) {
	store := &RefreshTokenRotationStore{}

	_, err := store.Inspect(
		context.Background(),
		"current-token-hash",
		time.Time{},
	)

	if err == nil {
		t.Fatal(
			"Inspect() accepted a zero inspection time",
		)
	}
}

func TestRefreshTokenRotationStoreRotateRejectsZeroRotationTime(
	t *testing.T,
) {
	store := &RefreshTokenRotationStore{}

	err := store.Rotate(
		context.Background(),
		auth.RefreshTokenRotationInput{
			CurrentTokenHash:     "current-token-hash",
			ReplacementTokenHash: "replacement-token-hash",
			RotatedAt:            time.Time{},
			ReplacementExpiresAt: time.Now().UTC().Add(time.Hour),
		},
	)

	if err == nil {
		t.Fatal(
			"Rotate() accepted a zero rotation time",
		)
	}
}

func TestRefreshTokenRotationStoreRotateRejectsZeroReplacementExpiration(
	t *testing.T,
) {
	store := &RefreshTokenRotationStore{}

	err := store.Rotate(
		context.Background(),
		auth.RefreshTokenRotationInput{
			CurrentTokenHash:     "current-token-hash",
			ReplacementTokenHash: "replacement-token-hash",
			RotatedAt:            time.Now().UTC(),
			ReplacementExpiresAt: time.Time{},
		},
	)

	if err == nil {
		t.Fatal(
			"Rotate() accepted a zero replacement expiration",
		)
	}
}

func TestNewRefreshTokenRotationStorePanicsWhenPoolIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewRefreshTokenRotationStore() did not panic for nil PostgreSQL pool",
			)
		}
	}()

	NewRefreshTokenRotationStore(nil)
}
