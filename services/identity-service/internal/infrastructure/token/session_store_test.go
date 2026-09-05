package token

import (
	"context"
	"testing"
	"time"
)

func TestSessionStoreCreateRejectsZeroSessionExpiration(
	t *testing.T,
) {
	store := &SessionStore{}

	now := time.Now().UTC()

	_, err := store.Create(
		context.Background(),
		SessionCreationInput{
			ChallengeID:           "otp_ch_test",
			VerifiedAt:            now,
			SessionID:             "session-test",
			IdentityID:            "identity-test",
			SessionExpiresAt:      time.Time{},
			RefreshTokenHash:      "refresh-token-hash",
			RefreshTokenExpiresAt: now.Add(24 * time.Hour),
		},
	)

	if err == nil {
		t.Fatal(
			"Create() accepted a zero session expiration",
		)
	}
}

func TestSessionStoreCreateRejectsZeroRefreshTokenExpiration(
	t *testing.T,
) {
	store := &SessionStore{}

	now := time.Now().UTC()

	_, err := store.Create(
		context.Background(),
		SessionCreationInput{
			ChallengeID:           "otp_ch_test",
			VerifiedAt:            now,
			SessionID:             "session-test",
			IdentityID:            "identity-test",
			SessionExpiresAt:      now.Add(24 * time.Hour),
			RefreshTokenHash:      "refresh-token-hash",
			RefreshTokenExpiresAt: time.Time{},
		},
	)

	if err == nil {
		t.Fatal(
			"Create() accepted a zero refresh token expiration",
		)
	}
}

func TestSessionStoreCreateRejectsRefreshTokenExpirationAfterSession(
	t *testing.T,
) {
	store := &SessionStore{}

	now := time.Now().UTC()

	_, err := store.Create(
		context.Background(),
		SessionCreationInput{
			ChallengeID:           "otp_ch_test",
			VerifiedAt:            now,
			SessionID:             "session-test",
			IdentityID:            "identity-test",
			SessionExpiresAt:      now.Add(24 * time.Hour),
			RefreshTokenHash:      "refresh-token-hash",
			RefreshTokenExpiresAt: now.Add(25 * time.Hour),
		},
	)

	if err == nil {
		t.Fatal(
			"Create() accepted a refresh token that expires after its session",
		)
	}
}

func TestSessionStoreCreateRejectsBlankChallengeID(
	t *testing.T,
) {
	store := &SessionStore{}

	now := time.Now().UTC()

	_, err := store.Create(
		context.Background(),
		SessionCreationInput{
			ChallengeID:           "   ",
			VerifiedAt:            now,
			SessionID:             "session-test",
			IdentityID:            "identity-test",
			SessionExpiresAt:      now.Add(24 * time.Hour),
			RefreshTokenHash:      "refresh-token-hash",
			RefreshTokenExpiresAt: now.Add(23 * time.Hour),
		},
	)

	if err == nil {
		t.Fatal(
			"Create() accepted a blank challenge ID",
		)
	}
}

func TestSessionStoreCreateRejectsZeroVerificationTime(
	t *testing.T,
) {
	store := &SessionStore{}

	now := time.Now().UTC()

	_, err := store.Create(
		context.Background(),
		SessionCreationInput{
			ChallengeID:           "otp_ch_test",
			VerifiedAt:            time.Time{},
			SessionID:             "session-test",
			IdentityID:            "identity-test",
			SessionExpiresAt:      now.Add(24 * time.Hour),
			RefreshTokenHash:      "refresh-token-hash",
			RefreshTokenExpiresAt: now.Add(23 * time.Hour),
		},
	)

	if err == nil {
		t.Fatal(
			"Create() accepted a zero OTP verification time",
		)
	}
}

func TestNewSessionStorePanicsWhenPoolIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewSessionStore() did not panic for nil PostgreSQL pool",
			)
		}
	}()

	NewSessionStore(nil)
}
