package token

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type sessionIDGenerator interface {
	Generate() (string, error)
}

type refreshTokenGenerator interface {
	Generate() (string, error)
}

type accessTokenSigner interface {
	IssueForSession(
		identityID string,
		sessionID string,
		issuedAt time.Time,
		sessionExpiresAt time.Time,
	) (string, int32, error)
}

type SessionCreationInput struct {
	ChallengeID           string
	VerifiedAt            time.Time
	SessionID             string
	IdentityID            string
	SessionExpiresAt      time.Time
	RefreshTokenHash      string
	RefreshTokenExpiresAt time.Time
	SessionMetadata       auth.SessionMetadata
}

type sessionStore interface {
	Create(
		ctx context.Context,
		input SessionCreationInput,
	) (IssuedSession, error)
}

type Issuer struct {
	sessionIDGenerator    sessionIDGenerator
	refreshTokenGenerator refreshTokenGenerator
	accessTokenSigner     accessTokenSigner
	sessionStore          sessionStore
	clock                 auth.Clock
	sessionTTL            time.Duration
	refreshTokenTTL       time.Duration
}

var _ auth.TokenIssuer = (*Issuer)(nil)

func NewIssuer(
	sessionIDGenerator sessionIDGenerator,
	refreshTokenGenerator refreshTokenGenerator,
	accessTokenSigner accessTokenSigner,
	sessionStore sessionStore,
	clock auth.Clock,
	sessionTTL time.Duration,
	refreshTokenTTL time.Duration,
) (*Issuer, error) {
	if sessionIDGenerator == nil {
		return nil, errors.New("session ID generator is required")
	}

	if refreshTokenGenerator == nil {
		return nil, errors.New("refresh token generator is required")
	}

	if accessTokenSigner == nil {
		return nil, errors.New("access token signer is required")
	}

	if sessionStore == nil {
		return nil, errors.New("session store is required")
	}

	if clock == nil {
		return nil, errors.New("clock is required")
	}

	if sessionTTL <= 0 {
		return nil, errors.New("session TTL must be greater than zero")
	}

	if refreshTokenTTL <= 0 {
		return nil, errors.New(
			"refresh token TTL must be greater than zero",
		)
	}

	if refreshTokenTTL > sessionTTL {
		return nil, errors.New(
			"refresh token TTL cannot exceed session TTL",
		)
	}

	return &Issuer{
		sessionIDGenerator:    sessionIDGenerator,
		refreshTokenGenerator: refreshTokenGenerator,
		accessTokenSigner:     accessTokenSigner,
		sessionStore:          sessionStore,
		clock:                 clock,
		sessionTTL:            sessionTTL,
		refreshTokenTTL:       refreshTokenTTL,
	}, nil
}

func (i *Issuer) Issue(
	ctx context.Context,
	input auth.TokenIssueInput,
) (auth.TokenPair, error) {
	if input.Identity.ID == "" {
		return auth.TokenPair{}, errors.New(
			"identity ID cannot be empty",
		)
	}

	if strings.TrimSpace(input.ChallengeID) == "" {
		return auth.TokenPair{}, errors.New(
			"challenge ID cannot be blank",
		)
	}

	if input.VerifiedAt.IsZero() {
		return auth.TokenPair{}, errors.New(
			"OTP verification time cannot be zero",
		)
	}

	sessionID, err := i.sessionIDGenerator.Generate()
	if err != nil {
		return auth.TokenPair{}, fmt.Errorf(
			"generate session ID: %w",
			err,
		)
	}

	refreshToken, err := i.refreshTokenGenerator.Generate()
	if err != nil {
		return auth.TokenPair{}, fmt.Errorf(
			"generate refresh token: %w",
			err,
		)
	}

	refreshTokenHash := HashRefreshToken(refreshToken)

	now := i.clock.Now().UTC()

	sessionExpiresAt := now.Add(i.sessionTTL)
	refreshTokenExpiresAt := now.Add(i.refreshTokenTTL)

	accessToken, accessTokenExpiresInSeconds, err :=
		i.accessTokenSigner.IssueForSession(
			input.Identity.ID,
			sessionID,
			now,
			sessionExpiresAt,
		)
	if err != nil {
		return auth.TokenPair{}, fmt.Errorf(
			"issue access token: %w",
			err,
		)
	}

	if _, err := i.sessionStore.Create(
		ctx,
		SessionCreationInput{
			ChallengeID:           input.ChallengeID,
			VerifiedAt:            input.VerifiedAt.UTC(),
			SessionID:             sessionID,
			IdentityID:            input.Identity.ID,
			SessionExpiresAt:      sessionExpiresAt,
			RefreshTokenHash:      refreshTokenHash,
			RefreshTokenExpiresAt: refreshTokenExpiresAt,
			SessionMetadata:       input.SessionMetadata,
		},
	); err != nil {
		return auth.TokenPair{}, fmt.Errorf(
			"persist authentication session: %w",
			err,
		)
	}

	return auth.TokenPair{
		AccessToken:                 accessToken,
		RefreshToken:                refreshToken,
		AccessTokenExpiresInSeconds: accessTokenExpiresInSeconds,
	}, nil
}
