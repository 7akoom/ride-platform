//go:build integration

package token

import (
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestSessionStoreCreatePersistsSessionMetadata(
	t *testing.T,
) {
	fixture := newSessionStoreIntegrationFixture(
		t,
		"+9647500000006",
	)

	sessionID := fixture.generateSessionID()

	now := time.Now().UTC()

	challengeID := "otp-" + sessionID

	fixture.createOTPChallenge(
		challengeID,
		strings.Repeat("a", 64),
		now.Add(5*time.Minute),
	)

	verifiedAt := time.Now().UTC()

	store := NewSessionStore(
		fixture.pool,
	)

	metadata := auth.SessionMetadata{
		ClientID:   " mobile-app ",
		DeviceID:   " device-123 ",
		DeviceName: " iPhone 15 Pro ",
		Platform:   " ios ",
		AppVersion: " 1.0.0 ",
		IPAddress:  " 192.0.2.10 ",
		UserAgent:  " ride-app/1.0.0 ",
	}

	_, err := store.Create(
		fixture.ctx,
		SessionCreationInput{
			ChallengeID:           challengeID,
			VerifiedAt:            verifiedAt,
			SessionID:             sessionID,
			IdentityID:            fixture.identityID,
			SessionExpiresAt:      now.Add(30 * 24 * time.Hour),
			RefreshTokenHash:      HashRefreshToken("rt-session-metadata"),
			RefreshTokenExpiresAt: now.Add(29 * 24 * time.Hour),
			SessionMetadata:       metadata,
		},
	)
	if err != nil {
		t.Fatalf(
			"Create() returned an error: %v",
			err,
		)
	}

	var storedMetadata auth.SessionMetadata

	err = fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT
				client_id,
				device_id,
				device_name,
				platform,
				app_version,
				host(ip_address),
				user_agent
			FROM auth_sessions
			WHERE id = $1::uuid
		`,
		sessionID,
	).Scan(
		&storedMetadata.ClientID,
		&storedMetadata.DeviceID,
		&storedMetadata.DeviceName,
		&storedMetadata.Platform,
		&storedMetadata.AppVersion,
		&storedMetadata.IPAddress,
		&storedMetadata.UserAgent,
	)
	if err != nil {
		t.Fatalf(
			"query stored session metadata: %v",
			err,
		)
	}

	expectedMetadata := auth.SessionMetadata{
		ClientID:   "mobile-app",
		DeviceID:   "device-123",
		DeviceName: "iPhone 15 Pro",
		Platform:   "ios",
		AppVersion: "1.0.0",
		IPAddress:  "192.0.2.10",
		UserAgent:  "ride-app/1.0.0",
	}

	if storedMetadata != expectedMetadata {
		t.Fatalf(
			"stored session metadata = %+v, expected %+v",
			storedMetadata,
			expectedMetadata,
		)
	}
}

func TestSessionStoreCreateStoresBlankSessionMetadataAsNull(
	t *testing.T,
) {
	fixture := newSessionStoreIntegrationFixture(
		t,
		"+9647500000007",
	)

	sessionID := fixture.generateSessionID()

	now := time.Now().UTC()

	challengeID := "otp-" + sessionID

	fixture.createOTPChallenge(
		challengeID,
		strings.Repeat("b", 64),
		now.Add(5*time.Minute),
	)

	verifiedAt := time.Now().UTC()

	store := NewSessionStore(
		fixture.pool,
	)

	_, err := store.Create(
		fixture.ctx,
		SessionCreationInput{
			ChallengeID:           challengeID,
			VerifiedAt:            verifiedAt,
			SessionID:             sessionID,
			IdentityID:            fixture.identityID,
			SessionExpiresAt:      now.Add(30 * 24 * time.Hour),
			RefreshTokenHash:      HashRefreshToken("rt-session-metadata-null"),
			RefreshTokenExpiresAt: now.Add(29 * 24 * time.Hour),
			SessionMetadata: auth.SessionMetadata{
				ClientID:   " ",
				DeviceID:   "\t",
				DeviceName: "\n",
				Platform:   "   ",
				AppVersion: "\t ",
				IPAddress:  " ",
				UserAgent:  "\n\t",
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"Create() returned an error: %v",
			err,
		)
	}

	var allMetadataNull bool

	err = fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT
				client_id IS NULL
				AND device_id IS NULL
				AND device_name IS NULL
				AND platform IS NULL
				AND app_version IS NULL
				AND ip_address IS NULL
				AND user_agent IS NULL
			FROM auth_sessions
			WHERE id = $1::uuid
		`,
		sessionID,
	).Scan(
		&allMetadataNull,
	)
	if err != nil {
		t.Fatalf(
			"query blank session metadata: %v",
			err,
		)
	}

	if !allMetadataNull {
		t.Fatal(
			"blank session metadata was not stored as NULL",
		)
	}
}
