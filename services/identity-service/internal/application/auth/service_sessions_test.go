package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestListMySessionsMarksCurrentSessionAndMapsDetails(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		26,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	clientID := "mobile-app"
	deviceID := "device-123"
	deviceName := "iPhone"
	platform := "ios"
	appVersion := "1.0.0"
	ipAddress := "192.0.2.10"
	userAgent := "ride-app/1.0.0"
	lastSeenAt := now.Add(-2 * time.Minute)

	sessionReader := &testSessionReader{
		sessions: []SessionDetails{
			{
				ID:         "session-current",
				ClientID:   &clientID,
				DeviceID:   &deviceID,
				DeviceName: &deviceName,
				Platform:   &platform,
				AppVersion: &appVersion,
				IPAddress:  &ipAddress,
				UserAgent:  &userAgent,
				ExpiresAt:  now.Add(time.Hour),
				LastSeenAt: &lastSeenAt,
				CreatedAt:  now.Add(-10 * time.Minute),
			},
			{
				ID:        "session-other",
				ExpiresAt: now.Add(2 * time.Hour),
				CreatedAt: now.Add(-20 * time.Minute),
			},
		},
	}

	service := &service{
		sessionReader: sessionReader,
		clock: &testClock{
			now: now,
		},
	}

	result, err := service.ListMySessions(
		context.Background(),
		ListMySessionsInput{
			IdentityID:       " identity-123 ",
			CurrentSessionID: " session-current ",
		},
	)
	if err != nil {
		t.Fatalf(
			"ListMySessions() returned an error: %v",
			err,
		)
	}

	if !sessionReader.called {
		t.Fatal(
			"ListMySessions() did not call SessionReader",
		)
	}

	if sessionReader.identityID != "identity-123" {
		t.Fatalf(
			"SessionReader identity ID = %q, want %q",
			sessionReader.identityID,
			"identity-123",
		)
	}

	if !sessionReader.now.Equal(now) {
		t.Fatalf(
			"SessionReader now = %v, want %v",
			sessionReader.now,
			now,
		)
	}

	if len(result.Sessions) != 2 {
		t.Fatalf(
			"ListMySessions() returned %d sessions, want 2",
			len(result.Sessions),
		)
	}

	current := result.Sessions[0]

	if current.SessionID != "session-current" {
		t.Fatalf(
			"current session ID = %q, want %q",
			current.SessionID,
			"session-current",
		)
	}

	if !current.IsCurrent {
		t.Fatal(
			"current session IsCurrent = false, want true",
		)
	}

	if current.ClientID == nil ||
		*current.ClientID != clientID {
		t.Fatalf(
			"current session ClientID = %v, want %q",
			current.ClientID,
			clientID,
		)
	}

	if current.DeviceID == nil ||
		*current.DeviceID != deviceID {
		t.Fatalf(
			"current session DeviceID = %v, want %q",
			current.DeviceID,
			deviceID,
		)
	}

	if current.IPAddress == nil ||
		*current.IPAddress != ipAddress {
		t.Fatalf(
			"current session IPAddress = %v, want %q",
			current.IPAddress,
			ipAddress,
		)
	}

	if result.Sessions[1].IsCurrent {
		t.Fatal(
			"non-current session IsCurrent = true, want false",
		)
	}
}

func TestRevokeSessionUsesAuthenticatedIdentityAndClock(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		26,
		13,
		0,
		0,
		0,
		time.UTC,
	)

	revocationStore :=
		&testSessionManagementRevocationStore{}

	service := &service{
		sessionManagementRevocationStore: revocationStore,
		clock: &testClock{
			now: now,
		},
	}

	err := service.RevokeSession(
		context.Background(),
		RevokeSessionInput{
			IdentityID: " identity-123 ",
			SessionID:  " session-456 ",
		},
	)
	if err != nil {
		t.Fatalf(
			"RevokeSession() returned an error: %v",
			err,
		)
	}

	if !revocationStore.called {
		t.Fatal(
			"RevokeSession() did not call revocation store",
		)
	}

	if revocationStore.identityID != "identity-123" {
		t.Fatalf(
			"revocation identity ID = %q, want %q",
			revocationStore.identityID,
			"identity-123",
		)
	}

	if revocationStore.sessionID != "session-456" {
		t.Fatalf(
			"revocation session ID = %q, want %q",
			revocationStore.sessionID,
			"session-456",
		)
	}

	if !revocationStore.revokedAt.Equal(now) {
		t.Fatalf(
			"revocation time = %v, want %v",
			revocationStore.revokedAt,
			now,
		)
	}
}
func TestListMySessionsRejectsMissingAuthenticatedSessionContext(
	t *testing.T,
) {
	tests := []struct {
		name  string
		input ListMySessionsInput
		want  error
	}{
		{
			name: "blank identity ID",
			input: ListMySessionsInput{
				IdentityID:       "   ",
				CurrentSessionID: "session-current",
			},
			want: ErrIdentityNotFound,
		},
		{
			name: "blank current session ID",
			input: ListMySessionsInput{
				IdentityID:       "identity-123",
				CurrentSessionID: "   ",
			},
			want: ErrSessionNotFound,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			sessionReader := &testSessionReader{}

			service := &service{
				sessionReader: sessionReader,
				clock:         &testClock{},
			}

			_, err := service.ListMySessions(
				context.Background(),
				testCase.input,
			)

			if err != testCase.want {
				t.Fatalf(
					"ListMySessions() error = %v, want %v",
					err,
					testCase.want,
				)
			}

			if sessionReader.called {
				t.Fatal(
					"ListMySessions() called SessionReader for invalid input",
				)
			}
		})
	}
}
func TestRevokeSessionRejectsInvalidInputBeforeStoreCall(
	t *testing.T,
) {
	tests := []struct {
		name  string
		input RevokeSessionInput
		want  error
	}{
		{
			name: "blank identity ID",
			input: RevokeSessionInput{
				IdentityID: "   ",
				SessionID:  "session-456",
			},
			want: ErrIdentityNotFound,
		},
		{
			name: "blank session ID",
			input: RevokeSessionInput{
				IdentityID: "identity-123",
				SessionID:  "   ",
			},
			want: ErrSessionNotFound,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			revocationStore :=
				&testSessionManagementRevocationStore{}

			service := &service{
				sessionManagementRevocationStore: revocationStore,
				clock: &testClock{
					now: time.Now(),
				},
			}

			err := service.RevokeSession(
				context.Background(),
				testCase.input,
			)

			if err != testCase.want {
				t.Fatalf(
					"RevokeSession() error = %v, want %v",
					err,
					testCase.want,
				)
			}

			if revocationStore.called {
				t.Fatal(
					"RevokeSession() called revocation store for invalid input",
				)
			}
		})
	}
}
func TestListMySessionsWrapsSessionReaderError(
	t *testing.T,
) {
	readerError := errors.New("session reader failed")

	sessionReader := &testSessionReader{
		err: readerError,
	}

	service := &service{
		sessionReader: sessionReader,
		clock: &testClock{
			now: time.Date(
				2026,
				time.August,
				26,
				14,
				0,
				0,
				0,
				time.UTC,
			),
		},
	}

	_, err := service.ListMySessions(
		context.Background(),
		ListMySessionsInput{
			IdentityID:       "identity-123",
			CurrentSessionID: "session-current",
		},
	)

	if !errors.Is(err, readerError) {
		t.Fatalf(
			"ListMySessions() error = %v, want wrapped %v",
			err,
			readerError,
		)
	}
}
func TestRevokeSessionReturnsSessionNotFound(
	t *testing.T,
) {
	revocationStore :=
		&testSessionManagementRevocationStore{
			err: ErrSessionNotFound,
		}

	service := &service{
		sessionManagementRevocationStore: revocationStore,
		clock: &testClock{
			now: time.Date(
				2026,
				time.August,
				26,
				15,
				0,
				0,
				0,
				time.UTC,
			),
		},
	}

	err := service.RevokeSession(
		context.Background(),
		RevokeSessionInput{
			IdentityID: "identity-123",
			SessionID:  "session-missing",
		},
	)

	if err != ErrSessionNotFound {
		t.Fatalf(
			"RevokeSession() error = %v, want %v",
			err,
			ErrSessionNotFound,
		)
	}

	if !revocationStore.called {
		t.Fatal(
			"RevokeSession() did not call revocation store",
		)
	}
}
func TestRevokeSessionWrapsUnexpectedStoreError(
	t *testing.T,
) {
	storeError := errors.New(
		"session revocation store failed",
	)

	revocationStore :=
		&testSessionManagementRevocationStore{
			err: storeError,
		}

	service := &service{
		sessionManagementRevocationStore: revocationStore,
		clock: &testClock{
			now: time.Date(
				2026,
				time.August,
				26,
				16,
				0,
				0,
				0,
				time.UTC,
			),
		},
	}

	err := service.RevokeSession(
		context.Background(),
		RevokeSessionInput{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)

	if !errors.Is(err, storeError) {
		t.Fatalf(
			"RevokeSession() error = %v, want wrapped %v",
			err,
			storeError,
		)
	}

	if !revocationStore.called {
		t.Fatal(
			"RevokeSession() did not call revocation store",
		)
	}
}
