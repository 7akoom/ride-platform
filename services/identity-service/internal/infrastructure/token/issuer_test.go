package token

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type fakeSessionIDGenerator struct {
	value string
	err   error
}

func (f *fakeSessionIDGenerator) Generate() (string, error) {
	return f.value, f.err
}

type fakeRefreshTokenGenerator struct {
	value string
	err   error
}

func (f *fakeRefreshTokenGenerator) Generate() (string, error) {
	return f.value, f.err
}

type fakeAccessTokenSigner struct {
	accessToken      string
	expiresInSeconds int32
	err              error

	identityID string
	sessionID  string
	issuedAt   time.Time
}

func (f *fakeAccessTokenSigner) Issue(
	identityID string,
	sessionID string,
	issuedAt time.Time,
) (string, int32, error) {
	f.identityID = identityID
	f.sessionID = sessionID
	f.issuedAt = issuedAt

	if f.err != nil {
		return "", 0, f.err
	}

	return f.accessToken, f.expiresInSeconds, nil
}

type fakeSessionStore struct {
	called bool

	sessionID             string
	identityID            string
	sessionExpiresAt      time.Time
	refreshTokenHash      string
	refreshTokenExpiresAt time.Time

	err error
}

func (f *fakeSessionStore) Create(
	ctx context.Context,
	sessionID string,
	identityID string,
	sessionExpiresAt time.Time,
	refreshTokenHash string,
	refreshTokenExpiresAt time.Time,
) (IssuedSession, error) {
	f.called = true
	f.sessionID = sessionID
	f.identityID = identityID
	f.sessionExpiresAt = sessionExpiresAt
	f.refreshTokenHash = refreshTokenHash
	f.refreshTokenExpiresAt = refreshTokenExpiresAt

	if f.err != nil {
		return IssuedSession{}, f.err
	}

	return IssuedSession{
		SessionID:      sessionID,
		RefreshTokenID: "refresh-token-record-id",
	}, nil
}

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	return f.now
}

func TestIssuerIssueReturnsTokenPairAndPersistsSession(t *testing.T) {
	fixedTime := time.Date(
		2026,
		time.August,
		10,
		4,
		0,
		0,
		0,
		time.UTC,
	)

	sessionIDGenerator := &fakeSessionIDGenerator{
		value: "11111111-1111-4111-8111-111111111111",
	}

	refreshTokenGenerator := &fakeRefreshTokenGenerator{
		value: "rt_test_refresh_token",
	}

	accessTokenSigner := &fakeAccessTokenSigner{
		accessToken:      "signed-access-token",
		expiresInSeconds: 900,
	}

	sessionStore := &fakeSessionStore{}

	clock := &fakeClock{
		now: fixedTime,
	}

	const (
		sessionTTL      = 30 * 24 * time.Hour
		refreshTokenTTL = 29 * 24 * time.Hour
	)

	issuer, err := NewIssuer(
		sessionIDGenerator,
		refreshTokenGenerator,
		accessTokenSigner,
		sessionStore,
		clock,
		sessionTTL,
		refreshTokenTTL,
	)
	if err != nil {
		t.Fatalf("NewIssuer() returned an error: %v", err)
	}

	identity := auth.Identity{
		ID:          "22222222-2222-4222-8222-222222222222",
		PhoneNumber: "+9647500000004",
		IsActive:    true,
	}

	tokenPair, err := issuer.Issue(
		context.Background(),
		identity,
	)
	if err != nil {
		t.Fatalf("Issue() returned an error: %v", err)
	}

	if tokenPair.AccessToken != "signed-access-token" {
		t.Fatalf(
			"AccessToken is %q, expected %q",
			tokenPair.AccessToken,
			"signed-access-token",
		)
	}

	if tokenPair.RefreshToken != "rt_test_refresh_token" {
		t.Fatalf(
			"RefreshToken is %q, expected %q",
			tokenPair.RefreshToken,
			"rt_test_refresh_token",
		)
	}

	if tokenPair.AccessTokenExpiresInSeconds != 900 {
		t.Fatalf(
			"AccessTokenExpiresInSeconds is %d, expected 900",
			tokenPair.AccessTokenExpiresInSeconds,
		)
	}

	if accessTokenSigner.identityID != identity.ID {
		t.Fatalf(
			"access token signer received identity ID %q, expected %q",
			accessTokenSigner.identityID,
			identity.ID,
		)
	}

	if accessTokenSigner.sessionID != sessionIDGenerator.value {
		t.Fatalf(
			"access token signer received session ID %q, expected %q",
			accessTokenSigner.sessionID,
			sessionIDGenerator.value,
		)
	}

	if !accessTokenSigner.issuedAt.Equal(fixedTime) {
		t.Fatalf(
			"access token signer received issuedAt %v, expected %v",
			accessTokenSigner.issuedAt,
			fixedTime,
		)
	}

	if !sessionStore.called {
		t.Fatal("session store was not called")
	}

	if sessionStore.sessionID != sessionIDGenerator.value {
		t.Fatalf(
			"session store received session ID %q, expected %q",
			sessionStore.sessionID,
			sessionIDGenerator.value,
		)
	}

	if sessionStore.identityID != identity.ID {
		t.Fatalf(
			"session store received identity ID %q, expected %q",
			sessionStore.identityID,
			identity.ID,
		)
	}

	expectedRefreshTokenHash := HashRefreshToken(
		refreshTokenGenerator.value,
	)

	if sessionStore.refreshTokenHash != expectedRefreshTokenHash {
		t.Fatal(
			"session store did not receive expected refresh token hash",
		)
	}

	if sessionStore.refreshTokenHash == refreshTokenGenerator.value {
		t.Fatal(
			"session store received raw refresh token instead of hash",
		)
	}

	expectedSessionExpiry := fixedTime.Add(sessionTTL)

	if !sessionStore.sessionExpiresAt.Equal(expectedSessionExpiry) {
		t.Fatalf(
			"session expiry is %v, expected %v",
			sessionStore.sessionExpiresAt,
			expectedSessionExpiry,
		)
	}

	expectedRefreshTokenExpiry := fixedTime.Add(
		refreshTokenTTL,
	)

	if !sessionStore.refreshTokenExpiresAt.Equal(
		expectedRefreshTokenExpiry,
	) {
		t.Fatalf(
			"refresh token expiry is %v, expected %v",
			sessionStore.refreshTokenExpiresAt,
			expectedRefreshTokenExpiry,
		)
	}
}

func TestIssuerIssueDoesNotPersistSessionWhenAccessTokenSigningFails(
	t *testing.T,
) {
	signingError := errors.New("signing failed")

	sessionStore := &fakeSessionStore{}

	issuer, err := NewIssuer(
		&fakeSessionIDGenerator{
			value: "33333333-3333-4333-8333-333333333333",
		},
		&fakeRefreshTokenGenerator{
			value: "rt_test_refresh_token",
		},
		&fakeAccessTokenSigner{
			err: signingError,
		},
		sessionStore,
		&fakeClock{
			now: time.Now().UTC(),
		},
		30*24*time.Hour,
		29*24*time.Hour,
	)
	if err != nil {
		t.Fatalf("NewIssuer() returned an error: %v", err)
	}

	_, err = issuer.Issue(
		context.Background(),
		auth.Identity{
			ID:       "44444444-4444-4444-8444-444444444444",
			IsActive: true,
		},
	)

	if err == nil {
		t.Fatal("Issue() returned nil error when signing failed")
	}

	if sessionStore.called {
		t.Fatal(
			"session store was called after access token signing failed",
		)
	}
}